package controller

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Exercise the documented token/userinfo contract through the HTTP callback,
// including the signed session cookie sent across the authorization round trip.
func TestNodeLocHTTPSessionRoundTrip(t *testing.T) {
	setupNodeLocController(t)
	var upstreamCalls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/oauth-provider/token":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "authorization_code", r.PostForm.Get("grant_type"))
			require.Equal(t, "client", r.PostForm.Get("client_id"))
			require.Equal(t, "SECRET-MUST-NOT-LEAK", r.PostForm.Get("client_secret"))
			require.Equal(t, "https://gateway.example/oauth/nodeloc", r.PostForm.Get("redirect_uri"))
			require.Equal(t, "code&+?", r.PostForm.Get("code"))
			fmt.Fprint(w, `{"access_token":"private-token","token_type":"Bearer","expires_in":7200,"scope":"openid profile"}`)
		case "/oauth-provider/userinfo":
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "Bearer private-token", r.Header.Get("Authorization"))
			fmt.Fprint(w, `{"id":456,"username":"node-http","name":"Node HTTP","trust_level":1,"email":"ignored@example.com"}`)
		default:
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(upstream.Close)
	originalTransport := http.DefaultTransport
	http.DefaultTransport = nodeLocControllerTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "www.nodeloc.com", r.URL.Host)
		r = r.Clone(r.Context())
		r.URL.Scheme = "http"
		r.URL.Host = strings.TrimPrefix(upstream.URL, "http://")
		return originalTransport.RoundTrip(r)
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	store := cookie.NewStore([]byte("nodeloc-http-test-session-key"))
	store.Options(sessions.Options{Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	engine := gin.New()
	engine.Use(sessions.Sessions("session", store))
	engine.GET("/api/oauth/state", GenerateOAuthCode)
	engine.GET("/api/oauth/:provider", HandleOAuth)
	engine.GET("/api/user/logout", Logout)
	engine.GET("/session", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": sessions.Default(c).Get("id")})
	})
	app := httptest.NewServer(engine)
	t.Cleanup(app.Close)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	browser := &http.Client{Transport: originalTransport, Jar: jar}
	stranger := &http.Client{Transport: originalTransport}
	get := func(client *http.Client, path string) (int, map[string]any) {
		t.Helper()
		res, err := client.Get(app.URL + path)
		require.NoError(t, err)
		defer res.Body.Close()
		var data map[string]any
		require.NoError(t, common.DecodeJson(res.Body, &data))
		return res.StatusCode, data
	}
	state := func() string {
		t.Helper()
		status, res := get(browser, "/api/oauth/state")
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, true, res["success"])
		return res["data"].(string)
	}
	callback := func(state string) string {
		return "/api/oauth/nodeloc?" + url.Values{"state": {state}, "code": {"code&+?"}}.Encode()
	}

	firstState := state()
	status, _ := get(stranger, callback(firstState))
	require.Equal(t, http.StatusForbidden, status)
	status, _ = get(browser, callback("wrong-state"))
	require.Equal(t, http.StatusForbidden, status)
	require.Zero(t, upstreamCalls.Load())
	_, denied := get(browser, "/api/oauth/nodeloc?"+url.Values{"state": {firstState}, "error": {"access_denied"}}.Encode())
	require.Equal(t, false, denied["success"])
	require.Zero(t, upstreamCalls.Load())

	_, first := get(browser, callback(state()))
	require.Equal(t, true, first["success"], first)
	id := first["data"].(map[string]any)["id"]
	_, session := get(browser, "/session")
	require.Equal(t, id, session["data"])
	_, loggedOut := get(browser, "/api/user/logout")
	require.Equal(t, true, loggedOut["success"])
	_, session = get(browser, "/session")
	require.Nil(t, session["data"])
	_, second := get(browser, callback(state()))
	require.Equal(t, true, second["success"], second)
	require.Equal(t, id, second["data"].(map[string]any)["id"])
	require.EqualValues(t, 4, upstreamCalls.Load())
	var users []model.User
	require.NoError(t, model.DB.Where("nodeloc_id = ?", "456").Find(&users).Error)
	require.Len(t, users, 1)
	require.Empty(t, users[0].Email)
	for _, result := range []map[string]any{first, second, session} {
		encoded, err := common.Marshal(result)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-token")
		require.NotContains(t, string(encoded), "SECRET-MUST-NOT-LEAK")
	}
}
