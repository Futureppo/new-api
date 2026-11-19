package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type queuedGMICloudImageSubmitter interface {
	SubmitImageTask(context.Context, string, string, string, string) (*http.Response, error)
}

// The status CAS is also the durable submission claim. A process crash after
// claiming is ambiguous (the provider has no idempotency key); never replay it.
// Existing task timeout/refund handling resolves abandoned claims.
func submitQueuedGMICloudImage(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, task *model.Task) error {
	return submitGMICloudImage(ctx, adaptor, ch, task, task.PrivateData.GMICloudImageRequest)
}

// SubmitPlaygroundGMICloudImage gives the submitter the request directly from
// memory. The persisted task contains the submission state and upstream ID.
func SubmitPlaygroundGMICloudImage(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, task *model.Task, body string) error {
	return submitGMICloudImage(common.WithPlaygroundPrivacy(ctx), adaptor, ch, task, body)
}

func submitGMICloudImage(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, task *model.Task, requestBody string) error {
	if task.Status != model.TaskStatusNotStart {
		return nil
	}
	submitter, ok := adaptor.(queuedGMICloudImageSubmitter)
	if !ok {
		return fmt.Errorf("GMICLOUD image submission is not supported by this adaptor")
	}
	rpm := ch.GetSetting().RPMProtection
	allowed := TryAcquireChannelRPM(ctx, ch.Id, rpm).Allowed
	if !allowed && !task.PrivateData.Playground {
		return nil
	}
	if !allowed {
		// Other jobs can wait for the durable poller. The playground worker
		// owns the only copy of this request, so wait for capacity in memory.
		waitLimit := 180 * time.Second
		if constant.TaskTimeoutMinutes > 0 {
			waitLimit = time.Until(time.Unix(task.SubmitTime, 0).Add(time.Duration(constant.TaskTimeoutMinutes) * time.Minute))
		}
		wait, cancel := context.WithTimeout(ctx, waitLimit)
		ticker := time.NewTicker(time.Second)
		for !allowed && wait.Err() == nil {
			select {
			case <-wait.Done():
			case <-ticker.C:
				allowed = TryAcquireChannelRPM(wait, ch.Id, rpm).Allowed
			}
		}
		ticker.Stop()
		cancel()
	}
	task.Status, task.Progress, task.StartTime = model.TaskStatusSubmitted, "10%", time.Now().Unix()
	won, err := task.UpdateWithStatus(model.TaskStatusNotStart)
	if err != nil || !won {
		return err
	}
	if !allowed {
		body, _ := common.Marshal(map[string]string{"status": "failed", "message": "上游渠道限流，提交失败"})
		return ApplyTaskPollingResult(ctx, adaptor, ch, task, body, http.StatusTooManyRequests)
	}

	key := task.PrivateData.Key
	if key == "" {
		key = ch.Key
	}
	// Independent of the client's shorter synchronous wait deadline.
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	resp, err := submitter.SubmitImageTask(ctx, ch.GetBaseURL(), key, requestBody, ch.GetSetting().Proxy)
	statusCode := http.StatusBadGateway
	var body []byte
	if resp != nil {
		defer resp.Body.Close()
		statusCode = resp.StatusCode
		if err == nil {
			body, err = io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
		}
	}
	if statusCode == http.StatusTooManyRequests {
		RecordChannelRPM429(ctx, ch.Id, rpm)
	}
	var envelope struct {
		RequestID string `json:"request_id"`
	}
	decodeErr := common.Unmarshal(body, &envelope)
	if err != nil || statusCode != http.StatusOK || decodeErr != nil || strings.TrimSpace(envelope.RequestID) == "" {
		// Do not expose transport errors, URLs, headers or credentials. Preserve
		// a structured provider failure body for its usual error-details policy.
		reason := fmt.Sprintf("GMICLOUD image submission failed (HTTP %d); do not automatically resubmit", statusCode)
		if err != nil || (statusCode == http.StatusOK && decodeErr != nil) {
			reason = "GMICLOUD image submission result is unknown; check the provider console before resubmitting"
		}
		failure := map[string]any{"status": "failed", "message": reason}
		if !task.PrivateData.Playground && err == nil && statusCode != http.StatusOK && common.GetJsonType(body) == "object" {
			var upstream map[string]any
			if common.Unmarshal(body, &upstream) == nil {
				failure["error"] = upstream["error"]
			}
		}
		body, _ = common.Marshal(failure)
		task.PrivateData.GMICloudImageRequest = ""
		return ApplyTaskPollingResult(context.Background(), adaptor, ch, task, body, statusCode)
	}
	// Save the upstream ID before interpreting its state. A crash afterwards
	// resumes with GET, never another billable POST.
	task.PrivateData.UpstreamTaskID = envelope.RequestID
	task.PrivateData.GMICloudImageRequest = ""
	task.Status, task.Progress = model.TaskStatusInProgress, "50%"
	task.Data = body
	if task.PrivateData.Playground {
		task.Data = PlaygroundTaskResponse(body)
	}
	won, err = task.UpdateWithStatus(model.TaskStatusSubmitted)
	if err != nil || !won {
		return err
	}
	return ApplyTaskPollingResult(context.Background(), adaptor, ch, task, body, statusCode)
}
