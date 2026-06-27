package openailocal

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDoResponseRewritesPublicTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	resp := &http.Response{
		Body: io.NopCloser(strings.NewReader(`{"id":"upstream_123","status":"queued","kind":"ppt"}`)),
	}
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}
	info.Action = constant.TaskActionPPT

	upstreamID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.Equal(t, "upstream_123", upstreamID)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(taskData, &payload))
	require.Equal(t, "task_public", payload["id"])
	require.Equal(t, "task_public", payload["taskId"])
	require.Equal(t, "ppt", payload["kind"])
	require.Contains(t, w.Body.String(), "task_public")
	require.NotContains(t, w.Body.String(), "upstream_123")
}

func TestParseTaskResultMapsSuccessAndFileURLs(t *testing.T) {
	body := []byte(`{
		"items": [{
			"id": "upstream_123",
			"status": "success",
			"kind": "psd",
			"result": {
				"primary_url": "/files/result.psd",
				"zip_url": "/files/assets.zip"
			}
		}]
	}`)

	taskInfo, err := (&TaskAdaptor{}).ParseTaskResult(body)
	require.NoError(t, err)
	require.Equal(t, string(model.TaskStatusSuccess), taskInfo.Status)
	require.Equal(t, "/files/result.psd", taskInfo.Url)
	require.Equal(t, "100%", taskInfo.Progress)
}

func TestNormalizeEditableFileTaskURLs(t *testing.T) {
	body := []byte(`{
		"items": [{
			"result": {
				"primary_url": "/files/result.psd",
				"zip_url": "files/assets.zip"
			}
		}]
	}`)

	normalized := normalizeEditableFileTaskURLs("https://local.openai.com", body)
	var payload editableFileTasksResponse
	require.NoError(t, common.Unmarshal(normalized, &payload))
	require.Equal(t, "https://local.openai.com/files/result.psd", payload.Items[0].Result.PrimaryURL)
	require.Equal(t, "https://local.openai.com/files/assets.zip", payload.Items[0].Result.ZipURL)
}
