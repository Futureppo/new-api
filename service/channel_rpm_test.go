package service

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func resetMemoryChannelRPMForTest(channelID int) {
	channelRPMMemory.Lock()
	delete(channelRPMMemory.channels, channelID)
	channelRPMMemory.Unlock()
}

func TestNormalizeChannelRPMConfigDisabled(t *testing.T) {
	_, enabled := normalizeChannelRPMConfig(nil)
	require.False(t, enabled)

	_, enabled = normalizeChannelRPMConfig(&dto.ChannelRPMProtectionSettings{Enabled: false, RPMLimit: 1000})
	require.False(t, enabled)

	_, enabled = normalizeChannelRPMConfig(&dto.ChannelRPMProtectionSettings{Enabled: true, RPMLimit: 0})
	require.False(t, enabled)
}

func TestMemoryChannelRPMSlidingWindow(t *testing.T) {
	const channelID = 910001
	resetMemoryChannelRPMForTest(channelID)
	t.Cleanup(func() { resetMemoryChannelRPMForTest(channelID) })
	cfg := channelRPMConfig{limit: 2, thresholdPercent: 60, ramp: 5 * time.Minute}
	const now = int64(1_000_000)

	require.True(t, acquireMemoryChannelRPM(channelID, cfg, now).Allowed)
	require.True(t, acquireMemoryChannelRPM(channelID, cfg, now).Allowed)
	blocked := acquireMemoryChannelRPM(channelID, cfg, now)
	require.False(t, blocked.Allowed)
	require.Equal(t, 2, blocked.CurrentRPM)

	afterWindow := acquireMemoryChannelRPM(channelID, cfg, now+channelRPMWindow.Milliseconds())
	require.True(t, afterWindow.Allowed)
	require.Equal(t, 1, afterWindow.CurrentRPM)
}

func TestMemoryChannelRPMConcurrentAcquireIsAtomic(t *testing.T) {
	const channelID = 910002
	resetMemoryChannelRPMForTest(channelID)
	t.Cleanup(func() { resetMemoryChannelRPMForTest(channelID) })
	cfg := channelRPMConfig{limit: 25, thresholdPercent: 60, ramp: 5 * time.Minute}

	var wg sync.WaitGroup
	allowed := make(chan bool, 100)
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed <- acquireMemoryChannelRPM(channelID, cfg, 1_000_000).Allowed
		}()
	}
	wg.Wait()
	close(allowed)

	allowedCount := 0
	for ok := range allowed {
		if ok {
			allowedCount++
		}
	}
	require.Equal(t, cfg.limit, allowedCount)
}

func TestMemoryChannelRPMProtectionAndRamp(t *testing.T) {
	const channelID = 910003
	resetMemoryChannelRPMForTest(channelID)
	t.Cleanup(func() { resetMemoryChannelRPMForTest(channelID) })
	cfg := channelRPMConfig{limit: 100, thresholdPercent: 60, ramp: 10 * time.Minute}
	const now = int64(1_000_000)

	for range 10 {
		require.True(t, acquireMemoryChannelRPM(channelID, cfg, now).Allowed)
	}
	current, cap := recordMemoryChannelRPM429(channelID, cfg, now)
	require.Equal(t, 10, current)
	require.Equal(t, 6, cap)
	require.False(t, acquireMemoryChannelRPM(channelID, cfg, now).Allowed)

	_, repeatedCap := recordMemoryChannelRPM429(channelID, cfg, now+1)
	require.Equal(t, 6, repeatedCap, "a repeated 429 must not relax the effective limit")

	halfRamp := acquireMemoryChannelRPM(channelID, cfg, now+1+5*time.Minute.Milliseconds())
	require.True(t, halfRamp.Allowed)
	require.Equal(t, 53, halfRamp.EffectiveLimit)

	fullyRecovered := acquireMemoryChannelRPM(channelID, cfg, now+1+cfg.ramp.Milliseconds())
	require.True(t, fullyRecovered.Allowed)
	require.Equal(t, cfg.limit, fullyRecovered.EffectiveLimit)
	channelRPMMemory.Lock()
	state := channelRPMMemory.channels[channelID]
	require.Zero(t, state.protectionCap)
	require.Zero(t, state.protectionTime)
	channelRPMMemory.Unlock()
}

func TestChannelRPMSkipTrackingAndLocalError(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	MarkChannelDailySuccessLimitSkipped(c, 1)
	MarkChannelRPMLimitSkipped(c, 2)
	require.Equal(t, map[int]bool{1: true, 2: true}, GetChannelSelectionExcludedIDs(c))

	err := NewChannelRPMLimitError("")
	require.True(t, IsChannelRPMLimitError(err))
	require.Equal(t, types.ErrorCodeChannelRPMLimitExceeded, err.GetErrorCode())
	require.Equal(t, 429, err.StatusCode)
	require.False(t, types.IsRecordErrorLog(err))
}

func startChannelRPMRedisForTest(t *testing.T) *redis.Client {
	t.Helper()
	redisServer, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())

	var output bytes.Buffer
	cmd := exec.Command(redisServer,
		"--bind", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--save", "",
		"--appendonly", "no",
		"--dir", t.TempDir(),
	)
	cmd.Stdout = &output
	cmd.Stderr = &output
	require.NoError(t, cmd.Start())

	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", port)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if err := client.Ping(ctx).Err(); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("redis-server did not start: %s", output.String())
		case <-time.After(20 * time.Millisecond):
		}
	}

	t.Cleanup(func() {
		_ = client.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return client
}

func TestRedisChannelRPMAcquireAndProtection(t *testing.T) {
	client := startChannelRPMRedisForTest(t)
	oldClient, oldEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = oldClient, oldEnabled
	})

	const channelID = 910004
	settings := &dto.ChannelRPMProtectionSettings{
		Enabled:                    true,
		RPMLimit:                   3,
		ProtectionThresholdPercent: 50,
		RampMinutes:                5,
	}
	for range 3 {
		require.True(t, TryAcquireChannelRPM(context.Background(), channelID, settings).Allowed)
	}
	require.False(t, TryAcquireChannelRPM(context.Background(), channelID, settings).Allowed)

	RecordChannelRPM429(context.Background(), channelID, settings)
	protected := TryAcquireChannelRPM(context.Background(), channelID, settings)
	require.False(t, protected.Allowed)
	require.Equal(t, 1, protected.EffectiveLimit)

	requestsKey, stateKey := channelRPMRedisKeys(channelID)
	require.Positive(t, client.Exists(context.Background(), requestsKey, stateKey).Val())
	ResetChannelRPMState(channelID)
	require.Zero(t, client.Exists(context.Background(), requestsKey, stateKey).Val())
}

func TestRedisChannelRPMFailureIsFailOpen(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	require.NoError(t, client.Close())
	oldClient, oldEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = oldClient, oldEnabled
	})

	settings := &dto.ChannelRPMProtectionSettings{
		Enabled:                    true,
		RPMLimit:                   1,
		ProtectionThresholdPercent: 60,
		RampMinutes:                5,
	}
	result := TryAcquireChannelRPM(context.Background(), 910005, settings)
	require.True(t, result.Allowed)
	require.Equal(t, settings.RPMLimit, result.EffectiveLimit)

	// Recording degradation state must also fail open when Redis is unavailable.
	RecordChannelRPM429(context.Background(), 910005, settings)
}
