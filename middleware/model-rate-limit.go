package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
	ModelRequestRateLimitConcurrencyMark  = "MRRLC"

	modelRequestConcurrencyLease           = 30 * time.Second
	modelRequestConcurrencyRefreshInterval = 10 * time.Second
	modelRequestConcurrencyRedisTTL        = 60 * time.Second
)

const redisAcquireModelRequestConcurrencyScript = `
local key = KEYS[1]
local member = ARGV[1]
local now = tonumber(ARGV[2])
local expire_before = tonumber(ARGV[3])
local limit = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

redis.call("ZREMRANGEBYSCORE", key, "-inf", expire_before)
if redis.call("ZCARD", key) >= limit then
	redis.call("EXPIRE", key, ttl)
	return 0
end

redis.call("ZADD", key, now, member)
redis.call("EXPIRE", key, ttl)
return 1
`

const redisRefreshModelRequestConcurrencyScript = `
local key = KEYS[1]
local member = ARGV[1]
local now = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])

if redis.call("ZSCORE", key, member) then
	redis.call("ZADD", key, now, member)
	redis.call("EXPIRE", key, ttl)
	return 1
end

return 0
`

var modelRequestConcurrencyLimiter = struct {
	sync.Mutex
	counts map[string]int
}{
	counts: make(map[string]int),
}

func modelRequestConcurrencyRedisKey(userId string) string {
	return fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitConcurrencyMark, userId)
}

func modelRequestConcurrencyMemoryKey(userId string) string {
	return ModelRequestRateLimitConcurrencyMark + userId
}

func modelRequestConcurrencyRequestId(c *gin.Context) string {
	requestId := c.GetString(common.RequestIdKey)
	if requestId == "" {
		requestId = common.GetUUID()
	}
	return requestId
}

func acquireRedisModelRequestConcurrency(ctx context.Context, rdb *redis.Client, userId string, maxCount int, requestId string) (func(), bool, error) {
	if maxCount == 0 {
		return func() {}, true, nil
	}

	key := modelRequestConcurrencyRedisKey(userId)
	now := time.Now().UnixMilli()
	leaseMillis := int64(modelRequestConcurrencyLease / time.Millisecond)
	ttlSeconds := int64(modelRequestConcurrencyRedisTTL / time.Second)

	allowed, err := rdb.Eval(
		ctx,
		redisAcquireModelRequestConcurrencyScript,
		[]string{key},
		requestId,
		now,
		now-leaseMillis,
		maxCount,
		ttlSeconds,
	).Int()
	if err != nil {
		return nil, false, err
	}
	if allowed != 1 {
		return nil, false, nil
	}

	refreshCtx, cancel := context.WithCancel(context.Background())
	var releaseOnce sync.Once
	go func() {
		ticker := time.NewTicker(modelRequestConcurrencyRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-ticker.C:
				_ = rdb.Eval(
					context.Background(),
					redisRefreshModelRequestConcurrencyScript,
					[]string{key},
					requestId,
					time.Now().UnixMilli(),
					ttlSeconds,
				).Err()
			}
		}
	}()

	release := func() {
		releaseOnce.Do(func() {
			cancel()
			_ = rdb.ZRem(context.Background(), key, requestId).Err()
		})
	}

	return release, true, nil
}

func acquireMemoryModelRequestConcurrency(userId string, maxCount int) (func(), bool) {
	if maxCount == 0 {
		return func() {}, true
	}

	key := modelRequestConcurrencyMemoryKey(userId)
	modelRequestConcurrencyLimiter.Lock()
	defer modelRequestConcurrencyLimiter.Unlock()

	if modelRequestConcurrencyLimiter.counts == nil {
		modelRequestConcurrencyLimiter.counts = make(map[string]int)
	}
	if modelRequestConcurrencyLimiter.counts[key] >= maxCount {
		return nil, false
	}
	modelRequestConcurrencyLimiter.counts[key]++

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			modelRequestConcurrencyLimiter.Lock()
			defer modelRequestConcurrencyLimiter.Unlock()

			if modelRequestConcurrencyLimiter.counts[key] <= 1 {
				delete(modelRequestConcurrencyLimiter.counts, key)
				return
			}
			modelRequestConcurrencyLimiter.counts[key]--
		})
	}

	return release, true
}

func rateLimitReachedMessage(c *gin.Context, maxCount int) string {
	return i18n.T(c, i18n.MsgRateLimitReached, map[string]any{
		"Minutes": setting.ModelRequestRateLimitDurationMinutes,
		"Max":     maxCount,
	})
}

func rateLimitTotalReachedMessage(c *gin.Context, maxCount int) string {
	return i18n.T(c, i18n.MsgRateLimitTotalReached, map[string]any{
		"Minutes": setting.ModelRequestRateLimitDurationMinutes,
		"Max":     maxCount,
	})
}

func rateLimitConcurrencyReachedMessage(c *gin.Context, maxCount int) string {
	return i18n.T(c, i18n.MsgRateLimitConcurrencyReached, map[string]any{
		"Max": maxCount,
	})
}

// 检查Redis中的请求限制
func checkRedisRateLimit(ctx context.Context, rdb *redis.Client, key string, maxCount int, duration int64) (bool, error) {
	// 如果maxCount为0，表示不限制
	if maxCount == 0 {
		return true, nil
	}

	// 获取当前计数
	length, err := rdb.LLen(ctx, key).Result()
	if err != nil {
		return false, err
	}

	// 如果未达到限制，允许请求
	if length < int64(maxCount) {
		return true, nil
	}

	// 检查时间窗口
	oldTimeStr, _ := rdb.LIndex(ctx, key, -1).Result()
	oldTime, err := time.Parse(timeFormat, oldTimeStr)
	if err != nil {
		return false, err
	}

	nowTimeStr := time.Now().Format(timeFormat)
	nowTime, err := time.Parse(timeFormat, nowTimeStr)
	if err != nil {
		return false, err
	}
	// 如果在时间窗口内已达到限制，拒绝请求
	subTime := nowTime.Sub(oldTime).Seconds()
	if int64(subTime) < duration {
		rdb.Expire(ctx, key, time.Duration(setting.ModelRequestRateLimitDurationMinutes)*time.Minute)
		return false, nil
	}

	return true, nil
}

