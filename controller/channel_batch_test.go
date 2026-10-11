package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gmicloud"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBatchGenerationChannelTestUsesExistingGMICloudProtocol(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, gmicloud.TaskRequestsPath, r.URL.Path)
		require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, gmicloud.BatchInferenceModel, gjson.GetBytes(body, "model").String())
		require.Equal(t, "gemini-3-flash-preview", gjson.GetBytes(body, "payload.model").String())
		require.True(t, gjson.Valid(gjson.GetBytes(body, "payload.input_data").String()))
		require.False(t, gjson.GetBytes(body, "stream").Bool())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"request_id":"batch-test","model":"Gemini-batch-inference","status":"queued"}`)
	}))
	defer upstream.Close()
	db, _, channel := setupDecisionsGateway(t, upstream.URL)
	// A manually selected batch endpoint is rejected on unsupported channels.
	result := testChannel(channel, gmicloud.BatchInferenceModel, "batch-generation", true)
	require.ErrorContains(t, result.localErr, "require a GMICloud channel")
	require.Zero(t, calls.Load())
	channel.Type = constant.ChannelTypeGMICloud
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"Gemini-batch-inference":0.002}`))
	result = testChannel(channel, gmicloud.BatchInferenceModel, "batch-generation", true)
	require.NoError(t, result.localErr)
	require.Nil(t, result.newAPIError)
	require.Equal(t, gmicloud.BatchInferenceModel, result.upstreamModel)
	require.EqualValues(t, 1, calls.Load())
	var log model.Log
	require.NoError(t, db.Where("token_name = ?", "模型测试").Last(&log).Error)
	require.False(t, log.IsStream)
	require.Equal(t, int(0.002*common.QuotaPerUnit), log.Quota)
	require.Equal(t, "/v1/batch/generations", gjson.Get(log.Other, "request_path").String())
}
