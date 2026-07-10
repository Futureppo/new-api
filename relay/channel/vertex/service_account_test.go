package vertex

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

func newTestCredentials(t *testing.T) Credentials {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	return Credentials{
		Type:         "service_account",
		ProjectID:    "vertex-test-project",
		PrivateKeyID: "test-key-id",
		PrivateKey: string(pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: der,
		})),
		ClientEmail: "vertex-test@vertex-test-project.iam.gserviceaccount.com",
		TokenURI:    defaultGoogleTokenURI,
	}
}

func resetTokenState(t *testing.T) {
	t.Helper()
	originalExchange := exchangeAccessToken
	accessTokenCache = sync.Map{}
	accessTokenGroup = singleflight.Group{}
	t.Cleanup(func() {
		exchangeAccessToken = originalExchange
		accessTokenCache = sync.Map{}
		accessTokenGroup = singleflight.Group{}
	})
}

func TestCreateSignedJWTUsesCredentialAudienceAndKeyID(t *testing.T) {
	creds := newTestCredentials(t)
	now := time.Unix(1_700_000_000, 0)
	signed, err := createSignedJWT(creds, creds.TokenURI, now)
	require.NoError(t, err)

	privateKey, err := parseRSAPrivateKey(creds.PrivateKey)
	require.NoError(t, err)
	parsed, err := jwt.Parse(signed, func(token *jwt.Token) (any, error) {
		return &privateKey.PublicKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithTimeFunc(func() time.Time { return now }))
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	require.Equal(t, creds.PrivateKeyID, parsed.Header["kid"])
	claims := parsed.Claims.(jwt.MapClaims)
	require.Equal(t, creds.ClientEmail, claims["iss"])
	require.Equal(t, googleCloudScope, claims["scope"])
	require.Equal(t, creds.TokenURI, claims["aud"])
	require.Equal(t, float64(now.Unix()), claims["iat"])
	require.Equal(t, float64(now.Add(time.Hour).Unix()), claims["exp"])
}

func TestNormalizeTokenURIRejectsUntrustedEndpoints(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default", input: "", want: defaultGoogleTokenURI},
		{name: "standard", input: defaultGoogleTokenURI, want: defaultGoogleTokenURI},
		{name: "legacy", input: "https://www.googleapis.com/oauth2/v4/token", want: "https://www.googleapis.com/oauth2/v4/token"},
		{name: "http", input: "http://oauth2.googleapis.com/token", wantErr: true},
		{name: "foreign host", input: "https://example.com/token", wantErr: true},
		{name: "userinfo", input: "https://user@oauth2.googleapis.com/token", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeTokenURI(test.input)
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestAccessTokenCacheKeyChangesWhenCredentialRotates(t *testing.T) {
	creds := newTestCredentials(t)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
		ChannelId:            42,
		ChannelIsMultiKey:    true,
		ChannelMultiKeyIndex: 3,
	}}
	first := accessTokenCacheKey(creds, info)
	creds.PrivateKeyID = "rotated-key-id"
	second := accessTokenCacheKey(creds, info)
	require.NotEqual(t, first, second)
	require.Contains(t, first, "channel-42-key-3")
}

func TestAcquireAccessTokenCoalescesConcurrentRefresh(t *testing.T) {
	resetTokenState(t)
	creds := newTestCredentials(t)
	var exchanges atomic.Int32
	exchangeAccessToken = func(context.Context, string, string, string) (string, int, error) {
		exchanges.Add(1)
		time.Sleep(20 * time.Millisecond)
		return "shared-token", 3600, nil
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 7}}

	const workers = 12
	results := make(chan string, workers)
	errorsCh := make(chan error, workers)
	var waitGroup sync.WaitGroup
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			token, err := acquireAccessToken(context.Background(), creds, "", info)
			results <- token
			errorsCh <- err
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		require.NoError(t, err)
	}
	for token := range results {
		require.Equal(t, "shared-token", token)
	}
	require.Equal(t, int32(1), exchanges.Load())
}

func TestExchangeJWTForAccessToken(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, http.MethodPost, r.Method)
			require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
			require.NoError(t, r.ParseForm())
			require.Equal(t, "signed-jwt", r.Form.Get("assertion"))
			_, _ = w.Write([]byte(`{"access_token":"token-value","expires_in":1800,"token_type":"Bearer"}`))
		}))
		defer server.Close()
		token, expiresIn, err := exchangeJWTForAccessToken(context.Background(), "signed-jwt", server.URL, "")
		require.NoError(t, err)
		require.Equal(t, "token-value", token)
		require.Equal(t, 1800, expiresIn)
	})

	t.Run("structured error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"credential rejected"}`))
		}))
		defer server.Close()
		_, _, err := exchangeJWTForAccessToken(context.Background(), "signed-jwt", server.URL, "")
		require.ErrorContains(t, err, "credential rejected")
	})
}
