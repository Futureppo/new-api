package qianwen

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestQianwenMediaBaseURLs(t *testing.T) {
	for _, suffix := range []string{"/compatible-mode/v1/images/generations", "/compatible-mode/v1/embeddings", "/compatible-api/v1/reranks", "/compatible-api/v1", multimodalPath, speechPath, musicPath, videoPath, rerankPath, "/api/v1"} {
		require.Equal(t, "https://proxy.example/prefix", NormalizeBaseURL("https://proxy.example/prefix"+suffix+"/"), suffix)
	}
}

func TestQianwenMultipartImageEdit(t *testing.T) {
	for _, field := range []string{"image", "image[]"} {
		t.Run(field, func(t *testing.T) {
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			file, err := form.CreateFormFile(field, "cat.png")
			require.NoError(t, err)
			_, err = file.Write([]byte("\x89PNG\r\n\x1a\nimage"))
			require.NoError(t, err)
			require.NoError(t, form.Close())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
			c.Request.Header.Set("Content-Type", form.FormDataContentType())
			info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeImagesEdits}
			info.PriceData.UsePrice = true
			converted, err := (&Adaptor{}).ConvertImageRequest(c, info, dto.ImageRequest{Model: "qwen-image-3.0", Prompt: "cat", Seed: common.GetPointer(int64(0)), PromptExtend: common.GetPointer(false)})
			require.NoError(t, err)
			data, err := common.Marshal(converted)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(gjson.GetBytes(data, "image.0").String(), "data:image/png;base64,"))
			require.Equal(t, "0", gjson.GetBytes(data, "seed").Raw)
			require.Equal(t, "false", gjson.GetBytes(data, "prompt_extend").Raw)
		})
	}
}

func TestQianwenMultimodalRerankRoundTrip(t *testing.T) {
	body := `{"model":"qwen3-vl-rerank","query":{"text":"cat","image":"https://example.com/cat.png"},"documents":[{"image":"https://example.com/a.png"}],"top_n":0,"return_documents":false,"fps":0}`
	var req dto.RerankRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	copy, err := common.DeepCopy(&req)
	require.NoError(t, err)
	roundtrip, err := common.Marshal(copy)
	require.NoError(t, err)
	require.JSONEq(t, body, string(roundtrip))
	converted, err := (&Adaptor{}).ConvertRerankRequest(nil, 0, *copy)
	require.NoError(t, err)
	data, err := common.Marshal(converted)
	require.NoError(t, err)
	require.Equal(t, "cat", gjson.GetBytes(data, "input.query.text").String())
	require.Equal(t, "0", gjson.GetBytes(data, "parameters.fps").Raw)
	req.Model = "qwen3-rerank"
	_, err = (&Adaptor{}).ConvertRerankRequest(nil, 0, req)
	require.ErrorContains(t, err, "query objects")
}

func TestQianwenSparseEmbeddingRejected(t *testing.T) {
	for _, req := range []dto.EmbeddingRequest{{OutputType: "sparse"}, {OutputType: "dense&sparse"}, {EncodingFormat: "base64"}} {
		_, err := (&Adaptor{}).ConvertEmbeddingRequest(nil, nil, req)
		require.Error(t, err)
	}
}

func TestQianwenVideoFinalBody(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"alias","prompt":"cat","seconds":"5","size":"1280x720"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "wan3.0-video", ParamOverride: map[string]any{"operations": []any{
		map[string]any{"mode": "set", "path": "parameters.duration", "value": 8},
		map[string]any{"mode": "set", "path": "parameters.audio", "value": false},
	}}}}
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	body, err := videoBody(c, info)
	require.NoError(t, err)
	data, err := common.Marshal(body)
	require.NoError(t, err)
	require.Equal(t, "720P", gjson.GetBytes(data, "parameters.resolution").String())
	require.Equal(t, "16:9", gjson.GetBytes(data, "parameters.ratio").String())
	require.Equal(t, "8", gjson.GetBytes(data, "parameters.duration").Raw)
	require.Equal(t, "false", gjson.GetBytes(data, "parameters.audio").Raw)
	require.Equal(t, 8.0, a.EstimateBilling(c, info)["seconds"])
	info.ParamOverride = map[string]any{"model": "different-model"}
	_, err = videoBody(c, info)
	require.ErrorContains(t, err, "model mapping")
	for _, size := range []string{"1024x1024", "0x720", "1280xabc"} {
		_, _, err = videoDimensions(size)
		require.Error(t, err)
	}
}

func TestQianwenMediaErrorEnvelopes(t *testing.T) {
	for _, body := range []string{`{"code":"InvalidParameter","message":"bad media"}`, `{"error":{"type":"upstream_error","message":"bad media"}}`, `not JSON`} {
		resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
		_, err := readJSONResponse(resp, &audioResult{})
		require.NotNil(t, err)
	}
	a := &TaskAdaptor{}
	for _, body := range []string{`{"output":{"task_status":"SUCCEEDED","video_url":"https://example.com/a.mp4"}}`, `{"output":{"task_status":"unexpected"}}`, `{"code":"InvalidApiKey","message":"bad key"}`} {
		_, err := a.ParseTaskResult([]byte(body))
		require.Error(t, err)
	}
}

func TestQianwenHeaderFinalizerPreservesOverrides(t *testing.T) {
	h := http.Header{"Authorization": []string{"Bearer client"}, "Anthropic-Version": []string{"custom"}, "Accept": []string{"custom"}}
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "channel"}}
	require.NoError(t, (&Adaptor{}).FinalizeRequestHeader(nil, &h, info))
	require.Empty(t, h.Get("Authorization"))
	require.Equal(t, "channel", h.Get("x-api-key"))
	require.Equal(t, "custom", h.Get("anthropic-version"))
	require.Equal(t, "custom", h.Get("Accept"))
}
