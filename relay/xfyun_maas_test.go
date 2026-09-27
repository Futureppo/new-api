package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestXfyunMaasImagePipeline(t *testing.T) {
	service.InitHttpClient()
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2.1/tti", r.URL.Path)
		captured, _ = io.ReadAll(r.Body)
		// Avoid DB billing while exercising the full conversion/HTTP pipeline.
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"code":10005,"message":"fixture"}`))
	}))
	defer server.Close()
	old := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	model_setting.GetGlobalSettings().PassThroughRequestEnabled = true
	t.Cleanup(func() { model_setting.GetGlobalSettings().PassThroughRequestEnabled = old })
	body := `{"model":"alias","prompt":"cat","seed":0,"guidance_scale":0,"num_inference_steps":0,"negative_prompt":"blur"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", strings.NewReader(body))
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeXunfeiMaas)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "fixture-key")
	c.Set("model_mapping", `{"alias":"real-image-id"}`)
	var req dto.ImageRequest
	require.NoError(t, common.UnmarshalJsonStr(body, &req))
	info := &relaycommon.RelayInfo{Request: &req, OriginModelName: "alias", RequestURLPath: c.Request.URL.Path, RelayMode: relayconstant.RelayModeImagesGenerations, RelayFormat: types.RelayFormatOpenAIImage}
	apiErr := ImageHelper(c, info)
	require.NotNil(t, apiErr)
	require.Equal(t, 400, apiErr.StatusCode)
	require.Contains(t, string(captured), `"domain":"real-image-id"`)
	for _, fragment := range []string{`"seed":0`, `"guidance_scale":0`, `"num_inference_steps":0`, `"negative_prompts":{"text":"blur"}`} {
		require.Contains(t, string(captured), fragment)
	}
	require.NotContains(t, string(captured), `"model":"alias"`)
}

func TestXfyunMaasPassthroughPolicy(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeXunfeiMaas, ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true}}}
	require.False(t, shouldPassThroughTextRequest(info, true))
	require.False(t, shouldPassThroughImageRequest(info))
	require.False(t, shouldChatCompletionsUseResponses(info))
	require.NotNil(t, GetAdaptor(constant.APITypeXunfeiMaas))
}
