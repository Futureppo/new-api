package vertex

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
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
	"golang.org/x/sync/singleflight"
)

const (
	defaultGoogleTokenURI = "https://oauth2.googleapis.com/token"
	googleCloudScope      = "https://www.googleapis.com/auth/cloud-platform"
	tokenRefreshSkew      = 5 * time.Minute
)

type Credentials struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	ClientID     string `json:"client_id"`
	TokenURI     string `json:"token_uri"`
}

type cachedAccessToken struct {
	Token     string
	ExpiresAt time.Time
}

type googleTokenResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	TokenType        string `json:"token_type"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

var (
	accessTokenCache    sync.Map
	accessTokenGroup    singleflight.Group
	exchangeAccessToken = exchangeJWTForAccessToken
)

func ParseCredentials(raw string) (Credentials, error) {
	var creds Credentials
	if err := common.UnmarshalJsonStr(strings.TrimSpace(raw), &creds); err != nil {
		return Credentials{}, fmt.Errorf("failed to decode credentials file: %w", err)
	}
	if err := validateCredentials(creds); err != nil {
		return Credentials{}, err
	}
	return creds, nil
}

func validateCredentials(creds Credentials) error {
	if strings.TrimSpace(creds.ProjectID) == "" {
		return errors.New("service account project_id is required")
	}
	if strings.TrimSpace(creds.ClientEmail) == "" {
		return errors.New("service account client_email is required")
	}
	if strings.TrimSpace(creds.PrivateKey) == "" {
		return errors.New("service account private_key is required")
	}
	_, err := normalizeTokenURI(creds.TokenURI)
	return err
}

func normalizeTokenURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultGoogleTokenURI, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("invalid service account token_uri")
	}
	if u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("service account token_uri must be an official Google HTTPS endpoint")
	}
	host := strings.ToLower(u.Hostname())
	path := strings.TrimRight(u.EscapedPath(), "/")
	valid := (host == "oauth2.googleapis.com" && path == "/token") ||
		(host == "www.googleapis.com" && path == "/oauth2/v4/token")
	if !valid || u.Port() != "" {
		return "", errors.New("service account token_uri must be an official Google OAuth endpoint")
	}
	return u.String(), nil
}

func credentialFingerprint(creds Credentials) string {
	value := strings.Join([]string{
		strings.TrimSpace(creds.ProjectID),
		strings.TrimSpace(creds.ClientEmail),
		strings.TrimSpace(creds.PrivateKeyID),
		strings.TrimSpace(creds.TokenURI),
		creds.PrivateKey,
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func accessTokenCacheKey(creds Credentials, info *relaycommon.RelayInfo) string {
	prefix := "standalone"
	if info != nil && info.ChannelMeta != nil {
		prefix = fmt.Sprintf("channel-%d", info.ChannelId)
		if info.ChannelIsMultiKey {
			prefix = fmt.Sprintf("%s-key-%d", prefix, info.ChannelMultiKeyIndex)
		}
	}
	return "vertex-access-token-" + prefix + "-" + credentialFingerprint(creds)
}

func getCachedAccessToken(key string, now time.Time) (string, bool) {
	value, ok := accessTokenCache.Load(key)
	if !ok {
		return "", false
	}
	cached, ok := value.(cachedAccessToken)
	if !ok || cached.Token == "" || !now.Before(cached.ExpiresAt) {
		accessTokenCache.Delete(key)
		return "", false
	}
	return cached.Token, true
}

func cacheAccessToken(key string, token string, expiresIn int, now time.Time) {
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	ttl := time.Duration(expiresIn) * time.Second
	if ttl > tokenRefreshSkew {
		ttl -= tokenRefreshSkew
	} else {
		ttl /= 2
	}
	if ttl <= 0 {
		return
	}
	pruneExpiredAccessTokens(now)
	accessTokenCache.Store(key, cachedAccessToken{Token: token, ExpiresAt: now.Add(ttl)})
}

func pruneExpiredAccessTokens(now time.Time) {
	accessTokenCache.Range(func(key, value any) bool {
		cached, ok := value.(cachedAccessToken)
		if !ok || cached.Token == "" || !now.Before(cached.ExpiresAt) {
			accessTokenCache.Delete(key)
		}
		return true
	})
}

func acquireAccessToken(ctx context.Context, creds Credentials, proxy string, info *relaycommon.RelayInfo) (string, error) {
	if err := validateCredentials(creds); err != nil {
		return "", err
	}
	cacheKey := accessTokenCacheKey(creds, info)
	if token, ok := getCachedAccessToken(cacheKey, time.Now()); ok {
		return token, nil
	}

	value, err, _ := accessTokenGroup.Do(cacheKey, func() (any, error) {
		now := time.Now()
		if token, ok := getCachedAccessToken(cacheKey, now); ok {
			return token, nil
		}
		tokenURI, err := normalizeTokenURI(creds.TokenURI)
		if err != nil {
			return "", err
		}
		signedJWT, err := createSignedJWT(creds, tokenURI, now)
		if err != nil {
			return "", fmt.Errorf("failed to create signed JWT: %w", err)
		}
		token, expiresIn, err := exchangeAccessToken(ctx, signedJWT, tokenURI, proxy)
		if err != nil {
			return "", fmt.Errorf("failed to exchange JWT for access token: %w", err)
		}
		cacheAccessToken(cacheKey, token, expiresIn, now)
		return token, nil
	})
	if err != nil {
		return "", err
	}
	return value.(string), nil
}

func getAccessToken(ctx context.Context, a *Adaptor, info *relaycommon.RelayInfo) (string, error) {
	if a == nil {
		return "", errors.New("vertex adaptor is nil")
	}
	proxy := ""
	if info != nil {
		proxy = info.ChannelSetting.Proxy
	}
	return AcquireAccessTokenForRelayContext(ctx, a.AccountCredentials, proxy, info)
}

func AcquireAccessToken(creds Credentials, proxy string) (string, error) {
	return AcquireAccessTokenContext(context.Background(), creds, proxy)
}

func AcquireAccessTokenContext(ctx context.Context, creds Credentials, proxy string) (string, error) {
	return acquireAccessToken(ctx, creds, proxy, nil)
}

func AcquireAccessTokenForRelayContext(ctx context.Context, creds Credentials, proxy string, info *relaycommon.RelayInfo) (string, error) {
	return acquireAccessToken(ctx, creds, proxy, info)
}

func createSignedJWT(creds Credentials, tokenURI string, now time.Time) (string, error) {
	privateKey, err := parseRSAPrivateKey(creds.PrivateKey)
	if err != nil {
		return "", err
	}
	claims := jwt.MapClaims{
		"iss":   strings.TrimSpace(creds.ClientEmail),
		"scope": googleCloudScope,
		"aud":   tokenURI,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if keyID := strings.TrimSpace(creds.PrivateKeyID); keyID != "" {
		token.Header["kid"] = keyID
	}
	return token.SignedString(privateKey)
}

func parseRSAPrivateKey(privateKeyPEM string) (*rsa.PrivateKey, error) {
	privateKeyPEM = strings.ReplaceAll(strings.TrimSpace(privateKeyPEM), "\\n", "\n")
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

func exchangeJWTForAccessToken(ctx context.Context, signedJWT, tokenURI, proxy string) (string, int, error) {
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", signedJWT)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client, err := vertexHTTPClient(proxy)
	if err != nil {
		return "", 0, fmt.Errorf("new proxy http client failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var result googleTokenResponse
	if err := common.DecodeJson(resp.Body, &result); err != nil {
		return "", 0, fmt.Errorf("failed to decode Google OAuth response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if result.ErrorDescription != "" {
			return "", 0, errors.New(result.ErrorDescription)
		}
		if result.Error != "" {
			return "", 0, errors.New(result.Error)
		}
		return "", 0, fmt.Errorf("Google OAuth returned HTTP %d", resp.StatusCode)
	}
	if strings.TrimSpace(result.AccessToken) == "" {
		return "", 0, errors.New("Google OAuth response did not include access_token")
	}
	return result.AccessToken, result.ExpiresIn, nil
}

func vertexHTTPClient(proxy string) (*http.Client, error) {
	if strings.TrimSpace(proxy) == "" {
		if client := service.GetHttpClient(); client != nil {
			return client, nil
		}
		return http.DefaultClient, nil
	}
	return service.GetHttpClientWithProxy(proxy)
}
