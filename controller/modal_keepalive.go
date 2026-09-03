package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	modalchannel "github.com/QuantumNous/new-api/relay/channel/modal"
	"github.com/QuantumNous/new-api/service"
)

const (
	modalKeepaliveTickInterval    = time.Second
	modalKeepaliveRefreshInterval = 5 * time.Second
	modalKeepaliveRequestTimeout  = 60 * time.Second
)

var modalKeepaliveTaskOnce sync.Once

type modalKeepaliveScheduleState struct {
	mu          sync.Mutex
	lastAttempt map[int]int64
	inFlight    map[int]struct{}
}

func newModalKeepaliveScheduleState() *modalKeepaliveScheduleState {
	return &modalKeepaliveScheduleState{
		lastAttempt: make(map[int]int64),
		inFlight:    make(map[int]struct{}),
	}
}

// startDue atomically selects due channels and marks them in flight. Recording
// the attempt before the request starts prevents a slow deployment from
// accumulating overlapping keepalive requests.
func (s *modalKeepaliveScheduleState) startDue(channels []*model.Channel, now time.Time) []*model.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowUnix := now.Unix()
	due := make([]*model.Channel, 0)
	for _, channel := range channels {
		if channel == nil || channel.Id <= 0 || channel.Type != constant.ChannelTypeModal || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		settings := channel.GetOtherSettings()
		if !settings.ModalKeepaliveEnabled {
			continue
		}
		if _, running := s.inFlight[channel.Id]; running {
			continue
		}
		if last, attempted := s.lastAttempt[channel.Id]; attempted && nowUnix-last < int64(settings.ModalKeepaliveInterval()) {
			continue
		}

		s.lastAttempt[channel.Id] = nowUnix
		s.inFlight[channel.Id] = struct{}{}
		due = append(due, channel)
	}
	return due
}

func (s *modalKeepaliveScheduleState) finish(channelID int) {
	s.mu.Lock()
	delete(s.inFlight, channelID)
	s.mu.Unlock()
}

func loadModalKeepaliveChannels() ([]*model.Channel, error) {
	var channels []*model.Channel
	err := model.DB.
		Select("id", "name", "type", "key", "status", "base_url", "models", "model_mapping", "settings", "setting", "channel_info", "header_override").
		Where("type = ? AND status = ?", constant.ChannelTypeModal, common.ChannelStatusEnabled).
		Find(&channels).Error
	return channels, err
}

type modalKeepaliveChatRequest struct {
	Model       string                  `json:"model"`
	Messages    []modalKeepaliveMessage `json:"messages"`
	Temperature float64                 `json:"temperature"`
	MaxTokens   int                     `json:"max_tokens"`
	Stream      bool                    `json:"stream"`
}

type modalKeepaliveMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func resolveModalKeepaliveModels(channel *model.Channel) ([]string, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}

	configuredModels := normalizeModelNames(channel.GetModels())
	if len(configuredModels) == 0 {
		return nil, fmt.Errorf("channel has no configured models")
	}

	mapping := normalizeChannelModelMapping(channel)
	resolvedModels := make([]string, 0, len(configuredModels))
	seenResolvedModels := make(map[string]struct{}, len(configuredModels))
	for _, configuredModel := range configuredModels {
		resolvedModel := configuredModel
		visited := map[string]struct{}{resolvedModel: {}}
		for {
			mappedModel, ok := mapping[resolvedModel]
			if !ok {
				break
			}
			if mappedModel == resolvedModel {
				break
			}
			if _, seen := visited[mappedModel]; seen {
				return nil, fmt.Errorf("model mapping contains cycle for %q", configuredModel)
			}
			visited[mappedModel] = struct{}{}
			resolvedModel = mappedModel
		}
		if _, seen := seenResolvedModels[resolvedModel]; seen {
			continue
		}
		seenResolvedModels[resolvedModel] = struct{}{}
		resolvedModels = append(resolvedModels, resolvedModel)
	}
	return resolvedModels, nil
}

func keepModalModelAlive(ctx context.Context, client *http.Client, requestURL string, headers http.Header, modelName string) error {
	payload, err := common.Marshal(modalKeepaliveChatRequest{
		Model: modelName,
		Messages: []modalKeepaliveMessage{
			{Role: "user", Content: "ping"},
		},
		Temperature: 0,
		MaxTokens:   1,
		Stream:      false,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header = headers.Clone()
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

func keepModalChannelAlive(ctx context.Context, channel *model.Channel) error {
	if channel == nil {
		return fmt.Errorf("channel is nil")
	}
	models, err := resolveModalKeepaliveModels(channel)
	if err != nil {
		return err
	}
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return fmt.Errorf("select enabled key: %w", apiErr)
	}
	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		return err
	}

	client, err := service.NewProxyHttpClient(channel.GetSetting().Proxy)
	if err != nil {
		return err
	}

	requestURL := fmt.Sprintf("%s/v1/chat/completions", modalchannel.NormalizeBaseURL(channel.GetBaseURL()))
	errs := make([]error, 0)
	for _, modelName := range models {
		requestCtx, cancel := context.WithTimeout(ctx, modalKeepaliveRequestTimeout)
		err := keepModalModelAlive(requestCtx, client, requestURL, headers, modelName)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("model %q: %w", modelName, err))
		}
	}
	return errors.Join(errs...)
}

func runModalKeepaliveSweep(channels []*model.Channel, now time.Time, state *modalKeepaliveScheduleState) {
	for _, channel := range state.startDue(channels, now) {
		go func(channel *model.Channel) {
			defer state.finish(channel.Id)

			if err := keepModalChannelAlive(context.Background(), channel); err != nil {
				common.SysLog(fmt.Sprintf("Modal keepalive failed: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, err))
			} else if common.DebugEnabled {
				common.SysLog(fmt.Sprintf("Modal keepalive succeeded: channel_id=%d channel_name=%s", channel.Id, channel.Name))
			}
		}(channel)
	}
}

func StartModalKeepaliveTask() {
	modalKeepaliveTaskOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}

		go func() {
			common.SysLog("Modal keepalive task started")
			state := newModalKeepaliveScheduleState()
			var channels []*model.Channel
			nextRefresh := time.Time{}
			ticker := time.NewTicker(modalKeepaliveTickInterval)
			defer ticker.Stop()

			for now := time.Now(); ; now = <-ticker.C {
				if !now.Before(nextRefresh) {
					refreshed, err := loadModalKeepaliveChannels()
					if err != nil {
						channels = nil
						common.SysLog(fmt.Sprintf("Modal keepalive channel refresh failed: %v", err))
					} else {
						channels = refreshed
					}
					nextRefresh = now.Add(modalKeepaliveRefreshInterval)
				}
				runModalKeepaliveSweep(channels, now, state)
			}
		}()
	})
}
