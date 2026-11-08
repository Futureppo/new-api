package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyFetchHidesFailedTaskMetadata(t *testing.T) {
	previousForward := setting.MjForwardUrlEnabled
	setting.MjForwardUrlEnabled = false
	t.Cleanup(func() { setting.MjForwardUrlEnabled = previousForward })
	for _, showDetails := range []bool{false, true} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(midjourneyErrorVisibilityContextKey, map[int]bool{42: showDetails})
		task := &model.Midjourney{
			ChannelId: 42, Status: "FAILURE", FailReason: "private failure", Description: "private description",
			State: "private state", ImageUrl: "https://private.example/image", VideoUrl: "https://private.example/video",
			VideoUrls: `[{"url":"https://private.example/video"}]`, Buttons: `[{"label":"private"}]`,
		}
		got := coverMidjourneyTaskDto(c, task)
		if showDetails {
			require.Equal(t, task.FailReason, got.FailReason)
			require.Equal(t, task.State, got.State)
			require.Equal(t, task.ImageUrl, got.ImageUrl)
		} else {
			require.Equal(t, dto.TaskFailureCode, got.FailReason)
			require.Equal(t, dto.TaskFailureCode, got.Description)
			require.Empty(t, got.State)
			require.Empty(t, got.ImageUrl)
			require.Empty(t, got.VideoUrl)
			require.Empty(t, got.VideoUrls)
			require.Empty(t, got.Buttons)
		}
		require.Equal(t, "private failure", task.FailReason)
		task.Status, task.FailReason = "SUCCESS", ""
		success := coverMidjourneyTaskDto(c, task)
		require.Equal(t, task.ImageUrl, success.ImageUrl)
		require.Equal(t, task.State, success.State)
	}
}
