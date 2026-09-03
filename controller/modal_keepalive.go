package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

const (
	modalKeepaliveTickInterval    = time.Second
	modalKeepaliveRefreshInterval = 5 * time.Second
	modalKeepaliveRequestTimeout  = 10 * time.Second
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
		Select("id", "name", "type", "key", "status", "base_url", "settings", "setting", "channel_info", "header_override").
		Where("type = ? AND status = ?", constant.ChannelTypeModal, common.ChannelStatusEnabled).
		Find(&channels).Error
	return channels, err
}

func keepModalChannelAlive(ctx context.Context, channel *model.Channel) error {
	if channel == nil {
		return fmt.Errorf("channel is nil")
	}
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return fmt.Errorf("select enabled key: %w", apiErr)
	}
	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		return err
	}

	requestURL := resolveFetchModelsURL(channel.Type, channel.GetBaseURL(), "")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	req.Header = headers

	client, err := service.NewProxyHttpClient(channel.GetSetting().Proxy)
	if err != nil {
		return err
	}
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

func runModalKeepaliveSweep(channels []*model.Channel, now time.Time, state *modalKeepaliveScheduleState) {
	for _, channel := range state.startDue(channels, now) {
		go func(channel *model.Channel) {
			defer state.finish(channel.Id)
			ctx, cancel := context.WithTimeout(context.Background(), modalKeepaliveRequestTimeout)
			defer cancel()

			if err := keepModalChannelAlive(ctx, channel); err != nil {
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
