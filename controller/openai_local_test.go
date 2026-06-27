package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAILocalEditableFileTasksReturnsItemsAndMissingIDs(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		TaskID:     "task_file_ok",
		UserId:     7,
		Platform:   constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAILocal)),
		Action:     constant.TaskActionPPT,
		Status:     model.TaskStatusSuccess,
		SubmitTime: 100,
		FinishTime: 110,
	}
	task.SetData(map[string]any{
		"items": []map[string]any{
			{
				"result": map[string]any{
					"primary_url": "https://local.openai.com/files/result.pptx",
					"zip_url":     "https://local.openai.com/files/assets.zip",
				},
			},
		},
	})
	require.NoError(t, task.Insert())

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 7)
	req := httptest.NewRequest(http.MethodGet, "/v1/editable-file-tasks?ids=task_file_ok,task_missing", nil)
	c.Request = req

	OpenAILocalEditableFileTasks(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp openAILocalEditableFileTasksResponse
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	require.Equal(t, "task_file_ok", resp.Items[0].ID)
	require.Equal(t, "success", resp.Items[0].Status)
	require.Equal(t, "https://local.openai.com/files/result.pptx", resp.Items[0].Result.PrimaryURL)
	require.Equal(t, []string{"task_missing"}, resp.MissingIDs)
}

func TestOpenAILocalEditableFileTasksFiltersOtherUsers(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))

	task := &model.Task{
		TaskID:   "task_other_user",
		UserId:   8,
		Platform: constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeOpenAILocal)),
		Action:   constant.TaskActionPSD,
		Status:   model.TaskStatusSuccess,
	}
	require.NoError(t, task.Insert())

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("id", 7)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/editable-file-tasks?ids=task_other_user", nil)

	OpenAILocalEditableFileTasks(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp openAILocalEditableFileTasksResponse
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &resp))
	require.Empty(t, resp.Items)
	require.Equal(t, []string{"task_other_user"}, resp.MissingIDs)
}
