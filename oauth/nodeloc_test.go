package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/require"
)

type nodeLocTestTransport func(*http.Request) (*http.Response, error)

func (f nodeLocTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func nodeLocTestProvider(t *testing.T, handler http.HandlerFunc) *NodeLocProvider {
	t.Helper()
	original := *system_setting.GetNodeLocSettings()
	originalAddress := system_setting.ServerAddress
	*system_setting.GetNodeLocSettings() = system_setting.NodeLocSettings{Enabled: true, ClientId: "client-id", ClientSecret: "client-secret"}
	system_setting.ServerAddress = "https://gateway.example///"
	t.Cleanup(func() {
		*system_setting.GetNodeLocSettings() = original
		system_setting.ServerAddress = originalAddress
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	transport := client.Transport
	client.Transport = nodeLocTestTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "www.nodeloc.com", r.URL.Host)
		r = r.Clone(r.Context())
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(server.URL, "http://")
		return transport.RoundTrip(r)
	})
	return &NodeLocProvider{client: client}
}

func TestNodeLocExchangeAndUserInfo(t *testing.T) {
	for _, level := range []int{0, 1, 2, 3, 4} {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			provider := nodeLocTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth-provider/token":
					require.Equal(t, http.MethodPost, r.Method)
					require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
					require.Empty(t, r.Header.Get("Authorization"))
					require.NoError(t, r.ParseForm())
					require.Len(t, r.PostForm, 5)
					for key, value := range map[string]string{"grant_type": "authorization_code", "code": "code&+?", "client_id": "client-id", "client_secret": "client-secret", "redirect_uri": "https://gateway.example/oauth/nodeloc"} {
						require.Equal(t, value, r.PostForm.Get(key))
					}
					fmt.Fprint(w, `{"access_token":"test-token","refresh_token":"ignored","id_token":"ignored","expires_in":7200}`)
				case "/oauth-provider/userinfo":
					require.Equal(t, http.MethodGet, r.Method)
					require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
					fmt.Fprintf(w, `{"id":9007199254740993,"username":"node-user","name":"Node User","email":"ignore@example.com","trust_level":%d}`, level)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
				}
			})
			token, err := provider.ExchangeToken(context.Background(), "code&+?", nil)
			require.NoError(t, err)
			require.Empty(t, token.RefreshToken)
			require.Empty(t, token.IDToken)
			user, err := provider.GetUserInfo(context.Background(), token)
			require.NoError(t, err)
			require.Equal(t, "9007199254740993", user.ProviderUserID)
			require.Equal(t, "node-user", user.Username)
			require.Equal(t, "Node User", user.DisplayName)
			require.Empty(t, user.Email)
		})
	}
}

func TestNodeLocRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		token  bool
	}{
		{"token http error", 401, `{"access_token":"private"}`, true},
		{"token invalid json", 200, `not-json-private`, true},
		{"empty token", 200, `{"access_token":" "}`, true},
		{"oauth token error", 200, `{"error":"invalid_grant","error_description":"private"}`, true},
		{"user http error", 403, `{"id":1}`, false},
		{"user invalid json", 200, `private`, false},
		{"missing id", 200, `{"username":"name"}`, false},
		{"zero id", 200, `{"id":0}`, false},
		{"negative id", 200, `{"id":-1}`, false},
		{"fractional id", 200, `{"id":1.2}`, false},
		{"string id", 200, `{"id":"123"}`, false},
		{"user oauth error", 200, `{"id":1,"error":"invalid_token"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := nodeLocTestProvider(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) })
			var err error
			if tc.token {
				_, err = p.ExchangeToken(context.Background(), "code", nil)
			} else {
				_, err = p.GetUserInfo(context.Background(), &OAuthToken{AccessToken: "token"})
			}
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
		})
	}
}

func TestNodeLocTimeoutAndMissingInputs(t *testing.T) {
	p := nodeLocTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	})
	p.client.Timeout = 20 * time.Millisecond
	_, err := p.ExchangeToken(context.Background(), "code", nil)
	require.Error(t, err)
	require.Equal(t, i18n.MsgOAuthConnectFailed, err.(*OAuthError).MsgKey)
	_, err = p.ExchangeToken(context.Background(), "", nil)
	require.Error(t, err)
	_, err = p.GetUserInfo(context.Background(), nil)
	require.Error(t, err)
	system_setting.ServerAddress = ""
	_, err = p.ExchangeToken(context.Background(), "code", nil)
	require.Equal(t, i18n.MsgNodeLocConfigInvalid, err.(*OAuthError).MsgKey)
}
