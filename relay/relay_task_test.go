package relay

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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
