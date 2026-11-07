package middleware

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckinRateLimitSeparatesUsersAndIsConcurrentSafe(t *testing.T) {
	old := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = old })
	gin.SetMode(gin.TestMode)
	route := gin.New()
	route.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-User") == "second" {
			c.Set("id", 97502)
		} else {
			c.Set("id", 97501)
		}
	})
	route.POST("/", CheckinRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			route.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
			if w.Code == http.StatusNoContent {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 5, accepted.Load())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-Test-User", "second")
	route.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestCheckinRateLimitRedisIsAtomicAndKeepsExpiry(t *testing.T) {
	socket := os.Getenv("NEWAPI_TEST_REDIS_SOCKET")
	if socket == "" {
		t.Skip("NEWAPI_TEST_REDIS_SOCKET is not set")
	}
	client := redis.NewClient(&redis.Options{Network: "unix", Addr: socket})
	require.NoError(t, client.Ping(context.Background()).Err())
	oldEnabled, oldClient := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() { common.RedisEnabled, common.RDB = oldEnabled, oldClient; _ = client.Close() })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 97503) })
	router.POST("/", CheckinRateLimit(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	key := "rateLimit:checkin:v2:user:97503"
	require.NoError(t, client.Del(context.Background(), key).Err())
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
			if w.Code == http.StatusNoContent {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 5, accepted.Load())
	ttl, err := client.TTL(context.Background(), key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, time.Minute)
}