// 记录Redis请求
func recordRedisRequest(ctx context.Context, rdb *redis.Client, key string, maxCount int) {
	// 如果maxCount为0，不记录请求
	if maxCount == 0 {
		return
	}

	now := time.Now().Format(timeFormat)
	rdb.LPush(ctx, key, now)
	rdb.LTrim(ctx, key, 0, int64(maxCount-1))
	rdb.Expire(ctx, key, time.Duration(setting.ModelRequestRateLimitDurationMinutes)*time.Minute)
}

// Redis限流处理器
func redisRateLimitHandler(duration int64, totalMaxCount, successMaxCount, concurrencyMaxCount int) gin.HandlerFunc {
	return func(c *gin.Context) {
		userId := strconv.Itoa(c.GetInt("id"))
		ctx := context.Background()
		rdb := common.RDB

		release, allowed, err := acquireRedisModelRequestConcurrency(ctx, rdb, userId, concurrencyMaxCount, modelRequestConcurrencyRequestId(c))
		if err != nil {
			fmt.Println("检查并发请求数限制失败:", err.Error())
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if !allowed {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, rateLimitConcurrencyReachedMessage(c, concurrencyMaxCount))
			return
		}
		defer release()

		// 1. 检查成功请求数限制
		successKey := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitSuccessCountMark, userId)
		allowed, err = checkRedisRateLimit(ctx, rdb, successKey, successMaxCount, duration)
		if err != nil {
			fmt.Println("检查成功请求数限制失败:", err.Error())
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
			return
		}
		if !allowed {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, rateLimitReachedMessage(c, successMaxCount))
			return
		}

		//2.检查总请求数限制并记录总请求（当totalMaxCount为0时会自动跳过，使用令牌桶限流器
		if totalMaxCount > 0 {
			totalKey := fmt.Sprintf("rateLimit:%s", userId)
			// 初始化
			tb := limiter.New(ctx, rdb)
			allowed, err = tb.Allow(
				ctx,
				totalKey,
				limiter.WithCapacity(int64(totalMaxCount)*duration),
				limiter.WithRate(int64(totalMaxCount)),
				limiter.WithRequested(duration),
			)

			if err != nil {
				fmt.Println("检查总请求数限制失败:", err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}

			if !allowed {
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, rateLimitTotalReachedMessage(c, totalMaxCount))
				return
			}
		}

		// 4. 处理请求
		c.Next()

		// 5. 如果请求成功，记录成功请求
		if c.Writer.Status() < 400 {
			recordRedisRequest(ctx, rdb, successKey, successMaxCount)
		}
	}
}

// 内存限流处理器
func memoryRateLimitHandler(duration int64, totalMaxCount, successMaxCount, concurrencyMaxCount int) gin.HandlerFunc {
	inMemoryRateLimiter.Init(time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute)

	return func(c *gin.Context) {
		userId := strconv.Itoa(c.GetInt("id"))
		totalKey := ModelRequestRateLimitCountMark + userId
		successKey := ModelRequestRateLimitSuccessCountMark + userId

		release, allowed := acquireMemoryModelRequestConcurrency(userId, concurrencyMaxCount)
		if !allowed {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, rateLimitConcurrencyReachedMessage(c, concurrencyMaxCount))
			return
		}
		defer release()

		// 1. 检查总请求数限制（当totalMaxCount为0时跳过）
		if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, rateLimitTotalReachedMessage(c, totalMaxCount))
			return
		}

		// 2. 检查成功请求数限制
		// 使用一个临时key来检查限制，这样可以避免实际记录
		checkKey := successKey + "_check"
		if !inMemoryRateLimiter.Request(checkKey, successMaxCount, duration) {
			abortWithOpenAiMessage(c, http.StatusTooManyRequests, rateLimitReachedMessage(c, successMaxCount))
			return
		}

		// 3. 处理请求
		c.Next()

		// 4. 如果请求成功，记录到实际的成功请求计数中
		if c.Writer.Status() < 400 {
			inMemoryRateLimiter.Request(successKey, successMaxCount, duration)
		}
	}
}

// ModelRequestRateLimit 模型请求限流中间件
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 在每个请求时检查是否启用限流
		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}

		// 计算限流参数
		duration := int64(setting.ModelRequestRateLimitDurationMinutes * 60)
		totalMaxCount := setting.ModelRequestRateLimitCount
		successMaxCount := setting.ModelRequestRateLimitSuccessCount
		concurrencyMaxCount := setting.ModelRequestConcurrencyLimit

		// 获取分组
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		//获取分组的限流配置
		groupTotalCount, groupSuccessCount, groupConcurrencyCount, hasConcurrencyLimit, found := setting.GetGroupRateLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
			if hasConcurrencyLimit {
				concurrencyMaxCount = groupConcurrencyCount
			}
		}

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisRateLimitHandler(duration, totalMaxCount, successMaxCount, concurrencyMaxCount)(c)
		} else {
			memoryRateLimitHandler(duration, totalMaxCount, successMaxCount, concurrencyMaxCount)(c)
		}
	}
}
