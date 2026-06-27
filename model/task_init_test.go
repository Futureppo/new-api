package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func TestInitTaskPersistsPollingKeyForMultiKeyChannel(t *testing.T) {
	info := newTaskRelayInfo(&relaycommon.ChannelMeta{
		ChannelType:       constant.ChannelTypeXai,
		ChannelId:         301,
		ChannelIsMultiKey: true,
		ApiKey:            "xai-selected-key",
	})

	task := InitTask(constant.TaskPlatform("48"), info)

	if task.PrivateData.Key != "xai-selected-key" {
		t.Fatalf("expected selected key to be persisted, got %q", task.PrivateData.Key)
	}
}

func TestInitTaskDoesNotPersistSingleKeyForRegularTaskChannel(t *testing.T) {
	info := newTaskRelayInfo(&relaycommon.ChannelMeta{
		ChannelType: constant.ChannelTypeXai,
		ChannelId:   310,
		ApiKey:      "xai-single-key",
	})

	task := InitTask(constant.TaskPlatform("48"), info)

	if task.PrivateData.Key != "" {
		t.Fatalf("expected single-key channel not to persist key, got %q", task.PrivateData.Key)
	}
}

func TestInitTaskKeepsGeminiPollingKeyBehavior(t *testing.T) {
	info := newTaskRelayInfo(&relaycommon.ChannelMeta{
		ChannelType: constant.ChannelTypeGemini,
		ChannelId:   120,
		ApiKey:      "gemini-key",
	})

	task := InitTask(constant.TaskPlatform("24"), info)

	if task.PrivateData.Key != "gemini-key" {
		t.Fatalf("expected Gemini key to be persisted, got %q", task.PrivateData.Key)
	}
}

func TestInitTaskPersistsRequestId(t *testing.T) {
	info := newTaskRelayInfo(&relaycommon.ChannelMeta{
		ChannelType: constant.ChannelTypeXai,
		ChannelId:   301,
		ApiKey:      "xai-key",
	})

	task := InitTask(constant.TaskPlatform("48"), info)

	if task.RequestId != "req_task_init_test" {
		t.Fatalf("expected request id to be persisted, got %q", task.RequestId)
	}
}

func newTaskRelayInfo(meta *relaycommon.ChannelMeta) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          7,
		UsingGroup:      "default",
		RequestId:       "req_task_init_test",
		OriginModelName: "grok-imagine-video-1.5-preview",
		ChannelMeta:     meta,
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			PublicTaskID: "task_test_public_id",
		},
	}
}
