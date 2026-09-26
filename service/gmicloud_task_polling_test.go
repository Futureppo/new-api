package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestGMICloudPollingTemporaryErrorsPreserveTask(t *testing.T) {
	for _, code := range []int{0, 429, 500, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			task, ch := seedAgnesPollingTask(t, BillingSourceWallet)
			ch.Type = constant.ChannelTypeGMICloud
			a := &agnesPollingFixture{httpStatus: code, transportError: code == 0, status: model.TaskStatusFailure}
			require.Error(t, RefreshVideoTask(context.Background(), a, ch, task))
			require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloadAgnesTask(t, task.ID).Status)
			require.Equal(t, 100000, getUserQuota(t, 1))
			require.Zero(t, countLogs(t))
		})
	}
}

type gmiImageSubmissionFixture struct {
	agnesPollingFixture
	posts   atomic.Int32
	key     string
	request string
}

func (a *gmiImageSubmissionFixture) SubmitImageTask(_ context.Context, _, key, body, _ string) (*http.Response, error) {
	a.posts.Add(1)
	a.key, a.request = key, body
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"request_id":"image-upstream","status":"success"}`))}, nil
}

func TestGMICloudDurableImageSubmissionClaim(t *testing.T) {
	task, ch := seedAgnesPollingTask(t, BillingSourceWallet)
	ch.Type = constant.ChannelTypeGMICloud
	task.Status, task.Platform, task.Action = model.TaskStatusNotStart, "67", constant.TaskActionImageGeneration
	task.PrivateData.UpstreamTaskID = ""
	task.PrivateData.GMICloudImageRequest = `{"model":"hy-image-v3.5-preview","payload":{"prompt":"cat","seed":0}}`
	require.NoError(t, task.Update())
	// Independent DB loads simulate restart recovery overlapping a foreground worker.
	first, second := reloadAgnesTask(t, task.ID), reloadAgnesTask(t, task.ID)
	a := &gmiImageSubmissionFixture{agnesPollingFixture: agnesPollingFixture{status: model.TaskStatusSuccess}}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, copy := range []*model.Task{first, second} {
		wg.Add(1)
		go func(copy *model.Task) { defer wg.Done(); errs <- RefreshVideoTask(context.Background(), a, ch, copy) }(copy)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, a.posts.Load())
	require.Equal(t, "saved-channel-key", a.key)
	require.Equal(t, task.PrivateData.GMICloudImageRequest, a.request)
	saved := reloadAgnesTask(t, task.ID)
	require.Equal(t, "image-upstream", saved.PrivateData.UpstreamTaskID)
	require.Empty(t, saved.PrivateData.GMICloudImageRequest)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), saved.Status)
	require.NoError(t, RefreshVideoTask(context.Background(), a, ch, saved))
	require.EqualValues(t, 1, a.posts.Load())
}

func TestGMICloudClaimedImageIsNeverResubmitted(t *testing.T) {
	task, ch := seedAgnesPollingTask(t, BillingSourceWallet)
	ch.Type = constant.ChannelTypeGMICloud
	task.Status = model.TaskStatusSubmitted
	task.PrivateData.GMICloudImageRequest = `{"model":"hy-image-v3.5-preview"}`
	require.NoError(t, task.Update())
	a := &gmiImageSubmissionFixture{}
	require.NoError(t, RefreshVideoTask(context.Background(), a, ch, reloadAgnesTask(t, task.ID)))
	require.Zero(t, a.posts.Load())
	require.Zero(t, a.calls.Load())
}

func TestGMICloudSubmissionAndPollingRefundOnce(t *testing.T) {
	task, ch := seedAgnesPollingTask(t, BillingSourceWallet)
	ch.Type = constant.ChannelTypeGMICloud
	a := &agnesPollingFixture{status: model.TaskStatusFailure}
	first, second := reloadAgnesTask(t, task.ID), reloadAgnesTask(t, task.ID)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, copy := range []*model.Task{first, second} {
		wg.Add(1)
		go func(copy *model.Task) {
			defer wg.Done()
			errs <- ApplyTaskPollingResult(context.Background(), a, ch, copy, []byte(`{"status":"failed"}`), http.StatusOK)
		}(copy)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 162500, getUserQuota(t, 1))
	require.Equal(t, 162500, getTokenRemainQuota(t, 1))
	require.EqualValues(t, 1, countLogs(t))
}
