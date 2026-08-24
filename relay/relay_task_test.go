package relay

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestTaskModel2DtoIncludesModelNameAndRequestId(t *testing.T) {
	task := &model.Task{
		ID:        1,
		TaskID:    "task_public",
		RequestId: "req_task_public",
		Platform:  constant.TaskPlatform("48"),
		Properties: model.Properties{
			OriginModelName:   "grok-imagine-video",
			UpstreamModelName: "upstream-grok-video",
		},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{OriginModelName: "billing-model"},
		},
		Data: json.RawMessage(`{}`),
	}

	got := TaskModel2Dto(task)

	require.Equal(t, "task_public", got.TaskID)
	require.Equal(t, "req_task_public", got.RequestId)
	require.Equal(t, "grok-imagine-video", got.ModelName)
}

func TestTaskModel2DtoFallsBackModelNameToBillingContextThenUpstream(t *testing.T) {
	task := &model.Task{
		TaskID: "task_public",
		Properties: model.Properties{
			UpstreamModelName: "upstream-grok-video",
		},
		PrivateData: model.TaskPrivateData{
			BillingContext: &model.TaskBillingContext{OriginModelName: "billing-model"},
		},
	}

	require.Equal(t, "billing-model", TaskModel2Dto(task).ModelName)

	task.PrivateData.BillingContext.OriginModelName = ""
	require.Equal(t, "upstream-grok-video", TaskModel2Dto(task).ModelName)
}

func TestTaskModel2DtoSanitizesFullRedirectModelMetadata(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_public",
		FailReason: "upstream-video-model is unavailable",
		Properties: model.Properties{
			Input:             "keep upstream-video-model in user input",
			OriginModelName:   "video-alias",
			UpstreamModelName: "upstream-video-model",
		},
		PrivateData: model.TaskPrivateData{ModelMappingFullEnabled: true},
		Data:        json.RawMessage(`{"model":"upstream-video-model","result":{"modelVersion":"upstream-video-model"},"content":"upstream-video-model in content","fail_reason":"upstream-video-model failed"}`),
	}

	got := TaskModel2Dto(task)

	require.Equal(t, "video-alias", got.ModelName)
	require.Equal(t, "video-alias", got.Properties.(model.Properties).OriginModelName)
	require.Equal(t, "video-alias", got.Properties.(model.Properties).UpstreamModelName)
	require.Equal(t, "keep upstream-video-model in user input", got.Properties.(model.Properties).Input)
	require.Equal(t, "video-alias is unavailable", got.FailReason)
	require.Contains(t, string(got.Data), `"model":"video-alias"`)
	require.Contains(t, string(got.Data), `"modelVersion":"video-alias"`)
	require.Contains(t, string(got.Data), `"content":"upstream-video-model in content"`)
	require.Contains(t, string(got.Data), `"fail_reason":"video-alias failed"`)
}

func TestTaskModel2DtoPreservesLegacyMappingDetailsWhenFullRedirectDisabled(t *testing.T) {
	task := &model.Task{
		Properties: model.Properties{
			OriginModelName:   "video-alias",
			UpstreamModelName: "upstream-video-model",
		},
		Data: json.RawMessage(`{"model":"upstream-video-model"}`),
	}

	got := TaskModel2Dto(task)
	require.Equal(t, "upstream-video-model", got.Properties.(model.Properties).UpstreamModelName)
	require.JSONEq(t, `{"model":"upstream-video-model"}`, string(got.Data))
}

func TestTaskModel2DtoHidesFailedTaskDetailsForClient(t *testing.T) {
	task := &model.Task{
		Status:     model.TaskStatusFailure,
		FailReason: "provider account and endpoint leaked",
		Properties: model.Properties{Input: "provider secret", UpstreamModelName: "upstream-model"},
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://provider.example/result",
		},
		Data: json.RawMessage(`{"error":{"message":"provider secret"}}`),
	}

	hidden := TaskModel2Dto(task, false)
	require.Equal(t, dto.TaskFailureCode, hidden.FailReason)
	require.Nil(t, hidden.Data)
	require.Empty(t, hidden.Properties)
	require.Empty(t, hidden.ResultURL)
	require.Equal(t, "provider account and endpoint leaked", task.FailReason)
	require.NotNil(t, task.Data)

	visible := TaskModel2Dto(task, true)
	require.Equal(t, task.FailReason, visible.FailReason)
	require.Equal(t, task.Data, visible.Data)
	require.Equal(t, task.Properties, visible.Properties)
	require.Equal(t, task.PrivateData.ResultURL, visible.ResultURL)
}

func TestInitTaskPersistsFullRedirectStateOnlyWhenActive(t *testing.T) {
	info := &relaycommon.RelayInfo{
		OriginModelName:        "video-alias",
		ClientModelName:        "video-alias",
		ModelMappingTargetName: "upstream-video-model",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelSetting:    dto.ChannelSettings{ModelMappingFullEnabled: true},
			UpstreamModelName: "upstream-video-model",
			IsModelMapped:     true,
		},
	}

	require.True(t, model.InitTask(constant.TaskPlatform("openai"), info).PrivateData.ModelMappingFullEnabled)

	info.MarkModelMappingBypassed()
	require.False(t, model.InitTask(constant.TaskPlatform("openai"), info).PrivateData.ModelMappingFullEnabled)
}
