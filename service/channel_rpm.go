package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
)

const (
	channelRPMWindow                    = time.Minute
	channelRPMRequestTTL                = 2 * time.Minute
	channelRPMDefaultLimit              = 1000
	channelRPMDefaultPercent            = 60
	channelRPMDefaultRampTime           = 5 * time.Minute
	ChannelRPMLimitExceededMessage      = "当前渠道 RPM 已达到保护上限"
	ChannelRPMGroupLimitExceededMessage = "当前分组可用渠道均已达到 RPM 保护上限"
)

var ErrChannelRPMLimitExceeded = errors.New("channel RPM protection limit exceeded")

type channelRPMLimitError struct {
	message string
}

func (e channelRPMLimitError) Error() string {
	return e.message
}

func (e channelRPMLimitError) Unwrap() error {
	return ErrChannelRPMLimitExceeded
}

type ChannelRPMAcquireResult struct {
	Allowed        bool
	CurrentRPM     int
	EffectiveLimit int
}

type channelRPMConfig struct {
	limit            int
	thresholdPercent int
	ramp             time.Duration
}

type memoryChannelRPMState struct {
	requests       []int64
	protectionCap  int
	protectionTime int64
}

var channelRPMMemory = struct {
	sync.Mutex
	channels map[int]*memoryChannelRPMState
}{channels: make(map[int]*memoryChannelRPMState)}

const channelRPMAcquireRedisScript = `
local requests_key = KEYS[1]
local state_key = KEYS[2]
local limit = tonumber(ARGV[1])
local ramp_ms = tonumber(ARGV[2])
local member = ARGV[3]
local requests_ttl = tonumber(ARGV[4])

local redis_time = redis.call('TIME')
local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', requests_key, '-inf', now_ms - 60000)

local effective = limit
local start_cap = tonumber(redis.call('HGET', state_key, 'start_cap'))
local start_ms = tonumber(redis.call('HGET', state_key, 'start_ms'))
if start_cap and start_ms then
  local elapsed = now_ms - start_ms
  if elapsed >= ramp_ms then
    redis.call('DEL', state_key)
  else
    if start_cap < 1 then start_cap = 1 end
    if start_cap > limit then start_cap = limit end
    if elapsed < 0 then elapsed = 0 end
    effective = math.floor(start_cap + ((limit - start_cap) * elapsed / ramp_ms))
    if effective < 1 then effective = 1 end
  end
end

local current = tonumber(redis.call('ZCARD', requests_key))
if current >= effective then
  redis.call('EXPIRE', requests_key, requests_ttl)
  return {0, current, effective}
end

redis.call('ZADD', requests_key, now_ms, member)
redis.call('EXPIRE', requests_key, requests_ttl)
return {1, current + 1, effective}
`

const channelRPMRecord429RedisScript = `
local requests_key = KEYS[1]
local state_key = KEYS[2]
local limit = tonumber(ARGV[1])
local threshold = tonumber(ARGV[2])
local ramp_ms = tonumber(ARGV[3])
local state_ttl = tonumber(ARGV[4])

local redis_time = redis.call('TIME')
local now_ms = tonumber(redis_time[1]) * 1000 + math.floor(tonumber(redis_time[2]) / 1000)
redis.call('ZREMRANGEBYSCORE', requests_key, '-inf', now_ms - 60000)

local effective = limit
local old_cap = tonumber(redis.call('HGET', state_key, 'start_cap'))
local old_start_ms = tonumber(redis.call('HGET', state_key, 'start_ms'))
if old_cap and old_start_ms then
  local elapsed = now_ms - old_start_ms
  if elapsed < ramp_ms then
    if old_cap < 1 then old_cap = 1 end
    if old_cap > limit then old_cap = limit end
    if elapsed < 0 then elapsed = 0 end
    effective = math.floor(old_cap + ((limit - old_cap) * elapsed / ramp_ms))
    if effective < 1 then effective = 1 end
  end
end

local current = tonumber(redis.call('ZCARD', requests_key))
local reduced = math.floor(current * threshold / 100)
if reduced < 1 then reduced = 1 end
if reduced > limit then reduced = limit end
local start_cap = reduced
if effective < start_cap then start_cap = effective end

redis.call('HSET', state_key, 'start_cap', start_cap, 'start_ms', now_ms)
redis.call('EXPIRE', state_key, state_ttl)
return {current, start_cap, effective}
`

