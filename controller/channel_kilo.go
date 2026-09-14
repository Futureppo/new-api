package controller

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

func isKiloManagedModelSyncEnabled(channel *model.Channel, settings dto.ChannelOtherSettings) bool {
	return channel != nil && channel.Type == constant.ChannelTypeKilo && settings.KiloFreeModelSyncEnabled
}

func fetchKiloModelIDs(channel *model.Channel, fetchURL, key string, freeOnly bool) ([]string, error) {
	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		return nil, err
	}
	body, err := GetResponseBody(http.MethodGet, fetchURL, channel, headers)
	if err != nil {
		return nil, fmt.Errorf("获取 Kilo 模型列表失败: %w", err)
	}
	var response struct {
		Data []struct {
			ID     string `json:"id"`
			IsFree *bool  `json:"isFree"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("解析 Kilo 模型列表失败: %w", err)
	}
	if response.Data == nil {
		return nil, fmt.Errorf("Kilo 模型列表缺少 data")
	}
	ids := make([]string, 0, len(response.Data))
	for _, item := range response.Data {
		if freeOnly && item.IsFree == nil {
			return nil, fmt.Errorf("Kilo 模型列表缺少 isFree 标记，无法同步免费模型")
		}
		if !freeOnly || *item.IsFree {
			ids = append(ids, item.ID)
		}
	}
	ids = normalizeModelNames(ids)
	if freeOnly && len(ids) == 0 {
		return nil, fmt.Errorf("Kilo 模型列表未返回任何免费模型")
	}
	return ids, nil
}

// Reuse the established alias conflict and manual-mapping rules, but determine
// ownership from Kilo metadata and previous syncs instead of model-name suffixes.
func buildKiloManagedModelChanges(channel *model.Channel, settings dto.ChannelOtherSettings, upstreamModels []string) (add, remove, managed []string, mappings map[string]string) {
	plan := buildOpenRouterManagedModelPlan(channel, upstreamModels,
		settings.KiloFreeModelNameSimplificationEnabled, settings.KiloFreeModelGeneratedMappings)
	add, _ = collectPendingOpenRouterManagedModelChangesFromModels(channel.GetModels(), plan, settings.UpstreamModelUpdateIgnoredModels)
	managed = mergeModelNames(settings.KiloFreeModelManagedModels, intersectModelNames(channel.GetModels(), upstreamModels))
	for alias := range settings.KiloFreeModelGeneratedMappings {
		if _, owned := plan.CurrentManagedMappings[alias]; !owned {
			// Removing an automatic mapping also relinquishes ownership of its alias.
			managed = subtractModelNames(managed, []string{alias})
		}
	}
	for alias := range plan.CurrentManagedMappings {
		managed = mergeModelNames(managed, []string{alias})
	}
	for _, name := range intersectModelNames(channel.GetModels(), managed) {
		if _, manual := plan.PreservedModelMappings[name]; manual {
			managed = subtractModelNames(managed, []string{name})
			continue
		}
		if !slices.Contains(plan.DesiredModels, name) {
			remove = append(remove, name)
		}
	}
	return add, normalizeModelNames(remove), managed, plan.DesiredMappings
}

func checkAndPersistKiloModelUpdates(channel *model.Channel, settings *dto.ChannelOtherSettings, force, allowAutoApply bool) (bool, channelUpstreamAutoApplyResult, error) {
	now := common.GetTimestamp()
	if !force && settings.UpstreamModelUpdateLastCheckTime > 0 && now-settings.UpstreamModelUpdateLastCheckTime < getUpstreamModelUpdateMinCheckIntervalSeconds() {
		return false, channelUpstreamAutoApplyResult{}, nil
	}
	upstreamModels, err := fetchChannelUpstreamModelIDs(channel)
	settings.UpstreamModelUpdateLastCheckTime = now
	if err != nil {
		if saveErr := updateChannelUpstreamModelSettings(channel, *settings, false); saveErr != nil {
			return false, channelUpstreamAutoApplyResult{}, saveErr
		}
		return false, channelUpstreamAutoApplyResult{}, err
	}
	add, remove, managed, mappings := buildKiloManagedModelChanges(channel, *settings, upstreamModels)
	settings.KiloFreeModelManagedModels = managed
	settings.KiloFreeModelPendingMappings = filterModelMappingsBySources(mappings, add)
	settings.UpstreamModelUpdateLastDetectedModels = add
	settings.UpstreamModelUpdateLastRemovedModels = remove
	if !allowAutoApply {
		return false, channelUpstreamAutoApplyResult{}, updateChannelUpstreamModelSettings(channel, *settings, false)
	}
	channel.SetOtherSettings(*settings)
	added, removed, _, _, changed, err := applyKiloModelUpdates(channel, add, nil, remove)
	*settings = channel.GetOtherSettings()
	return changed, channelUpstreamAutoApplyResult{AddedModels: added, RemovedModels: removed}, err
}

func applyKiloModelUpdates(channel *model.Channel, addInput, ignoreInput, removeInput []string) (added, removed, remaining, remainingRemove []string, changed bool, err error) {
	settings := channel.GetOtherSettings()
	pendingAdd := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
	pendingRemove := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
	add := intersectModelNames(addInput, pendingAdd)
	ignored := intersectModelNames(ignoreInput, pendingAdd)
	remove := subtractModelNames(intersectModelNames(removeInput, pendingRemove), add)
	currentMapping := normalizeChannelModelMapping(channel)
	generated := collectManagedOpenRouterFreeModelMappings(currentMapping, settings.KiloFreeModelGeneratedMappings)
	for alias := range settings.KiloFreeModelGeneratedMappings {
		if _, owned := generated[alias]; !owned {
			remove = subtractModelNames(remove, []string{alias})
			settings.KiloFreeModelManagedModels = subtractModelNames(settings.KiloFreeModelManagedModels, []string{alias})
		}
	}
	for alias := range settings.KiloFreeModelPendingMappings {
		if _, owned := generated[alias]; !owned && slices.Contains(channel.GetModels(), alias) {
			add = subtractModelNames(add, []string{alias})
		}
	}
	// A mapping may have been edited since detection. Never remove a manual mapping
	// or replace it with a pending generated alias from an older snapshot.
	for source := range currentMapping {
		if _, owned := generated[source]; !owned {
			remove = subtractModelNames(remove, []string{source})
			if _, pendingAlias := settings.KiloFreeModelPendingMappings[source]; pendingAlias {
				add = subtractModelNames(add, []string{source})
			}
		}
	}
	original := normalizeModelNames(channel.GetModels())
	next := applySelectedModelChanges(original, add, remove)
	for _, name := range remove {
		if _, owned := generated[name]; owned {
			delete(currentMapping, name)
			delete(generated, name)
		}
	}
	for _, name := range add {
		if target := settings.KiloFreeModelPendingMappings[name]; target != "" {
			currentMapping[name] = target
			generated[name] = target
		}
	}
	mappingChanged, err := setChannelModelMapping(channel, currentMapping)
	if err != nil {
		return nil, nil, nil, nil, false, err
	}
	changed = !slices.Equal(original, next) || mappingChanged
	channel.Models = strings.Join(next, ",")
	settings.KiloFreeModelGeneratedMappings = filterModelMappingsBySources(generated, next)
	settings.KiloFreeModelManagedModels = intersectModelNames(mergeModelNames(settings.KiloFreeModelManagedModels, add), next)
	settings.UpstreamModelUpdateIgnoredModels = subtractModelNames(mergeModelNames(settings.UpstreamModelUpdateIgnoredModels, ignored), add)
	remaining = subtractModelNames(pendingAdd, mergeModelNames(add, ignored))
	remainingRemove = subtractModelNames(pendingRemove, remove)
	settings.UpstreamModelUpdateLastDetectedModels = remaining
	settings.UpstreamModelUpdateLastRemovedModels = remainingRemove
	settings.KiloFreeModelPendingMappings = filterModelMappingsBySources(settings.KiloFreeModelPendingMappings, remaining)
	settings.UpstreamModelUpdateLastCheckTime = common.GetTimestamp()
	if err := updateChannelUpstreamModelSettings(channel, settings, changed); err != nil {
		return nil, nil, nil, nil, false, err
	}
	added, removed = subtractModelNames(next, original), subtractModelNames(original, next)
	if changed {
		err = channel.UpdateAbilities(nil)
	}
	return added, removed, remaining, remainingRemove, changed, err
}
