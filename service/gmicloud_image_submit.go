package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type queuedGMICloudImageSubmitter interface {
	SubmitImageTask(context.Context, string, string, string, string) (*http.Response, error)
}

// The status CAS is also the durable submission claim. A process crash after
// claiming is ambiguous (the provider has no idempotency key); never replay it.
// Existing task timeout/refund handling resolves abandoned claims.
func submitQueuedGMICloudImage(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, task *model.Task) error {
	if task.Status != model.TaskStatusNotStart {
		return nil
	}
	submitter, ok := adaptor.(queuedGMICloudImageSubmitter)
	if !ok {
		return fmt.Errorf("GMICLOUD image submission is not supported by this adaptor")
	}
	if !TryAcquireChannelRPM(ctx, ch.Id, ch.GetSetting().RPMProtection).Allowed {
		return nil
	}
	task.Status, task.Progress, task.StartTime = model.TaskStatusSubmitted, "10%", time.Now().Unix()
	won, err := task.UpdateWithStatus(model.TaskStatusNotStart)
	if err != nil || !won {
		return err
	}

	key := task.PrivateData.Key
	if key == "" {
		key = ch.Key
	}
	// Independent of the client's shorter synchronous wait deadline.
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	resp, err := submitter.SubmitImageTask(ctx, ch.GetBaseURL(), key, task.PrivateData.GMICloudImageRequest, ch.GetSetting().Proxy)
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
		RecordChannelRPM429(ctx, ch.Id, ch.GetSetting().RPMProtection)
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
		if err == nil && statusCode != http.StatusOK && common.GetJsonType(body) == "object" {
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
	task.Status, task.Progress, task.Data = model.TaskStatusInProgress, "50%", body
	won, err = task.UpdateWithStatus(model.TaskStatusSubmitted)
	if err != nil || !won {
		return err
	}
	return ApplyTaskPollingResult(context.Background(), adaptor, ch, task, body, statusCode)
}
