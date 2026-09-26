package gmicloud

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	channelgmicloud "github.com/QuantumNous/new-api/relay/channel/gmicloud"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHYImageRequests(t *testing.T) {
	for _, tc := range []struct{ name, path, body, errorText string }{
		{"sync", "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"a cat","size":"1920x1080","n":1,"stream":false}`, ""},
		{"async", "/v1/images/tasks", `{"model":"hy-image-v3.5-preview","payload":{"prompt":"a cat","seed":0,"watermark":false}}`, ""},
		{"auto size", "/v1/images/tasks", `{"model":"hy-image-v3.5-preview","payload":{"prompt":"a cat","size":""}}`, ""},
		{"missing prompt", "/v1/images/tasks", `{"model":"hy-image-v3.5-preview","payload":{}}`, "prompt"},
		{"bad size", "/v1/images/tasks", `{"model":"hy-image-v3.5-preview","payload":{"prompt":"cat","size":0}}`, "size"},
		{"null payload", "/v1/images/tasks", `{"model":"hy-image-v3.5-preview","payload":null}`, "payload"},
		{"zero n", "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"cat","n":0}`, "n=1"},
		{"multiple n", "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"cat","n":2}`, "n=1"},
		{"stream", "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"cat","stream":true}`, "streaming"},
		{"format", "/v1/images/generations", `{"model":"hy-image-v3.5-preview","prompt":"cat","response_format":"bad"}`, "response_format"},
		{"audio endpoint", "/v1/audio/generations", `{"model":"hy-image-v3.5-preview","payload":{"text":"cat"}}`, "images"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: channelgmicloud.HYImageModel}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			a := &TaskAdaptor{}
			err := a.ValidateRequestAndSetAction(c, info)
			if tc.errorText != "" {
				require.NotNil(t, err)
				require.Equal(t, http.StatusBadRequest, err.StatusCode)
				require.Contains(t, err.Message, tc.errorText)
				return
			}
			require.Nil(t, err)
			require.Equal(t, constant.TaskActionImageGeneration, info.Action)
			require.Nil(t, a.ValidateMappedRequest(c, info))
			body, buildErr := a.BuildRequestBody(c, info)
			require.NoError(t, buildErr)
			var request struct {
				Model   string
				Payload map[string]any
			}
			require.NoError(t, common.DecodeJson(body, &request))
			require.Equal(t, channelgmicloud.HYImageModel, request.Model)
			require.Equal(t, "a cat", request.Payload["prompt"])
			if tc.name == "async" {
				require.Equal(t, float64(0), request.Payload["seed"])
				require.Equal(t, false, request.Payload["watermark"])
			} else if tc.name == "sync" {
				require.Equal(t, "1920x1080", request.Payload["size"])
				require.NotContains(t, request.Payload, "n")
				require.NotContains(t, request.Payload, "stream")
			}
			info.UpstreamModelName = "not-an-image-model"
			require.NotNil(t, a.ValidateMappedRequest(c, info))
		})
	}
}

func TestHYImageResultsAndDeferredResponse(t *testing.T) {
	body := `{"request_id":"upstream-image","model":"hy-image-v3.5-preview","status":"success","outcome":{"media_urls":[{"url":"https://example.com/a.png","type":"image","width":1920,"height":1080},{"url":"https://example.com/a.png","type":"image"},{"url":"https://example.com/b.png","type":"image"},{"url":"https://example.com/audio.mp3","type":"audio"}]}}`
	images, err := ParseImageResults([]byte(body))
	require.NoError(t, err)
	require.Len(t, images, 2)
	require.Equal(t, 1920, images[0].Width)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: constant.TaskActionImageGeneration, PublicTaskID: "task_public"}}
	a := &TaskAdaptor{}
	id, data, taskErr := a.DoResponse(c, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, taskErr)
	require.Equal(t, "upstream-image", id)
	require.Empty(t, w.Body.String(), "response must wait until the task is persisted")
	result, err := a.ParseTaskResult(data)
	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), result.Status)
	require.Equal(t, images[0].URL, result.Url)
	for _, outcome := range []string{`{}`, `{"media_urls":[{"type":"audio","url":"https://example.com/a.mp3"}]}`} {
		result, err = a.ParseTaskResult([]byte(`{"model":"hy-image-v3.5-preview","status":"success","outcome":` + outcome + `}`))
		require.NoError(t, err)
		require.Equal(t, string(model.TaskStatusFailure), result.Status)
	}
	images, err = ParseImageResults([]byte(`{"outcome":{"thumbnail_image_url":"https://example.com/thumbnail.png"}}`))
	require.NoError(t, err)
	require.Len(t, images, 1)
}
