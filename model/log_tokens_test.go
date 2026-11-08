package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestLogTokenCounts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		prompt int
		other  string
		input  int
	}{
		{"plain", 100, "", 100},
		{"openai cache included", 1000, `{"cache_tokens":900,"cache_creation_tokens":50}`, 1000},
		{"anthropic cache read", 100, `{"usage_semantic":"anthropic","cache_tokens":900}`, 1000},
		{"legacy claude", 100, `{"claude":true,"cache_tokens":900}`, 1000},
		{"cache only", 0, `{"claude":true,"cache_tokens":900}`, 900},
		{"aggregate includes TTL remainder", 100, `{"claude":true,"cache_creation_tokens":80,"cache_creation_tokens_5m":40,"cache_creation_tokens_1h":20}`, 180},
		{"TTL split exceeds aggregate", 100, `{"claude":true,"cache_creation_tokens":20,"cache_creation_tokens_5m":40,"cache_creation_tokens_1h":20}`, 160},
		{"normalized write aliases", 100, `{"claude":true,"cache_write_tokens":80,"cache_creation_tokens":80,"cache_creation_tokens_5m":40,"cache_creation_tokens_1h":20}`, 180},
		{"legacy conversion", 62, `{"cache_tokens":3544,"cache_creation_tokens_5m":586}`, 4192},
		{"tagged conversion includes cache", 4192, `{"usage_source":"anthropic","cache_tokens":3544,"cache_creation_tokens_5m":586}`, 4192},
		{"tagged conversion keeps upstream format", 1000, `{"usage_source":"anthropic","claude":true,"cache_tokens":900}`, 1000},
		{"explicit semantic overrides source", 100, `{"usage_semantic":"anthropic","usage_source":"anthropic","claude":true,"cache_tokens":900}`, 1000},
		{"explicit semantic overrides format", 1000, `{"usage_semantic":"openai","claude":true,"cache_tokens":900,"cache_creation_tokens_5m":50}`, 1000},
		{"explicit total overrides semantic", 100, `{"input_tokens_total":1000,"usage_semantic":"anthropic","cache_tokens":900}`, 1000},
		{"explicit zero total", 100, `{"input_tokens_total":0,"claude":true,"cache_tokens":900}`, 0},
		{"null total uses semantic", 100, `{"input_tokens_total":null,"claude":true,"cache_tokens":900}`, 1000},
		{"negative total uses semantic", 100, `{"input_tokens_total":-1,"claude":true,"cache_tokens":900}`, 1000},
		{"unknown semantic", 100, `{"usage_semantic":"unknown","claude":true,"cache_tokens":900}`, 100},
		{"missing semantic does not guess from cache", 100, `{"cache_tokens":900}`, 100},
		{"malformed metadata", 100, `{"claude":true,`, 100},
		{"invalid number", 100, `{"claude":true,"cache_tokens":"bad"}`, 100},
		{"null metadata", 100, `null`, 100},
		{"negative counts", -5, `{"claude":true,"cache_tokens":-1,"cache_creation_tokens":-2}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, output := logTokenCounts(tc.prompt, 50, tc.other)
			require.Equal(t, tc.input, input)
			require.Equal(t, 50, output)
		})
	}
	_, output := logTokenCounts(100, -1, "")
	require.Zero(t, output)
}

func TestSumUsedQuotaCacheTokensAndWindow(t *testing.T) {
	truncateTables(t)
	now := common.GetTimestamp()
	newLog := func(kind int, age int64, prompt, output int, other string) Log {
		return Log{Username: "alice", CreatedAt: now - age, Type: kind,
			PromptTokens: prompt, CompletionTokens: output, Other: other, Quota: 10}
	}
	logs := []Log{
		newLog(LogTypeConsume, 5, 100, 50, `{"claude":true,"cache_tokens":900}`),
		newLog(LogTypeConsume, 5, 1000, 50, `{"cache_tokens":900}`),
		newLog(LogTypeConsume, 5, 0, 0, `{"claude":true,"cache_tokens":100}`),
		newLog(LogTypeError, 5, 9999, 9999, `{"input_tokens_total":9999}`),
		newLog(LogTypeRefund, 5, 9999, 9999, ""),
		newLog(LogTypeConsume, 120, 9999, 9999, ""),
		newLog(LogTypeConsume, -120, 9999, 9999, ""),
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	// The date picker controls quota, while RPM/TPM retain their live 60s window.
	stat, err := SumUsedQuota(0, now-180, now-90, "", "alice", "", 0, "", "", "")
	require.NoError(t, err)
	require.Equal(t, 10, stat.Quota)
	require.Equal(t, 3, stat.Rpm)
	require.Equal(t, 4, stat.RpmTotal)
	require.Equal(t, float64(75), stat.RpmSuccessRate)
	require.Equal(t, 2100, stat.InputTpm)
	require.Equal(t, 100, stat.OutputTpm)
	require.Equal(t, 2200, stat.Tpm)

	empty, err := SumUsedQuota(0, 0, 0, "", "nobody", "", 0, "", "", "")
	require.NoError(t, err)
	require.Equal(t, Stat{}, empty)
}

func TestRecordConsumeLogDashboardIncludesCacheTokens(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&QuotaData{}))
	previousExport := common.DataExportEnabled
	CacheQuotaDataLock.Lock()
	previousCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	common.DataExportEnabled = true
	t.Cleanup(func() {
		common.DataExportEnabled = previousExport
		CacheQuotaDataLock.Lock()
		CacheQuotaData = previousCache
		CacheQuotaDataLock.Unlock()
		DB.Where("model_name = ?", "cache-stat-test").Delete(&QuotaData{})
	})
	for _, params := range []RecordConsumeLogParams{
		{PromptTokens: 100, CompletionTokens: 50, Other: map[string]interface{}{"claude": true, "cache_tokens": 900}},
		{PromptTokens: 1000, CompletionTokens: 50, Other: map[string]interface{}{"cache_tokens": 900, "input_tokens_total": 1000}},
	} {
		params.ModelName, params.Quota = "cache-stat-test", 7
		RecordConsumeLog(newLogTestContext("cache-stat-test"), 1, params)
	}
	require.Eventually(t, func() bool {
		CacheQuotaDataLock.Lock()
		defer CacheQuotaDataLock.Unlock()
		count, tokens := 0, 0
		for _, data := range CacheQuotaData {
			count += data.Count
			tokens += data.TokenUsed
		}
		return count == 2 && tokens == 2100
	}, 3*time.Second, 10*time.Millisecond)
	SaveQuotaDataCache()
	var total QuotaData
	require.NoError(t, DB.Model(&QuotaData{}).Select("SUM(count) AS count, SUM(token_used) AS token_used, SUM(quota) AS quota").Where("model_name = ?", "cache-stat-test").Scan(&total).Error)
	require.Equal(t, 2, total.Count)
	require.Equal(t, 2100, total.TokenUsed)
	require.Equal(t, 14, total.Quota)
	var stored []Log
	require.NoError(t, LOG_DB.Where("model_name = ?", "cache-stat-test").Order("id").Find(&stored).Error)
	require.Len(t, stored, 2)
	require.Equal(t, 100, stored[0].PromptTokens)
	require.Equal(t, 1000, stored[1].PromptTokens)
}
