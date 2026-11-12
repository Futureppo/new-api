package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type turnstileTestTransport func(*http.Request) (*http.Response, error)

func (f turnstileTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func configureTurnstileTest(t *testing.T, transport turnstileTestTransport) {
	t.Helper()
	previousEnabled, previousSecret, previousClient := common.TurnstileCheckEnabled, common.TurnstileSecretKey, turnstileHTTPClient
	common.TurnstileCheckEnabled, common.TurnstileSecretKey = true, "test-only-secret"
	turnstileHTTPClient = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	t.Cleanup(func() {
		common.TurnstileCheckEnabled, common.TurnstileSecretKey, turnstileHTTPClient = previousEnabled, previousSecret, previousClient
	})
}

func turnstileTestResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestTurnstileEmailRequiresFreshVerificationDespiteApprovedSession(t *testing.T) {
	var calls, sends atomic.Int32
	var lock sync.Mutex
	used := map[string]bool{}
	configureTurnstileTest(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "https://challenges.cloudflare.com/turnstile/v0/siteverify", r.URL.String())
		require.NoError(t, r.ParseForm())
		require.Equal(t, "test-only-secret", r.Form.Get("secret"))
		deadline, ok := r.Context().Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), 10*time.Second)
		token := r.Form.Get("response")
		lock.Lock()
		defer lock.Unlock()
		if token == "expired" || used[token] {
			return turnstileTestResponse(200, `{"success":false}`), nil
		}
		used[token] = true
		return turnstileTestResponse(200, `{"success":true}`), nil
	})
	r := gin.New()
	r.Use(sessions.Sessions("test-session", cookie.NewStore([]byte("test-only-cookie-signing-secret"))))
	r.GET("/seed", func(c *gin.Context) {
		s := sessions.Default(c)
		s.Set("turnstile", true)
		require.NoError(t, s.Save())
		c.Status(204)
	})
	handler := func(c *gin.Context) { sends.Add(1); c.Status(http.StatusAccepted) }
	r.GET("/verification", TurnstileCheckPerRequest(), handler)
	r.GET("/reset_password", TurnstileCheckPerRequest(), handler)
	r.GET("/ordinary", TurnstileCheck(), func(c *gin.Context) { c.Status(http.StatusAccepted) })
	seed := httptest.NewRecorder()
	r.ServeHTTP(seed, httptest.NewRequest(http.MethodGet, "/seed", nil))
	sessionCookie := seed.Result().Cookies()[0]
	request := func(path, token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path+"?turnstile="+url.QueryEscape(token), nil)
		req.AddCookie(sessionCookie)
		r.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/verification", "/reset_password"} {
		w := request(path, "")
		require.NotEqual(t, http.StatusAccepted, w.Code)
		require.Contains(t, w.Body.String(), `"success":false`)
	}
	require.Zero(t, calls.Load())
	require.Zero(t, sends.Load())
	require.Equal(t, http.StatusAccepted, request("/ordinary", "").Code)
	require.Zero(t, calls.Load(), "other flows retain the existing session behavior")
	require.Equal(t, http.StatusAccepted, request("/verification", "fresh-one").Code)
	require.Contains(t, request("/verification", "fresh-one").Body.String(), `"success":false`)
	require.Contains(t, request("/reset_password", "fresh-one").Body.String(), `"success":false`)
	require.Contains(t, request("/verification", "expired").Body.String(), `"success":false`)
	require.Equal(t, http.StatusAccepted, request("/reset_password", "fresh-two").Code)
	require.Equal(t, int32(2), sends.Load())
	// A concurrent duplicate can reach siteverify, but cannot send two emails.
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); request("/verification", "fresh-concurrent") }()
	}
	wg.Wait()
	require.Equal(t, int32(3), sends.Load())
	require.Equal(t, int32(7), calls.Load())
}

func TestTurnstileEmailFailsClosedOnVerificationServiceErrors(t *testing.T) {
	for _, scenario := range []string{"bad-json", "http-error", "timeout", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			configureTurnstileTest(t, func(r *http.Request) (*http.Response, error) {
				switch scenario {
				case "bad-json":
					return turnstileTestResponse(200, "invalid"), nil
				case "http-error":
					return turnstileTestResponse(503, `{"success":true}`), nil
				case "timeout":
					return nil, context.DeadlineExceeded
				default:
					require.ErrorIs(t, r.Context().Err(), context.Canceled)
					return nil, r.Context().Err()
				}
			})
			r := gin.New()
			r.GET("/email", TurnstileCheckPerRequest(), func(c *gin.Context) { t.Error("email handler ran after verification failed") })
			req := httptest.NewRequest(http.MethodGet, "/email?turnstile=test-token", nil)
			if scenario == "canceled" {
				ctx, cancel := context.WithCancel(req.Context())
				cancel()
				req = req.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Contains(t, w.Body.String(), `"success":false`)
		})
	}
}

func TestTurnstileEmailHonorsDisabledSetting(t *testing.T) {
	configureTurnstileTest(t, func(*http.Request) (*http.Response, error) { t.Fatal("unexpected verification call"); return nil, nil })
	common.TurnstileCheckEnabled = false
	r := gin.New()
	r.GET("/email", TurnstileCheckPerRequest(), func(c *gin.Context) { c.Status(202) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/email", nil))
	require.Equal(t, 202, w.Code)
}
