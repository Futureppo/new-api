package gmicloud

import (
	"bytes"
	"io"
	"mime/multipart"
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

func TestHYImageReferenceRequests(t *testing.T) {
	for _, tc := range []struct {
		name, path, fields, errorText string
		editing                       bool
	}{
		{"4k", "generations", `"size":"4096x4096","generate_max_pixels":4194304`, "", false},
		{"auto", "generations", `"size":"auto","seed":0`, "", false},
		{"image string", "generations", `"image":"https://example.com/a.png"`, "", true},
		{"image array", "edits", `"image":["https://example.com/a.png","https://example.com/b.png"]`, "", true},
		{"OpenAI JSON", "edits", `"images":[{"image_url":"https://example.com/a.png"}]`, "", true},
		{"missing reference", "edits", `"size":"4096x4096"`, "reference image", false},
		{"empty reference", "edits", `"image":[]`, "reference image", false},
		{"null reference", "edits", `"image":null`, "image must", false},
		{"null images", "edits", `"images":null`, "images must", false},
		{"conflicting fields", "edits", `"image":"https://example.com/a.png","images":[]`, "only one", false},
		{"file ID", "edits", `"images":[{"file_id":"file_123"}]`, "file_id", false},
		{"mask", "edits", `"image":"https://example.com/a.png","mask":{"image_url":"https://example.com/mask.png"}`, "mask", false},
		{"data URL", "edits", `"image":"data:image/png;base64,abc"`, "public HTTP(S)", false},
		{"local file", "edits", `"image":"file:///tmp/a.png"`, "public HTTP(S)", false},
		{"authenticated URL", "edits", `"image":"https://user:pass@example.com/a.png"`, "public HTTP(S)", false},
		{"bad ref type", "edits", `"image":true`, "image must", false},
		{"empty URL", "edits", `"images":[{}]`, "public HTTP(S)", false},
		{"too many", "edits", `"image":["https://example.com/a","https://example.com/b","https://example.com/c","https://example.com/d","https://example.com/e","https://example.com/f"]`, "at most 5", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/"+tc.path, strings.NewReader(`{"model":"hy-image-v3.5-preview","prompt":"cat",`+tc.fields+`}`))
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			a := &TaskAdaptor{}
			err := a.ValidateRequestAndSetAction(c, info)
			if tc.errorText != "" {
				require.NotNil(t, err)
				require.Equal(t, http.StatusBadRequest, err.StatusCode)
				require.Contains(t, err.Message, tc.errorText)
				return
			}
			require.Nil(t, err)
			require.Equal(t, tc.editing, info.Action == constant.TaskActionImageEdit)
			body, buildErr := a.BuildRequestBody(c, info)
			require.NoError(t, buildErr)
			var req struct{ Payload map[string]any }
			require.NoError(t, common.DecodeJson(body, &req))
			if tc.editing {
				require.NotEmpty(t, req.Payload["image"])
				require.NotContains(t, req.Payload, "images")
			}
			if tc.name == "4k" {
				require.Equal(t, "4096x4096", req.Payload["size"])
				require.Equal(t, float64(4194304), req.Payload["generate_max_pixels"])
			}
			if tc.name == "auto" {
				require.Equal(t, "", req.Payload["size"])
				require.Equal(t, float64(0), req.Payload["seed"])
			}
		})
	}
}

func TestHYRejectsMultipartInsteadOfDroppingUploadedImage(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", channelgmicloud.HYImageModel))
	require.NoError(t, writer.WriteField("prompt", "edit this"))
	file, err := writer.CreateFormFile("image", "image.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("image bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	_, err = parseSyncImageRequest(c)
	require.ErrorContains(t, err, "multipart file uploads are not supported")
}

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
