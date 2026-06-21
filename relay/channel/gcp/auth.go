package gcp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/golang-jwt/jwt/v5"
)

type Credentials struct {
	Type        string `json:"type"`
	ProjectID   string `json:"project_id"`
	PrivateKey  string `json:"private_key"`
	ClientEmail string `json:"client_email"`
	TokenURI    string `json:"token_uri"`
}

type cachedAccessToken struct {
	Token     string
	ExpiresAt time.Time
}

var accessTokenCache sync.Map

func buildAccessTokenCacheKey(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil {
		return "gcp-access-token-unknown"
	}
	if info.ChannelIsMultiKey {
		return fmt.Sprintf("gcp-access-token-%d-%d", info.ChannelId, info.ChannelMultiKeyIndex)
	}
	return fmt.Sprintf("gcp-access-token-%d", info.ChannelId)
}

func getAccessToken(info *relaycommon.RelayInfo, creds Credentials) (string, error) {
	cacheKey := buildAccessTokenCacheKey(info)
	if val, ok := accessTokenCache.Load(cacheKey); ok {
		if cached, ok := val.(cachedAccessToken); ok && cached.Token != "" && time.Now().Before(cached.ExpiresAt) {
			return cached.Token, nil
		}
		accessTokenCache.Delete(cacheKey)
	}

	tokenURI := strings.TrimSpace(creds.TokenURI)
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	signedJWT, err := createSignedJWT(creds.ClientEmail, creds.PrivateKey, tokenURI)
	if err != nil {
		return "", fmt.Errorf("failed to create signed JWT: %w", err)
	}

	token, expiresIn, err := exchangeJWTForAccessToken(signedJWT, tokenURI, info)
	if err != nil {
		return "", err
	}

	if expiresIn <= 0 {
		expiresIn = 3600
	}
	accessTokenCache.Store(cacheKey, cachedAccessToken{
		Token:     token,
		ExpiresAt: time.Now().Add(time.Duration(expiresIn-300) * time.Second),
	})
	return token, nil
}

func createSignedJWT(email, privateKeyPEM, tokenURI string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return "", errors.New("service account client_email is required")
	}
	privateKey, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss":   email,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   tokenURI,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(privateKey)
}

func parseRSAPrivateKey(privateKeyPEM string) (*rsa.PrivateKey, error) {
	privateKeyPEM = strings.ReplaceAll(privateKeyPEM, "\\n", "\n")
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing the private key")
	}

	privateKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPrivateKey, ok := privateKey.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("service account private key is not RSA")
	}
	return rsaPrivateKey, nil
}

func exchangeJWTForAccessToken(signedJWT, tokenURI string, info *relaycommon.RelayInfo) (string, int, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", signedJWT)

	client, err := httpClient(info)
	if err != nil {
		return "", 0, err
	}

	resp, err := client.PostForm(tokenURI, form)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken      string `json:"access_token"`
		ExpiresIn        int    `json:"expires_in"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err = common.DecodeJson(resp.Body, &result); err != nil {
		return "", 0, err
	}
	if result.AccessToken != "" {
		return result.AccessToken, result.ExpiresIn, nil
	}
	if result.ErrorDescription != "" {
		return "", 0, errors.New(result.ErrorDescription)
	}
	if result.Error != "" {
		return "", 0, errors.New(result.Error)
	}
	return "", 0, errors.New("failed to get Google access token")
}

func httpClient(info *relaycommon.RelayInfo) (*http.Client, error) {
	if info != nil && info.ChannelSetting.Proxy != "" {
		client, err := service.NewProxyHttpClient(info.ChannelSetting.Proxy)
		if err != nil {
			return nil, fmt.Errorf("new proxy http client failed: %w", err)
		}
		return client, nil
	}
	return service.GetHttpClient(), nil
}
