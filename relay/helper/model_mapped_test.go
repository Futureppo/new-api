package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newModelMappingContext(mapping string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("model_mapping", mapping)
	return c
}

func newModelMappingInfo(enabled bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: "A",
		ClientModelName: "A",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{ModelMappingFullEnabled: enabled},
			UpstreamModelName: "A",
		},
	}
}

func TestModelMappedHelperChainedFullRedirect(t *testing.T) {
	c := newModelMappingContext(`{"A":"B","B":"C"}`)
	info := newModelMappingInfo(true)
	request := &dto.GeneralOpenAIRequest{Model: "A"}

	require.NoError(t, ModelMappedHelper(c, info, request))
	require.Equal(t, "C", request.Model)
	require.Equal(t, "C", info.UpstreamModelName)
	require.Equal(t, "C", info.ModelMappingTargetName)
	require.Equal(t, "A", info.GetDisplayModelName())
	require.True(t, info.IsModelMappingFullActive())
	require.False(t, info.ShouldExposeModelMapping())
}

func TestModelMappedHelperFullRedirectInactiveWithoutHitOrWhenDisabled(t *testing.T) {
	c := newModelMappingContext(`{"other":"C"}`)
	info := newModelMappingInfo(true)
	request := &dto.GeneralOpenAIRequest{Model: "A"}

	require.NoError(t, ModelMappedHelper(c, info, request))
	require.False(t, info.IsModelMapped)
	require.False(t, info.IsModelMappingFullActive())
	require.Equal(t, "A", request.Model)

	c.Set("model_mapping", `{"A":"C"}`)
	info.ChannelSetting.ModelMappingFullEnabled = false
	require.NoError(t, ModelMappedHelper(c, info, request))
	require.True(t, info.IsModelMapped)
	require.False(t, info.IsModelMappingFullActive())
	require.True(t, info.ShouldExposeModelMapping())
}

func TestModelMappedHelperRetryResetsMappingState(t *testing.T) {
	c := newModelMappingContext(`{"A":"B"}`)
	info := newModelMappingInfo(true)
	request := &dto.GeneralOpenAIRequest{Model: "A"}

	require.NoError(t, ModelMappedHelper(c, info, request))
	require.Equal(t, "B", info.ModelMappingTargetName)
	info.MarkModelMappingBypassed()
	require.False(t, info.IsModelMappingFullActive())

	c.Set("model_mapping", `{"A":"C"}`)
	info.OriginModelName = "B-derived"
	info.UpstreamModelName = "B"
	require.NoError(t, ModelMappedHelper(c, info, request))
	require.Equal(t, "A", info.OriginModelName)
	require.Equal(t, "C", info.ModelMappingTargetName)
	require.Equal(t, "C", request.Model)
	require.False(t, info.ModelMappingBypassed)
	require.True(t, info.IsModelMappingFullActive())
}
