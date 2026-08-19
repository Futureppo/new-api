package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSanitizeTaskModelText(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{
			OriginModelName:   "request-alias",
			UpstreamModelName: "upstream-model",
		},
		PrivateData: model.TaskPrivateData{ModelMappingFullEnabled: true},
	}

	require.Equal(t, "request-alias unavailable", sanitizeTaskModelText(task, "upstream-model unavailable"))

	task.PrivateData.ModelMappingFullEnabled = false
	require.Equal(t, "upstream-model unavailable", sanitizeTaskModelText(task, "upstream-model unavailable"))
}

func TestGenerateTextOtherInfoHidesFullRedirectMappingDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		StartTime:              time.Now(),
		FirstResponseTime:      time.Now(),
		ClientModelName:        "request-alias",
		ModelMappingTargetName: "upstream-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{ModelMappingFullEnabled: true},
			UpstreamModelName: "upstream-model",
			IsModelMapped:     true,
		},
	}

	other := GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 0, 0, 0)
	require.NotContains(t, other, "is_model_mapped")
	require.NotContains(t, other, "upstream_model_name")

	info.ChannelSetting.ModelMappingFullEnabled = false
	other = GenerateTextOtherInfo(c, info, 1, 1, 1, 0, 0, 0, 0)
	require.Equal(t, true, other["is_model_mapped"])
	require.Equal(t, "upstream-model", other["upstream_model_name"])
}
