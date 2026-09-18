package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExtractTestUpstreamModel(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
	}{
		{"chat", `{"model":"actual-model-20260918","choices":[]}`, "actual-model-20260918"},
		{"responses", `{"object":"response","model":"resolved-model"}`, "resolved-model"},
		{"anthropic", `{"type":"message","model":"claude-resolved"}`, "claude-resolved"},
		{"gemini", `{"modelVersion":"gemini-resolved"}`, "gemini-resolved"},
		{"task", `{"id":"task-1","model":"video-resolved"}`, "video-resolved"},
		{"wrapped task", `{"data":{"model":"video-resolved"}}`, "video-resolved"},
		{"result", `{"result":{"modelVersion":"resolved-version"}}`, "resolved-version"},
		{"array", `[null,{}, {"model":""}, {"modelVersion":"first-model"}, {"model":"second-model"}]`, "first-model"},
		{"empty body", "", ""},
		{"absent", `{"choices":[]}`, ""},
		{"empty", `{"model":" \t "}`, ""},
		{"null", `{"model":null}`, ""},
		{"number", `{"model":123}`, ""},
		{"boolean", `{"model":true}`, ""},
		{"object", `{"model":{"name":"not-a-model"}}`, ""},
		{"model array", `{"model":["not-a-model"]}`, ""},
		{"invalid json", `{"model":"partial"`, ""},
		{"whitespace", `{"model":" actual-model "}`, "actual-model"},
		{"fallback metadata", `{"model":null,"modelVersion":"actual-version"}`, "actual-version"},
		{"generated metadata", `{"choices":[{"message":{"model":"generated-model","content":"model: guessed"}}],"request":{"model":"request-echo"}}`, ""},
		{"chat stream", "data: {\"model\":\"first-model\"}\n\ndata: {\"model\":\"second-model\"}\n\ndata: [DONE]\n\n", "first-model"},
		{"responses stream", "event: response.created\ndata: {\"response\":{\"model\":\"response-model\"}}\n\n", "response-model"},
		{"anthropic stream", "event: message_start\ndata: {\"message\":{\"model\":\"claude-model\"}}\n\n", "claude-model"},
		{"gemini stream", "data: {\"modelVersion\":\"gemini-model\"}\n\n", "gemini-model"},
		{"stream array", "data: [{\"modelVersion\":\"gemini-model\"}]\n\n", "gemini-model"},
		{"empty events", ": ping\n\nevent: ping\ndata: {}\n\ndata: {\"model\":\"\"}\n\ndata: {\"model\":\"actual-model\"}\n\n", "actual-model"},
		{"done only", "data: [DONE]\n\n", ""},
		{"bad event", "data: malformed\n\ndata: {\"model\":\"actual-model\"}\n\n", "actual-model"},
		{"crlf", "data: {\"model\":\"actual-model\"}\r\n\r\n", "actual-model"},
		{"multiline event", "data: {\n: comment\ndata: \"model\":\"actual-model\"\ndata: }\n\n", "actual-model"},
		{"final event", "data:{\"model\":\"actual-model\"}", "actual-model"},
		{"large event", "data: {\"content\":\"" + strings.Repeat("x", 70<<10) + "\",\"model\":\"actual-model\"}\n\n", "actual-model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, extractTestUpstreamModel([]byte(tt.body)))
		})
	}
}

type channelTestResponseBody struct {
	io.Reader
	closed bool
}

func (body *channelTestResponseBody) Close() error {
	body.closed = true
	return nil
}

func TestCaptureTestUpstreamResponsePreservesBodyAndMapping(t *testing.T) {
	for _, body := range []string{
		`{"model":"resolved-model","choices":[]}`,
		"event: message_start\r\ndata: {\"message\":{\"model\":\"resolved-model\"}}\r\n\r\ndata: [DONE]\r\n\r\n",
	} {
		t.Run(body[:10], func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				ClientModelName:        "request-alias",
				ModelMappingTargetName: "configured-model",
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: "configured-model",
					IsModelMapped:     true,
					ChannelSetting:    dto.ChannelSettings{ModelMappingFullEnabled: true},
				},
			}
			original := &channelTestResponseBody{Reader: iotest.OneByteReader(strings.NewReader(body))}
			resp := &http.Response{Body: original}
			capture := captureTestUpstreamResponse(info, resp)
			require.Empty(t, capture.Snapshot().UpstreamResponseBody, "capture must not eagerly read the response")
			read, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(read))
			require.NoError(t, resp.Body.Close())
			require.True(t, original.closed)

			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			relaycommon.SetRelayInfo(c, info)
			mapped := relaycommon.RewriteClientResponseBytes(c, read)
			require.Equal(t, "request-alias", extractTestUpstreamModel(mapped))
			require.Equal(t, "resolved-model", extractTestUpstreamModel(capture.Snapshot().UpstreamResponseBody))
		})
	}
}