func normalizeChannelRPMConfig(settings *dto.ChannelRPMProtectionSettings) (channelRPMConfig, bool) {
	if settings == nil || !settings.Enabled || settings.RPMLimit == 0 {
		return channelRPMConfig{}, false
	}
	limit := settings.RPMLimit
	if limit < 0 {
		return channelRPMConfig{}, false
	}
	threshold := settings.ProtectionThresholdPercent
	if threshold == 0 {
		threshold = channelRPMDefaultPercent
	}
	ramp := time.Duration(settings.RampMinutes) * time.Minute
	if ramp <= 0 {
		ramp = channelRPMDefaultRampTime
	}
	return channelRPMConfig{limit: limit, thresholdPercent: threshold, ramp: ramp}, true
}

func channelRPMRedisKeys(channelID int) (string, string) {
	tag := fmt.Sprintf("{%d}", channelID)
	return "channel_rpm:" + tag + ":requests", "channel_rpm:" + tag + ":protection"
}

func channelRPMEffectiveLimit(limit, startCap int, startMillis, nowMillis int64, ramp time.Duration) int {
	if startCap <= 0 || startMillis <= 0 || ramp <= 0 {
		return limit
	}
	elapsed := nowMillis - startMillis
	if elapsed >= ramp.Milliseconds() {
		return limit
	}
	if elapsed < 0 {
		elapsed = 0
	}
	startCap = min(max(startCap, 1), limit)
	effective := startCap + int((int64(limit-startCap)*elapsed)/ramp.Milliseconds())
	return min(max(effective, 1), limit)
}

func pruneChannelRPMRequests(requests []int64, nowMillis int64) []int64 {
	cutoff := nowMillis - channelRPMWindow.Milliseconds()
	first := 0
	for first < len(requests) && requests[first] <= cutoff {
		first++
	}
	if first == 0 {
		return requests
	}
	return append([]int64(nil), requests[first:]...)
}

func acquireMemoryChannelRPM(channelID int, cfg channelRPMConfig, nowMillis int64) ChannelRPMAcquireResult {
	channelRPMMemory.Lock()
	defer channelRPMMemory.Unlock()
	state := channelRPMMemory.channels[channelID]
	if state == nil {
		state = &memoryChannelRPMState{}
		channelRPMMemory.channels[channelID] = state
	}
	state.requests = pruneChannelRPMRequests(state.requests, nowMillis)
	effective := channelRPMEffectiveLimit(cfg.limit, state.protectionCap, state.protectionTime, nowMillis, cfg.ramp)
	if effective == cfg.limit && state.protectionCap > 0 && nowMillis-state.protectionTime >= cfg.ramp.Milliseconds() {
		state.protectionCap = 0
		state.protectionTime = 0
	}
	current := len(state.requests)
	if current >= effective {
		return ChannelRPMAcquireResult{Allowed: false, CurrentRPM: current, EffectiveLimit: effective}
	}
	state.requests = append(state.requests, nowMillis)
	return ChannelRPMAcquireResult{Allowed: true, CurrentRPM: current + 1, EffectiveLimit: effective}
}

func acquireRedisChannelRPM(ctx context.Context, channelID int, cfg channelRPMConfig) (ChannelRPMAcquireResult, error) {
	requestsKey, stateKey := channelRPMRedisKeys(channelID)
	result, err := common.RDB.Eval(ctx, channelRPMAcquireRedisScript, []string{requestsKey, stateKey},
		cfg.limit, cfg.ramp.Milliseconds(), common.GetUUID(), int(channelRPMRequestTTL/time.Second)).Slice()
	if err != nil {
		return ChannelRPMAcquireResult{}, err
	}
	if len(result) != 3 {
		return ChannelRPMAcquireResult{}, fmt.Errorf("unexpected channel RPM limiter response: %v", result)
	}
	values := make([]int, 3)
	for i, value := range result {
		switch v := value.(type) {
		case int64:
			values[i] = int(v)
		case string:
			if _, err := fmt.Sscan(v, &values[i]); err != nil {
				return ChannelRPMAcquireResult{}, err
			}
		default:
			return ChannelRPMAcquireResult{}, fmt.Errorf("invalid channel RPM limiter value %T", value)
		}
	}
	return ChannelRPMAcquireResult{Allowed: values[0] == 1, CurrentRPM: values[1], EffectiveLimit: values[2]}, nil
}

