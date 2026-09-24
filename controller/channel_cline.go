package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/cline"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func isClineManagedModelSyncEnabled(channel *model.Channel, settings dto.ChannelOtherSettings) bool {
	return channel != nil && channel.Type == constant.ChannelTypeCline && settings.ShouldSyncClineFreeModels()
}

func fetchClineModelIDs(ctx context.Context, channel *model.Channel, baseURL, key, customURL string, freeOnly bool) ([]string, error) {
	fetchURL := strings.TrimSpace(customURL)
	if fetchURL == "" {
		fetchURL = cline.NormalizeBaseURL(baseURL) + "/v1/models"
		if freeOnly {
			fetchURL = cline.NormalizeBaseURL(baseURL) + cline.FreeModelsPath
		}
	}
	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("Cline 模型列表地址无效")
	}
	req.Header = headers
	client, err := service.NewProxyHttpClient(channel.GetSetting().Proxy)
	if err != nil {
		return nil, fmt.Errorf("Cline 模型列表代理配置无效")
	}
	resp, err := client.Do(req)
	if err != nil {
		// Do not log URLs, headers or credentials from a transport error.
		return nil, fmt.Errorf("获取 Cline 模型列表失败，请检查网络或代理配置")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 Cline 模型列表失败: HTTP %d", resp.StatusCode)
	}
	const maxCatalogBytes = 8 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil || len(body) > maxCatalogBytes {
		return nil, fmt.Errorf("读取 Cline 模型列表失败")
	}
	if !freeOnly {
		ids, ok := parseModelIDsFromResponseBody(body)
		if !ok {
			return nil, fmt.Errorf("解析 Cline 模型列表失败")
		}
		return normalizeModelNames(ids), nil
	}
	var catalog struct {
		Success *bool `json:"success"`
		Free    []struct {
			ID string `json:"id"`
		} `json:"free"`
	}
	if err := common.Unmarshal(body, &catalog); err != nil || (catalog.Success != nil && !*catalog.Success) {
		return nil, fmt.Errorf("解析 Cline 免费模型列表失败")
	}
	if len(catalog.Free) == 0 {
		return nil, fmt.Errorf("Cline 模型列表未返回任何官方免费模型")
	}
	ids := make([]string, 0, len(catalog.Free))
	for _, item := range catalog.Free {
		id := strings.TrimSpace(item.ID)
		if id == "" || len(id) > 255 || strings.ContainsAny(id, ",\r\n") {
			return nil, fmt.Errorf("Cline 免费模型列表包含无效模型 ID")
		}
		ids = append(ids, id)
	}
	return normalizeModelNames(ids), nil
}

func buildClineManagedModelChanges(channel *model.Channel, settings dto.ChannelOtherSettings, upstream []string) (add, remove, managed []string) {
	current := channel.GetModels()
	mappings := normalizeChannelModelMapping(channel)
	managed = mergeModelNames(settings.ClineFreeModelManagedModels, intersectModelNames(current, upstream))
	for _, id := range upstream {
		if _, manual := mappings[id]; !manual && !slices.Contains(current, id) && !isIgnoredUpstreamModel(id, settings.UpstreamModelUpdateIgnoredModels) {
			add = append(add, id)
		}
	}
	for _, id := range managed {
		if _, manual := mappings[id]; manual {
			managed = subtractModelNames(managed, []string{id})
			continue
		}
		if slices.Contains(current, id) && !slices.Contains(upstream, id) {
			remove = append(remove, id)
		}
	}
	return normalizeModelNames(add), normalizeModelNames(remove), managed
}

// Update model availability and its ownership together. Optimistic matching
// prevents an in-flight catalogue request from overwriting an administrator edit.
func persistClineModelUpdates(original model.Channel, channel *model.Channel, settings dto.ChannelOtherSettings, modelsChanged bool) error {
	channel.SetOtherSettings(settings)
	if !modelsChanged && original.OtherSettings == channel.OtherSettings {
		return nil
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.Channel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, original.Id).Error; err != nil {
			return err
		}
		// Compare keys in memory rather than including credentials in SQL logs.
		if current.Key != original.Key {
			return fmt.Errorf("Cline 渠道密钥已变化，请重新检测模型列表")
		}
		updates := map[string]any{"settings": channel.OtherSettings}
		if modelsChanged {
			updates["models"] = channel.Models
		}
		result := tx.Model(&model.Channel{}).Where(map[string]any{
			"id": original.Id, "type": original.Type, "status": original.Status,
			"settings": original.OtherSettings, "models": original.Models,
			"model_mapping": original.ModelMapping, "base_url": original.BaseURL,
			"setting": original.Setting, "header_override": original.HeaderOverride,
			"group": original.Group, "priority": original.Priority, "weight": original.Weight, "tag": original.Tag,
		}).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("Cline 渠道配置已变化，请重新检测模型列表")
		}
		if modelsChanged {
			return channel.UpdateAbilities(tx)
		}
		return nil
	})
	if err != nil {
		*channel = original
	}
	return err
}

