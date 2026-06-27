package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskQueriesFilterByRequestId(t *testing.T) {
	truncateTables(t)

	insertTask(t, &Task{
		TaskID:    "task_req_match_user",
		RequestId: "req_task_query_match",
		UserId:    42,
		Status:    TaskStatusSuccess,
		Data:      json.RawMessage(`{}`),
	})
	insertTask(t, &Task{
		TaskID:    "task_req_other",
		RequestId: "req_task_query_other",
		UserId:    42,
		Status:    TaskStatusSuccess,
		Data:      json.RawMessage(`{}`),
	})
	insertTask(t, &Task{
		TaskID:    "task_req_match_other_user",
		RequestId: "req_task_query_match",
		UserId:    99,
		Status:    TaskStatusSuccess,
		Data:      json.RawMessage(`{}`),
	})

	params := SyncTaskQueryParams{RequestId: "req_task_query_match"}

	adminTasks := TaskGetAllTasks(0, 10, params)
	require.Len(t, adminTasks, 2)
	assert.Equal(t, int64(2), TaskCountAllTasks(params))

	userTasks := TaskGetAllUserTask(42, 0, 10, params)
	require.Len(t, userTasks, 1)
	assert.Equal(t, "task_req_match_user", userTasks[0].TaskID)
	assert.Equal(t, int64(1), TaskCountAllUserTask(42, params))
}