func TryAcquireChannelRPM(ctx context.Context, channelID int, settings *dto.ChannelRPMProtectionSettings) ChannelRPMAcquireResult {
	cfg, enabled := normalizeChannelRPMConfig(settings)
	if !enabled || channelID <= 0 {
		return ChannelRPMAcquireResult{Allowed: true, EffectiveLimit: channelRPMDefaultLimit}
	}
	if common.RedisEnabled && common.RDB != nil {
		result, err := acquireRedisChannelRPM(ctx, channelID, cfg)
		if err == nil {
			return result
		}
		common.SysError(fmt.Sprintf("channel RPM Redis limiter failed open for channel %d: %v", channelID, err))
		return ChannelRPMAcquireResult{Allowed: true, EffectiveLimit: cfg.limit}
	}
	return acquireMemoryChannelRPM(channelID, cfg, time.Now().UnixMilli())
}

func recordMemoryChannelRPM429(channelID int, cfg channelRPMConfig, nowMillis int64) (int, int) {
	channelRPMMemory.Lock()
	defer channelRPMMemory.Unlock()
	state := channelRPMMemory.channels[channelID]
	if state == nil {
		state = &memoryChannelRPMState{}
		channelRPMMemory.channels[channelID] = state
	}
	state.requests = pruneChannelRPMRequests(state.requests, nowMillis)
	current := len(state.requests)
	reduced := current * cfg.thresholdPercent / 100
	reduced = min(max(reduced, 1), cfg.limit)
	effective := channelRPMEffectiveLimit(cfg.limit, state.protectionCap, state.protectionTime, nowMillis, cfg.ramp)
	state.protectionCap = min(reduced, effective)
	state.protectionTime = nowMillis
	return current, state.protectionCap
}

func RecordChannelRPM429(ctx context.Context, channelID int, settings *dto.ChannelRPMProtectionSettings) {
	cfg, enabled := normalizeChannelRPMConfig(settings)
	if !enabled || channelID <= 0 {
		return
	}
	var current, startCap int
	if common.RedisEnabled && common.RDB != nil {
		requestsKey, stateKey := channelRPMRedisKeys(channelID)
		stateTTL := int(math.Ceil(cfg.ramp.Seconds())) + int(channelRPMRequestTTL/time.Second)
		result, err := common.RDB.Eval(ctx, channelRPMRecord429RedisScript, []string{requestsKey, stateKey},
			cfg.limit, cfg.thresholdPercent, cfg.ramp.Milliseconds(), stateTTL).Slice()
		if err != nil {
			common.SysError(fmt.Sprintf("failed to record channel %d upstream 429: %v", channelID, err))
			return
		}
		if len(result) >= 2 {
			if value, ok := result[0].(int64); ok {
				current = int(value)
			}
			if value, ok := result[1].(int64); ok {
				startCap = int(value)
			}
		}
	} else {
		current, startCap = recordMemoryChannelRPM429(channelID, cfg, time.Now().UnixMilli())
	}
	common.SysLog(fmt.Sprintf("channel #%d RPM protection activated after upstream 429: current=%d start_limit=%d configured_limit=%d ramp=%s", channelID, current, startCap, cfg.limit, cfg.ramp))
}

func ResetChannelRPMState(channelID int) {
	ResetChannelRPMStates([]int{channelID})
}

func ResetChannelRPMStates(channelIDs []int) {
	if len(channelIDs) == 0 {
		return
	}
	channelRPMMemory.Lock()
	validChannelIDs := make([]int, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID <= 0 {
			continue
		}
		delete(channelRPMMemory.channels, channelID)
		validChannelIDs = append(validChannelIDs, channelID)
	}
	channelRPMMemory.Unlock()
	if len(validChannelIDs) > 0 && common.RedisEnabled && common.RDB != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		pipe := common.RDB.Pipeline()
		for _, channelID := range validChannelIDs {
			requestsKey, stateKey := channelRPMRedisKeys(channelID)
			pipe.Del(ctx, requestsKey, stateKey)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			common.SysError(fmt.Sprintf("failed to reset RPM state for %d channels: %v", len(validChannelIDs), err))
		}
	}
}

func NewChannelRPMLimitError(message string) *types.NewAPIError {
	if message == "" {
		message = ChannelRPMLimitExceededMessage
	}
	return types.NewErrorWithStatusCode(
		channelRPMLimitError{message: message},
		types.ErrorCodeChannelRPMLimitExceeded,
		http.StatusTooManyRequests,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
}

func IsChannelRPMLimitError(err *types.NewAPIError) bool {
	return err != nil && (errors.Is(err, ErrChannelRPMLimitExceeded) || err.GetErrorCode() == types.ErrorCodeChannelRPMLimitExceeded)
}