func TestCaptureTestUpstreamResponsePreservesReadErrors(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(iotest.ErrReader(io.ErrUnexpectedEOF))}
	capture := captureTestUpstreamResponse(&relaycommon.RelayInfo{}, resp)
	_, err := io.ReadAll(resp.Body)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Empty(t, extractTestUpstreamModel(capture.Snapshot().UpstreamResponseBody))
	require.Empty(t, extractTestUpstreamModel(captureTestUpstreamResponse(&relaycommon.RelayInfo{}, nil).Snapshot().UpstreamResponseBody))
}

func TestChannelReturnsUpstreamModel(t *testing.T) {
	db := setupChannelRangeTestDB(t)
	previousLogEnabled := common.LogConsumeEnabled
	previousStreamingTimeout := constant.StreamingTimeout
	common.LogConsumeEnabled = false
	constant.StreamingTimeout = 10
	t.Cleanup(func() {
		common.LogConsumeEnabled = previousLogEnabled
		constant.StreamingTimeout = previousStreamingTimeout
	})
	service.InitHttpClient()
	userSetting, err := common.Marshal(dto.UserSetting{AcceptUnsetRatioModel: true})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "channel-test", Group: "default", Status: common.UserStatusEnabled,
		Setting: string(userSetting),
	}).Error)

	for _, tt := range []struct {
		name, upstreamModel string
		stream              bool
		video               bool
	}{
		{"json", "resolved-model", false, false},
		{"json absent", "", false, false},
		{"stream", "resolved-stream-model", true, false},
		{"stream absent", "", true, false},
		{"video", "resolved-video-model", false, true},
		{"video absent", "", false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request dto.GeneralOpenAIRequest
				var err error
				if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
					err = r.ParseMultipartForm(1 << 20)
					request.Model = r.FormValue("model")
				} else {
					err = common.DecodeJson(r.Body, &request)
				}
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				requests <- request.Model
				payload := gin.H{
					"id": "chatcmpl-test", "object": "chat.completion", "created": 1,
					"choices": []gin.H{{"index": 0, "message": gin.H{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}},
					"usage":   gin.H{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
				}
				if tt.upstreamModel != "" {
					payload["model"] = tt.upstreamModel
				}
				if tt.video {
					payload["object"] = "video"
					payload["status"] = "queued"
				}
				if tt.stream {
					w.Header().Set("Content-Type", "text/event-stream")
					payload["object"] = "chat.completion.chunk"
					payload["choices"] = []gin.H{{"index": 0, "delta": gin.H{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}}
				} else {
					w.Header().Set("Content-Type", "application/json")
				}
				data, err := common.Marshal(payload)
				if err != nil {
					t.Error(err)
					return
				}
				if tt.stream {
					_, _ = w.Write([]byte("data: " + string(data) + "\n\ndata: [DONE]\n\n"))
				} else {
					_, _ = w.Write(data)
				}
			}))
			defer upstream.Close()

			requestedModel, endpoint := "gpt-4o-mini", "openai"
			if tt.video {
				requestedModel, endpoint = "sora-2", "openai-video"
			}
			mapping, err := common.Marshal(map[string]string{requestedModel: "configured-model"})
			require.NoError(t, err)
			channel := &model.Channel{
				Type: constant.ChannelTypeOpenAI, Key: "test-key", Status: common.ChannelStatusEnabled,
				Name: "response-model-test", Models: requestedModel, Group: "default", BaseURL: &upstream.URL,
				ModelMapping: common.GetPointer(string(mapping)),
			}
			channel.SetSetting(dto.ChannelSettings{ModelMappingFullEnabled: true})
			require.NoError(t, db.Create(channel).Error)
			router := gin.New()
			router.GET("/api/channel/test/:id", TestChannel)
			w := httptest.NewRecorder()
			path := "/api/channel/test/" + strconv.Itoa(channel.Id) + "?model=" + requestedModel + "&endpoint_type=" + endpoint
			if tt.stream {
				path += "&stream=true"
			}
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, w.Code)
			var result map[string]any
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, true, result["success"], w.Body.String())
			require.Contains(t, result, "upstream_model")
			require.Equal(t, tt.upstreamModel, result["upstream_model"])
			require.Len(t, requests, 1)
			require.Equal(t, "configured-model", <-requests)
			require.Empty(t, requests, "model extraction must not send a second request")
			// Wait for TestChannel's existing asynchronous timing update before
			// allowing this fixture's database to be closed or replaced.
			require.Eventually(t, func() bool {
				var saved model.Channel
				return db.First(&saved, channel.Id).Error == nil && saved.TestTime != 0
			}, time.Second, 10*time.Millisecond)
		})
	}
}
