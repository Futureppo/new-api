package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAzureChannelProbeTokensAndRateLimits(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	oldLogEnabled, oldTimeout := common.LogConsumeEnabled, constant.StreamingTimeout
	common.LogConsumeEnabled, constant.StreamingTimeout = false, 10
	t.Cleanup(func() {
		common.LogConsumeEnabled, constant.StreamingTimeout = oldLogEnabled, oldTimeout
	})
	service.InitHttpClient()
	settings, err := common.Marshal(dto.UserSetting{AcceptUnsetRatioModel: true})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "azure-probe", Group: "default", Status: common.UserStatusEnabled,
		Setting: string(settings),
	}).Error)

	for _, tc := range []struct {
		name, requested, upstream, endpoint string
		stream, missingLimits, invalidBody  bool
		status                              int
		override                            string
	}{
		{name: "luna auto", requested: "gpt-6-luna", upstream: "gpt-6-luna"},
		{name: "luna chat", requested: "gpt-6-luna", upstream: "gpt-6-luna", endpoint: "openai"},
		{name: "mapped alias", requested: "my-model", upstream: "gpt-6-luna"},
		{name: "terra", requested: "gpt-6.1-terra", upstream: "gpt-6.1-terra"},
		{name: "sol 6.2", requested: "gpt-6.2-sol", upstream: "gpt-6.2-sol"},
		{name: "mapped terra", requested: "gpt-6.1-terra", upstream: "dep.terra-high"},
		{name: "stream", requested: "gpt-6-luna", upstream: "gpt-6-luna", stream: true},
		{name: "legacy", requested: "gpt-4o", upstream: "gpt-4o"},
		{name: "explicit zero override", requested: "my-model", upstream: "gpt-6-luna", override: `{"max_tokens":40,"max_completion_tokens":0}`},
		{name: "429 retains limits", requested: "gpt-6-luna", upstream: "gpt-6-luna", status: 429},
		{name: "400 retains limits", requested: "gpt-6-luna", upstream: "gpt-6-luna", status: 400},
		{name: "invalid body retains limits", requested: "gpt-6-luna", upstream: "gpt-6-luna", invalidBody: true},
		{name: "remaining without limits", requested: "gpt-6-luna", upstream: "gpt-6-luna", missingLimits: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := make(chan map[string]any, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/openai/deployments/"+tc.upstream+"/chat/completions", r.URL.Path)
				require.Equal(t, "2024-10-21", r.URL.Query().Get("api-version"))
				require.Equal(t, "test-key", r.Header.Get("api-key"))
				var request map[string]any
				if err := common.DecodeJson(r.Body, &request); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- request
				if !tc.missingLimits {
					w.Header().Set("x-ratelimit-limit-requests", "60")
					w.Header().Set("x-ratelimit-limit-tokens", "150000")
				}
				w.Header().Set("x-ratelimit-remaining-requests", "0")
				w.Header().Set("x-ratelimit-remaining-tokens", "149984")
				w.Header().Set("x-ratelimit-reset-requests", "10")
				w.Header().Set("x-ratelimit-reset-tokens", "300")
				w.Header().Set("Content-Type", "application/json")
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, `{"error":{"message":"upstream rejected probe","type":"rate_limit_error"}}`)
				} else if tc.invalidBody {
					_, _ = io.WriteString(w, "invalid upstream JSON")
				} else if tc.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: "+`{"id":"test","object":"chat.completion.chunk","model":"gpt-6-luna","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`+"\n\ndata: [DONE]\n\n")
				} else {
					_, _ = io.WriteString(w, `{"id":"test","object":"chat.completion","model":"gpt-6-luna","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
				}
			}))
			defer upstream.Close()
			mapping, err := common.Marshal(map[string]string{tc.requested: tc.upstream})
			require.NoError(t, err)
			channel := &model.Channel{
				Type: constant.ChannelTypeAzure, Key: "test-key", Status: common.ChannelStatusEnabled,
				Name: "azure-probe", Models: tc.requested, Group: "default", BaseURL: &upstream.URL,
				Other: "2024-10-21", ModelMapping: common.GetPointer(string(mapping)),
			}
			if tc.override != "" {
				channel.ParamOverride = &tc.override
			}
			require.NoError(t, db.Create(channel).Error)
			router := gin.New()
			router.GET("/api/channel/test/:id", TestChannel)
			w := httptest.NewRecorder()
			path := "/api/channel/test/" + strconv.Itoa(channel.Id) + "?model=" + tc.requested + "&endpoint_type=" + tc.endpoint + "&stream=" + strconv.FormatBool(tc.stream)
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, w.Code)
			var result struct {
				Success   bool                  `json:"success"`
				RateLimit *channelRateLimitInfo `json:"rate_limit"`
			}
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, tc.status == 0 && !tc.invalidBody, result.Success, w.Body.String())
			require.NotNil(t, result.RateLimit, w.Body.String())
			require.Equal(t, "azure", result.RateLimit.Provider)
			require.Empty(t, result.RateLimit.Tier)
			if tc.missingLimits {
				require.Empty(t, result.RateLimit.LimitRequests)
				require.Empty(t, result.RateLimit.LimitTokens)
			} else {
				require.Equal(t, "60", result.RateLimit.LimitRequests)
				require.Equal(t, "150000", result.RateLimit.LimitTokens)
			}
			require.Equal(t, "0", result.RateLimit.RemainingRequests)
			require.Equal(t, "149984", result.RateLimit.RemainingTokens)
			require.Equal(t, "10", result.RateLimit.ResetRequests)
			require.Equal(t, "300", result.RateLimit.ResetTokens)
			require.Len(t, requests, 1)
			request := <-requests
			if tc.upstream == "gpt-4o" {
				require.EqualValues(t, 16, request["max_tokens"])
				require.NotContains(t, request, "max_completion_tokens")
			} else {
				want := 16
				if tc.override != "" {
					want = 0
				}
				require.EqualValues(t, want, request["max_completion_tokens"])
				require.NotContains(t, request, "max_tokens")
			}
			if result.Success {
				// Wait for the handler's asynchronous timing update before closing the DB.
				require.Eventually(t, func() bool {
					var saved model.Channel
					return db.First(&saved, channel.Id).Error == nil && saved.TestTime != 0
				}, time.Second, 10*time.Millisecond)
			}
		})
	}
}

