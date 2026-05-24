package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var modelRequestRateLimitTestUserId int64 = 1000

func setupModelRequestRateLimitTest(t *testing.T, group string) (*gin.Engine, int) {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)

	oldRedisEnabled := common.RedisEnabled
	oldEnabled := setting.ModelRequestRateLimitEnabled
	oldDuration := setting.ModelRequestRateLimitDurationMinutes
	oldTotalCount := setting.ModelRequestRateLimitCount
	oldSuccessCount := setting.ModelRequestRateLimitSuccessCount
	oldConcurrencyLimit := setting.ModelRequestConcurrencyLimit

	setting.ModelRequestRateLimitMutex.RLock()
	oldGroup := setting.ModelRequestRateLimitGroup
	setting.ModelRequestRateLimitMutex.RUnlock()

	userId := int(atomic.AddInt64(&modelRequestRateLimitTestUserId, 1))
	common.RedisEnabled = false
	setting.ModelRequestRateLimitEnabled = true
	setting.ModelRequestRateLimitDurationMinutes = 1
	setting.ModelRequestRateLimitCount = 0
	setting.ModelRequestRateLimitSuccessCount = 1000
	setting.ModelRequestConcurrencyLimit = 1

	modelRequestConcurrencyLimiter.Lock()
	modelRequestConcurrencyLimiter.counts = make(map[string]int)
	modelRequestConcurrencyLimiter.Unlock()

	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		setting.ModelRequestRateLimitEnabled = oldEnabled
		setting.ModelRequestRateLimitDurationMinutes = oldDuration
		setting.ModelRequestRateLimitCount = oldTotalCount
		setting.ModelRequestRateLimitSuccessCount = oldSuccessCount
		setting.ModelRequestConcurrencyLimit = oldConcurrencyLimit

		setting.ModelRequestRateLimitMutex.Lock()
		setting.ModelRequestRateLimitGroup = oldGroup
		setting.ModelRequestRateLimitMutex.Unlock()

		modelRequestConcurrencyLimiter.Lock()
		modelRequestConcurrencyLimiter.counts = make(map[string]int)
		modelRequestConcurrencyLimiter.Unlock()
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(constant.ContextKeyUserId), userId)
		if group != "" {
			common.SetContextKey(c, constant.ContextKeyUserGroup, group)
		}
		c.Next()
	})
	router.Use(ModelRequestRateLimit())

	return router, userId
}

func addBlockingHandler(router *gin.Engine) (chan struct{}, chan struct{}, chan int) {
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan int, 1)
	var active int32
	var firstOnce sync.Once

	router.GET("/", func(c *gin.Context) {
		current := atomic.AddInt32(&active, 1)
		defer atomic.AddInt32(&active, -1)

		if current == 1 {
			firstOnce.Do(func() {
				close(firstEntered)
				<-releaseFirst
			})
		}
		c.Status(http.StatusOK)
	})

	go func() {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		firstDone <- recorder.Code
	}()

	return firstEntered, releaseFirst, firstDone
}

func waitForFirstModelRequest(t *testing.T, firstEntered <-chan struct{}) {
	t.Helper()
	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first request to enter handler")
	}
}

func finishFirstModelRequest(t *testing.T, releaseFirst chan struct{}, firstDone <-chan int) {
	t.Helper()
	close(releaseFirst)

	select {
	case status := <-firstDone:
		require.Equal(t, http.StatusOK, status)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first request to finish")
	}
}

func TestModelRequestConcurrencyLimitRejectsSecondInMemoryRequest(t *testing.T) {
	router, _ := setupModelRequestRateLimitTest(t, "")
	firstEntered, releaseFirst, firstDone := addBlockingHandler(router)
	waitForFirstModelRequest(t, firstEntered)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)

	finishFirstModelRequest(t, releaseFirst, firstDone)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestModelRequestConcurrencyLimitCanBeDisabled(t *testing.T) {
	router, _ := setupModelRequestRateLimitTest(t, "")
	setting.ModelRequestConcurrencyLimit = 0
	firstEntered, releaseFirst, firstDone := addBlockingHandler(router)
	waitForFirstModelRequest(t, firstEntered)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	finishFirstModelRequest(t, releaseFirst, firstDone)
}

func TestModelRequestConcurrencyLimitUsesGroupOverride(t *testing.T) {
	router, _ := setupModelRequestRateLimitTest(t, "default")
	setting.ModelRequestConcurrencyLimit = 2
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{"default":[0,1000,1]}`))
	firstEntered, releaseFirst, firstDone := addBlockingHandler(router)
	waitForFirstModelRequest(t, firstEntered)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)

	finishFirstModelRequest(t, releaseFirst, firstDone)
}

func TestModelRequestConcurrencyLimitUsesGlobalForLegacyGroupConfig(t *testing.T) {
	router, _ := setupModelRequestRateLimitTest(t, "default")
	setting.ModelRequestConcurrencyLimit = 1
	require.NoError(t, setting.UpdateModelRequestRateLimitGroupByJSONString(`{"default":[0,1000]}`))
	firstEntered, releaseFirst, firstDone := addBlockingHandler(router)
	waitForFirstModelRequest(t, firstEntered)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)

	finishFirstModelRequest(t, releaseFirst, firstDone)
}

func TestModelRequestConcurrencySlotReleasedAfterError(t *testing.T) {
	router, _ := setupModelRequestRateLimitTest(t, "")
	var requestCount int32
	router.GET("/", func(c *gin.Context) {
		count := atomic.AddInt32(&requestCount, 1)
		if c.Query("status") != "" {
			status, err := strconv.Atoi(c.Query("status"))
			require.NoError(t, err)
			c.Status(status)
			return
		}
		c.String(http.StatusOK, strconv.Itoa(int(count)))
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/?status=500", nil))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
}