func prepareClineModelUpdates(channel *model.Channel, settings *dto.ChannelOtherSettings, addInput, ignoreInput, removeInput []string) (added, removed, remaining, remainingRemove []string, changed bool) {
	add := intersectModelNames(addInput, settings.UpstreamModelUpdateLastDetectedModels)
	ignored := intersectModelNames(ignoreInput, settings.UpstreamModelUpdateLastDetectedModels)
	remove := intersectModelNames(removeInput, settings.UpstreamModelUpdateLastRemovedModels)
	remove = subtractModelNames(intersectModelNames(remove, settings.ClineFreeModelManagedModels), add)
	for source := range normalizeChannelModelMapping(channel) {
		add = subtractModelNames(add, []string{source})
		remove = subtractModelNames(remove, []string{source})
		settings.ClineFreeModelManagedModels = subtractModelNames(settings.ClineFreeModelManagedModels, []string{source})
	}
	original := normalizeModelNames(channel.GetModels())
	next := applySelectedModelChanges(original, add, remove)
	channel.Models = strings.Join(next, ",")
	settings.ClineFreeModelManagedModels = intersectModelNames(mergeModelNames(settings.ClineFreeModelManagedModels, add), next)
	settings.UpstreamModelUpdateIgnoredModels = subtractModelNames(mergeModelNames(settings.UpstreamModelUpdateIgnoredModels, ignored), add)
	remaining = subtractModelNames(settings.UpstreamModelUpdateLastDetectedModels, mergeModelNames(add, ignored))
	remainingRemove = subtractModelNames(settings.UpstreamModelUpdateLastRemovedModels, remove)
	settings.UpstreamModelUpdateLastDetectedModels = remaining
	settings.UpstreamModelUpdateLastRemovedModels = remainingRemove
	return subtractModelNames(next, original), subtractModelNames(original, next), remaining, remainingRemove, !slices.Equal(original, next)
}

func checkAndPersistClineModelUpdates(channel *model.Channel, settings *dto.ChannelOtherSettings, force, allowAutoApply bool) (bool, channelUpstreamAutoApplyResult, error) {
	now := common.GetTimestamp()
	if !force && settings.UpstreamModelUpdateLastCheckTime > 0 && now-settings.UpstreamModelUpdateLastCheckTime < getUpstreamModelUpdateMinCheckIntervalSeconds() {
		return false, channelUpstreamAutoApplyResult{}, nil
	}
	original := *channel
	upstream, fetchErr := fetchChannelUpstreamModelIDs(channel)
	settings.UpstreamModelUpdateLastCheckTime = now
	changed := false
	result := channelUpstreamAutoApplyResult{}
	if fetchErr == nil {
		add, remove, managed := buildClineManagedModelChanges(channel, *settings, upstream)
		settings.ClineFreeModelManagedModels = managed
		settings.UpstreamModelUpdateLastDetectedModels = add
		settings.UpstreamModelUpdateLastRemovedModels = remove
		if allowAutoApply {
			result.AddedModels, result.RemovedModels, _, _, changed = prepareClineModelUpdates(channel, settings, add, nil, remove)
		}
	}
	if err := persistClineModelUpdates(original, channel, *settings, changed); err != nil {
		*settings = channel.GetOtherSettings()
		return false, channelUpstreamAutoApplyResult{}, err
	}
	return changed, result, fetchErr
}

func applyClineModelUpdates(channel *model.Channel, addInput, ignoreInput, removeInput []string) (added, removed, remaining, remainingRemove []string, changed bool, err error) {
	original := *channel
	settings := channel.GetOtherSettings()
	added, removed, remaining, remainingRemove, changed = prepareClineModelUpdates(channel, &settings, addInput, ignoreInput, removeInput)
	settings.UpstreamModelUpdateLastCheckTime = common.GetTimestamp()
	err = persistClineModelUpdates(original, channel, settings, changed)
	if err != nil {
		return nil, nil, nil, nil, false, err
	}
	return
}

// Run once after a saved channel becomes eligible; subsequent checks use the
// existing master-node scheduler and its global settings.
func syncClineModelsAfterSave(channel *model.Channel) {
	if !common.IsMasterNode || !common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true) || channel.Status != common.ChannelStatusEnabled {
		return
	}
	settings := channel.GetOtherSettings()
	if !isClineManagedModelSyncEnabled(channel, settings) {
		return
	}
	changed, _, err := checkAndPersistClineModelUpdates(channel, &settings, true, true)
	if err != nil {
		common.SysLog(fmt.Sprintf("Cline free model sync failed: channel_id=%d err=%v", channel.Id, err))
	}
	if changed {
		refreshChannelRuntimeCache()
	}
}