func TestAzureChannelRateLimitMissingHeaders(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeAzure}
	require.Nil(t, extractChannelRateLimitInfo(channel, "gpt-6-luna", http.Header{}))
	require.Nil(t, extractChannelRateLimitInfo(nil, "gpt-6-luna", http.Header{}))
}

func TestAzureClaudeProbeAndExplicitEndpointAuthentication(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	oldLogEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = oldLogEnabled })
	service.InitHttpClient()
	settings, err := common.Marshal(dto.UserSetting{AcceptUnsetRatioModel: true})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "azure-claude-probe", Group: "default", Status: common.UserStatusEnabled, Setting: string(settings)}).Error)
	for _, endpoint := range []string{"", "anthropic"} {
		t.Run(endpoint, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/anthropic/v1/messages", r.URL.Path)
				require.Equal(t, "test-key", r.Header.Get("x-api-key"))
				require.Empty(t, r.Header.Get("Authorization"))
				var request map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &request))
				require.Equal(t, "opaque", request["model"])
				require.Contains(t, request, "max_tokens")
				w.Header().Set("anthropic-ratelimit-requests-limit", "60")
				w.Header().Set("anthropic-ratelimit-tokens-limit", "50000")
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"opaque","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
			}))
			defer server.Close()
			channel := &model.Channel{Type: constant.ChannelTypeAzure, Key: "test-key", Status: common.ChannelStatusEnabled, Name: "azure-claude", Models: "opaque", Group: "default", BaseURL: &server.URL}
			channel.SetOtherSettings(dto.ChannelOtherSettings{AzureDeploymentModels: map[string]string{"opaque": "claude-opus-5"}})
			require.NoError(t, db.Create(channel).Error)
			router := gin.New()
			router.GET("/api/channel/test/:id", TestChannel)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/channel/test/"+strconv.Itoa(channel.Id)+"?model=opaque&endpoint_type="+endpoint, nil))
			var result struct {
				Success   bool                  `json:"success"`
				RateLimit *channelRateLimitInfo `json:"rate_limit"`
			}
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
			require.True(t, result.Success, w.Body.String())
			require.NotNil(t, result.RateLimit)
			require.Equal(t, "azure", result.RateLimit.Provider)
			require.Equal(t, "60", result.RateLimit.LimitRequests)
			require.Equal(t, "50000", result.RateLimit.LimitTokens)
			require.Eventually(t, func() bool {
				var saved model.Channel
				return db.First(&saved, channel.Id).Error == nil && saved.TestTime != 0
			}, time.Second, 10*time.Millisecond)
		})
	}
}
