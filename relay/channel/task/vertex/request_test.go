package vertex

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func videoTestContext(body string, native bool) (*gin.Context, *relaycommon.RelayInfo) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	path := "/v1/videos"
	if native {
		path = "/v1/models/veo-3.1-generate-preview:predictLongRunning"
		c.Set("vertex_native_video", true)
	}
	c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview", ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: dto.VertexKeyTypeJSON}}}
	return c, info
}
func TestVideoBodyAndBillingUseSameDuration(t *testing.T) {
	c, info := videoTestContext(`{"model":"veo-3.1-generate-preview","prompt":"cat","seconds":"6","metadata":{"generateAudio":false,"seed":0,"lastFrame":{"gcsUri":"gs://bucket/end.png","mimeType":"image/png"}},"image":"gs://bucket/start.png"}`, false)
	a := &TaskAdaptor{}
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	require.Nil(t, a.ValidateMappedRequest(c, info))
	body, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Contains(t, string(data), `"durationSeconds":6`)
	require.Contains(t, string(data), `"generateAudio":false`)
	require.Contains(t, string(data), `"seed":0`)
	require.Contains(t, string(data), `"lastFrame"`)
	require.Equal(t, 6.0, a.EstimateBilling(c, info)["seconds"])
}
func TestNativeVideoPreservesReferenceAndExtensionFields(t *testing.T) {
	for _, instance := range []string{`{"prompt":"scene","referenceImages":[{"image":{"gcsUri":"gs://bucket/ref.png"},"referenceType":"asset"}]}`, `{"video":{"gcsUri":"gs://bucket/source.mp4","mimeType":"video/mp4"},"mask":{"gcsUri":"gs://bucket/mask.png","maskMode":"insert"}}`} {
		c, info := videoTestContext(`{"instances":[`+instance+`],"parameters":{"sampleCount":3,"durationSeconds":8,"generateAudio":false,"task":"edit","customFlag":0}}`, true)
		a := &TaskAdaptor{}
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		require.Nil(t, a.ValidateMappedRequest(c, info))
		body, err := a.BuildRequestBody(c, info)
		require.NoError(t, err)
		data, _ := io.ReadAll(body)
		require.Contains(t, string(data), `"sampleCount":3`)
		require.Contains(t, string(data), `"customFlag":0`)
		require.Equal(t, 3.0, a.EstimateBilling(c, info)["n"])
	}
}
func TestVideoRejectsConflictsAndMultipleCompatibleResults(t *testing.T) {
	for _, body := range []string{`{"model":"veo-3.1-generate-preview","prompt":"cat","seconds":"6","duration":8}`, `{"model":"veo-3.1-generate-preview","prompt":"cat","n":2}`, `{"model":"veo-3.1-generate-preview","prompt":"cat","metadata":{"lastFrame":{"gcsUri":"gs://bucket/end.png"}}}`, `{"model":"veo-3.1-generate-preview","prompt":"cat","metadata":{"video":{"gcsUri":"gs://bucket/v.mp4"},"image":{"gcsUri":"gs://bucket/i.png"}}}`} {
		c, info := videoTestContext(body, false)
		a := &TaskAdaptor{}
		err := a.ValidateRequestAndSetAction(c, info)
		if err == nil {
			err = a.ValidateMappedRequest(c, info)
		}
		require.NotNil(t, err, body)
	}
}
func TestVideoResultsAndActualCountBilling(t *testing.T) {
	a := &TaskAdaptor{}
	result, err := a.ParseTaskResult([]byte(`{"done":true,"response":{"videos":[{}, {"gcsUri":"gs://bucket/out.mp4"}]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "gs://bucket/out.mp4", result.Url)
	result, err = a.ParseTaskResult([]byte(`{"done":true,"response":{"raiMediaFilteredCount":2,"videos":[]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, result.Status)
	task := &model.Task{Quota: 3000, PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{OtherRatios: map[string]float64{"n": 3}}}}
	task.Data = []byte(`{"done":true,"response":{"videos":[{"bytesBase64Encoded":"AQID"},{"gcsUri":"gs://bucket/out.mp4"}]}}`)
	require.Equal(t, 2000, a.AdjustBillingOnComplete(task, nil))

}
