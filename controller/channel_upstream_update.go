package controller

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

const (
	channelUpstreamModelUpdateTaskDefaultIntervalMinutes  = 30
	channelUpstreamModelUpdateTaskBatchSize               = 100
	channelUpstreamModelUpdateMinCheckIntervalSeconds     = 300
	channelUpstreamModelUpdateNotifySuppressWindowSeconds = 86400
	channelUpstreamModelUpdateNotifyMaxChannelDetails     = 8
	channelUpstreamModelUpdateNotifyMaxModelDetails       = 12
	channelUpstreamModelUpdateNotifyMaxFailedChannelIDs   = 10
)

var (
	channelUpstreamModelUpdateTaskOnce    sync.Once
	channelUpstreamModelUpdateTaskRunning atomic.Bool
	channelUpstreamModelUpdateNotifyState = struct {
		sync.Mutex
		lastNotifiedAt      int64
		lastChangedChannels int
		lastFailedChannels  int
	}{}
)

type applyChannelUpstreamModelUpdatesRequest struct {
	ID           int      `json:"id"`
	AddModels    []string `json:"add_models"`
	RemoveModels []string `json:"remove_models"`
	IgnoreModels []string `json:"ignore_models"`
}

type applyAllChannelUpstreamModelUpdatesResult struct {
	ChannelID             int      `json:"channel_id"`
	ChannelName           string   `json:"channel_name"`
	AddedModels           []string `json:"added_models"`
	RemovedModels         []string `json:"removed_models"`
	RemainingModels       []string `json:"remaining_models"`
	RemainingRemoveModels []string `json:"remaining_remove_models"`
}

type detectChannelUpstreamModelUpdatesResult struct {
	ChannelID         int      `json:"channel_id"`
	ChannelName       string   `json:"channel_name"`
	AddModels         []string `json:"add_models"`
	RemoveModels      []string `json:"remove_models"`
	LastCheckTime     int64    `json:"last_check_time"`
	AutoAddedModels   int      `json:"auto_added_models"`
	AutoRemovedModels int      `json:"auto_removed_models"`
}

type channelUpstreamAutoApplyResult struct {
	AddedModels   []string
	RemovedModels []string
}

type openRouterManagedModelPlan struct {
	DesiredModels          []string
	DesiredMappings        map[string]string
	CurrentManagedMappings map[string]string
	PreservedModelMappings map[string]string
}

type upstreamModelUpdateChannelSummary struct {
	ChannelName string
	AddCount    int
	RemoveCount int
}

func normalizeModelNames(models []string) []string {
	return lo.Uniq(lo.FilterMap(models, func(model string, _ int) (string, bool) {
		trimmed := strings.TrimSpace(model)
		return trimmed, trimmed != ""
	}))
}

func isOpenRouterManagedFreeOrAlphaModel(modelName string) bool {
	modelName = strings.ToLower(strings.TrimSpace(modelName))
	if modelName == "openrouter/free" || strings.HasSuffix(modelName, ":free") {
		return true
	}
	if !strings.HasPrefix(modelName, "openrouter/") || !strings.HasSuffix(modelName, "-alpha") {
		return false
	}
	alphaName := strings.TrimSuffix(strings.TrimPrefix(modelName, "openrouter/"), "-alpha")
	return alphaName != "" && !strings.Contains(alphaName, "/")
}

func filterOpenRouterManagedFreeAndAlphaModels(models []string) []string {
	return lo.Filter(normalizeModelNames(models), func(modelName string, _ int) bool {
		return isOpenRouterManagedFreeOrAlphaModel(modelName)
	})
}

func isOpenRouterManagedModelSyncEnabled(channel *model.Channel, settings dto.ChannelOtherSettings) bool {
	return channel != nil &&
		channel.Type == constant.ChannelTypeOpenRouter &&
		settings.OpenRouterFreeAlphaSyncEnabled
}

func isChannelUpstreamModelUpdateEnabled(channel *model.Channel, settings dto.ChannelOtherSettings) bool {
	return settings.UpstreamModelUpdateCheckEnabled || isOpenRouterManagedModelSyncEnabled(channel, settings)
}

func mergeModelNames(base []string, appended []string) []string {
	merged := normalizeModelNames(base)
	seen := make(map[string]struct{}, len(merged))
	for _, model := range merged {
		seen[model] = struct{}{}
	}
	for _, model := range normalizeModelNames(appended) {
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		merged = append(merged, model)
	}
	return merged
}

func subtractModelNames(base []string, removed []string) []string {
	removeSet := make(map[string]struct{}, len(removed))
	for _, model := range normalizeModelNames(removed) {
		removeSet[model] = struct{}{}
	}
	return lo.Filter(normalizeModelNames(base), func(model string, _ int) bool {
		_, ok := removeSet[model]
		return !ok
	})
}

func intersectModelNames(base []string, allowed []string) []string {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, model := range normalizeModelNames(allowed) {
		allowedSet[model] = struct{}{}
	}
	return lo.Filter(normalizeModelNames(base), func(model string, _ int) bool {
		_, ok := allowedSet[model]
		return ok
	})
}

func applySelectedModelChanges(originModels []string, addModels []string, removeModels []string) []string {
	// Add wins when the same model appears in both selected lists.
	normalizedAdd := normalizeModelNames(addModels)
	normalizedRemove := subtractModelNames(normalizeModelNames(removeModels), normalizedAdd)
	return subtractModelNames(mergeModelNames(originModels, normalizedAdd), normalizedRemove)
}

func normalizeChannelModelMapping(channel *model.Channel) map[string]string {
	if channel == nil || channel.ModelMapping == nil {
		return nil
	}
	rawMapping := strings.TrimSpace(*channel.ModelMapping)
	if rawMapping == "" || rawMapping == "{}" {
		return nil
	}
	parsed := make(map[string]string)
	if err := common.UnmarshalJsonStr(rawMapping, &parsed); err != nil {
		return nil
	}
	normalized := make(map[string]string, len(parsed))
	for source, target := range parsed {
		normalizedSource := strings.TrimSpace(source)
		normalizedTarget := strings.TrimSpace(target)
		if normalizedSource == "" || normalizedTarget == "" {
			continue
		}
		normalized[normalizedSource] = normalizedTarget
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func cloneModelMapping(modelMapping map[string]string) map[string]string {
	cloned := make(map[string]string, len(modelMapping))
	for source, target := range modelMapping {
		cloned[source] = target
	}
	return cloned
}

func modelMappingsEqual(left map[string]string, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for source, target := range left {
		if right[source] != target {
			return false
		}
	}
	return true
}

func setChannelModelMapping(channel *model.Channel, modelMapping map[string]string) (bool, error) {
	normalized := make(map[string]string, len(modelMapping))
	for source, target := range modelMapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if source == "" || target == "" {
			continue
		}
		normalized[source] = target
	}
	if modelMappingsEqual(normalizeChannelModelMapping(channel), normalized) {
		return false, nil
	}
	mappingBytes, err := common.Marshal(normalized)
	if err != nil {
		return false, err
	}
	mappingJSON := string(mappingBytes)
	channel.ModelMapping = &mappingJSON
	return true, nil
}

func isIgnoredUpstreamModel(modelName string, ignoredModels []string) bool {
	return lo.ContainsBy(normalizeModelNames(ignoredModels), func(ignoredModel string) bool {
		if regexBody, ok := strings.CutPrefix(ignoredModel, "regex:"); ok {
			matched, err := regexp.MatchString(strings.TrimSpace(regexBody), modelName)
			return err == nil && matched
		}
		return ignoredModel == modelName
	})
}

func simplifyOpenRouterFreeModelName(modelName string) (string, bool) {
	modelName = strings.TrimSpace(modelName)
	lowerName := strings.ToLower(modelName)
	if lowerName == "openrouter/free" || !strings.HasSuffix(lowerName, ":free") {
		return "", false
	}
	slashIndex := strings.Index(modelName, "/")
	if slashIndex < 0 || slashIndex == len(modelName)-1 {
		return "", false
	}
	simplified := strings.TrimSpace(modelName[slashIndex+1 : len(modelName)-len(":free")])
	if simplified == "" {
		return "", false
	}
	return simplified, true
}

func collectManagedOpenRouterFreeModelMappings(
	modelMapping map[string]string,
	generatedMappings map[string]string,
) map[string]string {
	managed := make(map[string]string)
	for source, target := range generatedMappings {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if modelMapping[source] != target {
			continue
		}
		simplified, ok := simplifyOpenRouterFreeModelName(target)
		if ok && source == simplified {
			managed[source] = target
		}
	}
	return managed
}

func buildOpenRouterManagedModelPlan(
	channel *model.Channel,
	upstreamModels []string,
	simplifyFreeModelNames bool,
	generatedMappings map[string]string,
) openRouterManagedModelPlan {
	currentMapping := normalizeChannelModelMapping(channel)
	currentManagedMappings := collectManagedOpenRouterFreeModelMappings(currentMapping, generatedMappings)
	preservedMappings := cloneModelMapping(currentMapping)
	for source := range currentManagedMappings {
		delete(preservedMappings, source)
	}

	plan := openRouterManagedModelPlan{
		DesiredModels:          make([]string, 0, len(upstreamModels)),
		DesiredMappings:        make(map[string]string),
		CurrentManagedMappings: currentManagedMappings,
		PreservedModelMappings: preservedMappings,
	}
	if !simplifyFreeModelNames {
		plan.DesiredModels = normalizeModelNames(upstreamModels)
		return plan
	}

	aliasCounts := make(map[string]int)
	for _, upstreamModel := range normalizeModelNames(upstreamModels) {
		if alias, ok := simplifyOpenRouterFreeModelName(upstreamModel); ok {
			aliasCounts[alias]++
		}
	}
	localModelSet := make(map[string]struct{})
	for _, localModel := range normalizeModelNames(channel.GetModels()) {
		localModelSet[localModel] = struct{}{}
	}

	for _, upstreamModel := range normalizeModelNames(upstreamModels) {
		alias, canSimplify := simplifyOpenRouterFreeModelName(upstreamModel)
		if !canSimplify || aliasCounts[alias] != 1 {
			plan.DesiredModels = append(plan.DesiredModels, upstreamModel)
			continue
		}
		_, aliasAlreadyManaged := currentManagedMappings[alias]
		if _, exists := localModelSet[alias]; exists && !aliasAlreadyManaged {
			plan.DesiredModels = append(plan.DesiredModels, upstreamModel)
			continue
		}
		if _, exists := preservedMappings[alias]; exists {
			plan.DesiredModels = append(plan.DesiredModels, upstreamModel)
			continue
		}
		plan.DesiredModels = append(plan.DesiredModels, alias)
		plan.DesiredMappings[alias] = upstreamModel
	}
	plan.DesiredModels = normalizeModelNames(plan.DesiredModels)
	return plan
}

func buildAutoAppliedOpenRouterModelMapping(
	channel *model.Channel,
	currentManagedMappings map[string]string,
	desiredMappings map[string]string,
	nextModels []string,
) map[string]string {
	nextMapping := cloneModelMapping(normalizeChannelModelMapping(channel))
	for source := range currentManagedMappings {
		delete(nextMapping, source)
	}
	nextModelSet := make(map[string]struct{}, len(nextModels))
	for _, modelName := range normalizeModelNames(nextModels) {
		nextModelSet[modelName] = struct{}{}
	}
	for source, target := range desiredMappings {
		if _, exists := nextModelSet[source]; exists {
			nextMapping[source] = target
		}
	}
	return nextMapping
}

func filterModelMappingsBySources(modelMapping map[string]string, sources []string) map[string]string {
	sourceSet := make(map[string]struct{}, len(sources))
	for _, source := range normalizeModelNames(sources) {
		sourceSet[source] = struct{}{}
	}
	filtered := make(map[string]string)
	for source, target := range modelMapping {
		if _, exists := sourceSet[source]; exists {
			filtered[source] = target
		}
	}
	return filtered
}

func collectPendingUpstreamModelChangesFromModels(
	localModels []string,
	upstreamModels []string,
	ignoredModels []string,
	modelMapping map[string]string,
) (pendingAddModels []string, pendingRemoveModels []string) {
	localSet := make(map[string]struct{})
	localModels = normalizeModelNames(localModels)
	upstreamModels = normalizeModelNames(upstreamModels)
	for _, modelName := range localModels {
		localSet[modelName] = struct{}{}
	}
	upstreamSet := make(map[string]struct{}, len(upstreamModels))
	for _, modelName := range upstreamModels {
		upstreamSet[modelName] = struct{}{}
	}

	redirectSourceSet := make(map[string]struct{}, len(modelMapping))
	redirectTargetSet := make(map[string]struct{}, len(modelMapping))
	for source, target := range modelMapping {
		redirectSourceSet[source] = struct{}{}
		redirectTargetSet[target] = struct{}{}
	}

	coveredUpstreamSet := make(map[string]struct{}, len(localSet)+len(redirectTargetSet))
	for modelName := range localSet {
		coveredUpstreamSet[modelName] = struct{}{}
	}
	for modelName := range redirectTargetSet {
		coveredUpstreamSet[modelName] = struct{}{}
	}

	pendingAdd := lo.Filter(upstreamModels, func(modelName string, _ int) bool {
		if _, ok := coveredUpstreamSet[modelName]; ok {
			return false
		}
		if isIgnoredUpstreamModel(modelName, ignoredModels) {
			return false
		}
		return true
	})
	pendingRemove := lo.Filter(localModels, func(modelName string, _ int) bool {
		// Redirect source models are virtual aliases and should not be removed
		// only because they are absent from upstream model list.
		if _, ok := redirectSourceSet[modelName]; ok {
			return false
		}
		_, ok := upstreamSet[modelName]
		return !ok
	})
	return normalizeModelNames(pendingAdd), normalizeModelNames(pendingRemove)
}

func collectPendingOpenRouterManagedModelChangesFromModels(
	localModels []string,
	plan openRouterManagedModelPlan,
	ignoredModels []string,
) (pendingAddModels []string, pendingRemoveModels []string) {
	pendingAddModels, _ = collectPendingUpstreamModelChangesFromModels(
		localModels,
		plan.DesiredModels,
		ignoredModels,
		plan.PreservedModelMappings,
	)
	for source, target := range plan.DesiredMappings {
		if isIgnoredUpstreamModel(source, ignoredModels) || isIgnoredUpstreamModel(target, ignoredModels) {
			pendingAddModels = subtractModelNames(pendingAddModels, []string{source})
			continue
		}
		if plan.CurrentManagedMappings[source] != target {
			pendingAddModels = mergeModelNames(pendingAddModels, []string{source})
		}
	}

	desiredModelSet := make(map[string]struct{}, len(plan.DesiredModels))
	for _, modelName := range normalizeModelNames(plan.DesiredModels) {
		desiredModelSet[modelName] = struct{}{}
	}
	preservedRedirectSourceSet := make(map[string]struct{}, len(plan.PreservedModelMappings))
	for source := range plan.PreservedModelMappings {
		preservedRedirectSourceSet[source] = struct{}{}
	}
	pendingRemoveModels = lo.Filter(normalizeModelNames(localModels), func(modelName string, _ int) bool {
		_, isManagedAlias := plan.CurrentManagedMappings[modelName]
		if !isOpenRouterManagedFreeOrAlphaModel(modelName) && !isManagedAlias {
			return false
		}
		if _, ok := preservedRedirectSourceSet[modelName]; ok {
			return false
		}
		_, exists := desiredModelSet[modelName]
		return !exists
	})
	return normalizeModelNames(pendingAddModels), normalizeModelNames(pendingRemoveModels)
}

func collectPendingUpstreamModelChanges(channel *model.Channel, settings dto.ChannelOtherSettings) (pendingAddModels []string, pendingRemoveModels []string, desiredMappings map[string]string, err error) {
	upstreamModels, err := fetchChannelUpstreamModelIDs(channel)
	if err != nil {
		return nil, nil, nil, err
	}
	if isOpenRouterManagedModelSyncEnabled(channel, settings) {
		plan := buildOpenRouterManagedModelPlan(
			channel,
			upstreamModels,
			settings.OpenRouterFreeModelNameSimplificationEnabled,
			settings.OpenRouterFreeModelGeneratedMappings,
		)
		pendingAddModels, pendingRemoveModels = collectPendingOpenRouterManagedModelChangesFromModels(
			channel.GetModels(),
			plan,
			settings.UpstreamModelUpdateIgnoredModels,
		)
		return pendingAddModels, pendingRemoveModels, plan.DesiredMappings, nil
	}
	pendingAddModels, pendingRemoveModels = collectPendingUpstreamModelChangesFromModels(
		channel.GetModels(),
		upstreamModels,
		settings.UpstreamModelUpdateIgnoredModels,
		normalizeChannelModelMapping(channel),
	)
	return pendingAddModels, pendingRemoveModels, nil, nil
}

func getUpstreamModelUpdateMinCheckIntervalSeconds() int64 {
	interval := int64(common.GetEnvOrDefault(
		"CHANNEL_UPSTREAM_MODEL_UPDATE_MIN_CHECK_INTERVAL_SECONDS",
		channelUpstreamModelUpdateMinCheckIntervalSeconds,
	))
	if interval < 0 {
		return channelUpstreamModelUpdateMinCheckIntervalSeconds
	}
	return interval
}

func fetchOpenRouterManagedFreeAndAlphaModelIDs(
	channel *model.Channel,
	baseURL string,
	key string,
	customModelListURL string,
) ([]string, error) {
	fetchURL := strings.TrimSpace(customModelListURL)
	if fetchURL == "" {
		fetchURL = fmt.Sprintf("%s/v1/models/user", strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	}
	models, err := fetchOpenAICompatibleModelIDs(channel, fetchURL, key)
	if err != nil {
		return nil, fmt.Errorf("获取 OpenRouter 用户模型列表失败: %w", err)
	}
	managedModels := filterOpenRouterManagedFreeAndAlphaModels(models)
	if len(managedModels) == 0 {
		return nil, fmt.Errorf("OpenRouter 用户模型列表未返回任何免费或匿名 Alpha 模型")
	}
	return managedModels, nil
}

func fetchChannelUpstreamModelIDs(channel *model.Channel) ([]string, error) {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	if channel.Type == constant.ChannelTypeOllama {
		key := strings.TrimSpace(strings.Split(channel.Key, "\n")[0])
		return fetchChannelModelIDsWithKey(channel, baseURL, key, channel.GetOtherSettings().CustomModelListURL)
	}

	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
	}
	key = strings.TrimSpace(key)
	settings := channel.GetOtherSettings()
	if isOpenRouterManagedModelSyncEnabled(channel, settings) {
		return fetchOpenRouterManagedFreeAndAlphaModelIDs(
			channel,
			baseURL,
			key,
			settings.CustomModelListURL,
		)
	}
	return fetchChannelModelIDsWithKey(channel, baseURL, key, settings.CustomModelListURL)
}

func updateChannelUpstreamModelSettings(channel *model.Channel, settings dto.ChannelOtherSettings, updateModels bool) error {
	channel.SetOtherSettings(settings)
	updates := map[string]interface{}{
		"settings": channel.OtherSettings,
	}
	if updateModels {
		updates["models"] = channel.Models
		if channel.ModelMapping != nil {
			updates["model_mapping"] = *channel.ModelMapping
		}
	}
	return model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}

func checkAndPersistChannelUpstreamModelUpdates(
	channel *model.Channel,
	settings *dto.ChannelOtherSettings,
	force bool,
	allowAutoApply bool,
) (modelsChanged bool, autoApplyResult channelUpstreamAutoApplyResult, err error) {
	now := common.GetTimestamp()
	if !force {
		minInterval := getUpstreamModelUpdateMinCheckIntervalSeconds()
		if settings.UpstreamModelUpdateLastCheckTime > 0 &&
			now-settings.UpstreamModelUpdateLastCheckTime < minInterval {
			return false, channelUpstreamAutoApplyResult{}, nil
		}
	}

	pendingAddModels, pendingRemoveModels, desiredMappings, fetchErr := collectPendingUpstreamModelChanges(channel, *settings)
	settings.UpstreamModelUpdateLastCheckTime = now
	if fetchErr != nil {
		if err = updateChannelUpstreamModelSettings(channel, *settings, false); err != nil {
			return false, channelUpstreamAutoApplyResult{}, err
		}
		return false, channelUpstreamAutoApplyResult{}, fetchErr
	}
	settings.OpenRouterFreeModelPendingMappings = filterModelMappingsBySources(desiredMappings, pendingAddModels)

	if allowAutoApply && isOpenRouterManagedModelSyncEnabled(channel, *settings) {
		originModels := normalizeModelNames(channel.GetModels())
		nextModels := applySelectedModelChanges(originModels, pendingAddModels, pendingRemoveModels)
		modelListChanged := !slices.Equal(originModels, nextModels)
		if modelListChanged {
			channel.Models = strings.Join(nextModels, ",")
			autoApplyResult.AddedModels = subtractModelNames(nextModels, originModels)
			autoApplyResult.RemovedModels = subtractModelNames(originModels, nextModels)
		}
		currentManagedMappings := collectManagedOpenRouterFreeModelMappings(
			normalizeChannelModelMapping(channel),
			settings.OpenRouterFreeModelGeneratedMappings,
		)
		nextMapping := buildAutoAppliedOpenRouterModelMapping(
			channel,
			currentManagedMappings,
			desiredMappings,
			nextModels,
		)
		mappingChanged, mappingErr := setChannelModelMapping(channel, nextMapping)
		if mappingErr != nil {
			return false, autoApplyResult, mappingErr
		}
		modelsChanged = modelListChanged || mappingChanged
		settings.OpenRouterFreeModelGeneratedMappings = filterModelMappingsBySources(desiredMappings, nextModels)
		settings.OpenRouterFreeModelPendingMappings = nil
		settings.UpstreamModelUpdateLastDetectedModels = []string{}
		settings.UpstreamModelUpdateLastRemovedModels = []string{}
	} else if allowAutoApply && settings.UpstreamModelUpdateAutoSyncEnabled && len(pendingAddModels) > 0 {
		originModels := normalizeModelNames(channel.GetModels())
		mergedModels := mergeModelNames(originModels, pendingAddModels)
		if len(mergedModels) > len(originModels) {
			channel.Models = strings.Join(mergedModels, ",")
			autoApplyResult.AddedModels = subtractModelNames(mergedModels, originModels)
			modelsChanged = true
		}
		settings.UpstreamModelUpdateLastDetectedModels = []string{}
		settings.UpstreamModelUpdateLastRemovedModels = pendingRemoveModels
	} else {
		settings.UpstreamModelUpdateLastDetectedModels = pendingAddModels
		settings.UpstreamModelUpdateLastRemovedModels = pendingRemoveModels
	}

	if err = updateChannelUpstreamModelSettings(channel, *settings, modelsChanged); err != nil {
		return false, autoApplyResult, err
	}
	if modelsChanged {
		if err = channel.UpdateAbilities(nil); err != nil {
			return true, autoApplyResult, err
		}
	}
	return modelsChanged, autoApplyResult, nil
}

func refreshChannelRuntimeCache() {
	if common.MemoryCacheEnabled {
		func() {
			defer func() {
				if r := recover(); r != nil {
					common.SysLog(fmt.Sprintf("InitChannelCache panic: %v", r))
				}
			}()
			model.InitChannelCache()
		}()
	}
	service.ResetProxyClientCache()
}

func shouldSendUpstreamModelUpdateNotification(now int64, changedChannels int, failedChannels int) bool {
	if changedChannels <= 0 && failedChannels <= 0 {
		return true
	}

	channelUpstreamModelUpdateNotifyState.Lock()
	defer channelUpstreamModelUpdateNotifyState.Unlock()

	if channelUpstreamModelUpdateNotifyState.lastNotifiedAt > 0 &&
		now-channelUpstreamModelUpdateNotifyState.lastNotifiedAt < channelUpstreamModelUpdateNotifySuppressWindowSeconds &&
		channelUpstreamModelUpdateNotifyState.lastChangedChannels == changedChannels &&
		channelUpstreamModelUpdateNotifyState.lastFailedChannels == failedChannels {
		return false
	}

	channelUpstreamModelUpdateNotifyState.lastNotifiedAt = now
	channelUpstreamModelUpdateNotifyState.lastChangedChannels = changedChannels
	channelUpstreamModelUpdateNotifyState.lastFailedChannels = failedChannels
	return true
}

func buildUpstreamModelUpdateTaskNotificationContent(
	checkedChannels int,
	changedChannels int,
	detectedAddModels int,
	detectedRemoveModels int,
	autoAddedModels int,
	autoRemovedModels int,
	failedChannelIDs []int,
	channelSummaries []upstreamModelUpdateChannelSummary,
	addModelSamples []string,
	removeModelSamples []string,
) string {
	var builder strings.Builder
	failedChannels := len(failedChannelIDs)
	builder.WriteString(fmt.Sprintf(
		"上游模型巡检摘要：检测渠道 %d 个，发现变更 %d 个，新增 %d 个，删除 %d 个，自动同步新增 %d 个、删除 %d 个，失败 %d 个。",
		checkedChannels,
		changedChannels,
		detectedAddModels,
		detectedRemoveModels,
		autoAddedModels,
		autoRemovedModels,
		failedChannels,
	))

	if len(channelSummaries) > 0 {
		displayCount := min(len(channelSummaries), channelUpstreamModelUpdateNotifyMaxChannelDetails)
		builder.WriteString(fmt.Sprintf("\n\n变更渠道明细（展示 %d/%d）：", displayCount, len(channelSummaries)))
		for _, summary := range channelSummaries[:displayCount] {
			builder.WriteString(fmt.Sprintf("\n- %s (+%d / -%d)", summary.ChannelName, summary.AddCount, summary.RemoveCount))
		}
		if len(channelSummaries) > displayCount {
			builder.WriteString(fmt.Sprintf("\n- 其余 %d 个渠道已省略", len(channelSummaries)-displayCount))
		}
	}

	normalizedAddModelSamples := normalizeModelNames(addModelSamples)
	if len(normalizedAddModelSamples) > 0 {
		displayCount := min(len(normalizedAddModelSamples), channelUpstreamModelUpdateNotifyMaxModelDetails)
		builder.WriteString(fmt.Sprintf("\n\n新增模型示例（展示 %d/%d）：%s",
			displayCount,
			len(normalizedAddModelSamples),
			strings.Join(normalizedAddModelSamples[:displayCount], ", "),
		))
		if len(normalizedAddModelSamples) > displayCount {
			builder.WriteString(fmt.Sprintf("（其余 %d 个已省略）", len(normalizedAddModelSamples)-displayCount))
		}
	}

	normalizedRemoveModelSamples := normalizeModelNames(removeModelSamples)
	if len(normalizedRemoveModelSamples) > 0 {
		displayCount := min(len(normalizedRemoveModelSamples), channelUpstreamModelUpdateNotifyMaxModelDetails)
		builder.WriteString(fmt.Sprintf("\n\n删除模型示例（展示 %d/%d）：%s",
			displayCount,
			len(normalizedRemoveModelSamples),
			strings.Join(normalizedRemoveModelSamples[:displayCount], ", "),
		))
		if len(normalizedRemoveModelSamples) > displayCount {
			builder.WriteString(fmt.Sprintf("（其余 %d 个已省略）", len(normalizedRemoveModelSamples)-displayCount))
		}
	}

	if failedChannels > 0 {
		displayCount := min(failedChannels, channelUpstreamModelUpdateNotifyMaxFailedChannelIDs)
		displayIDs := lo.Map(failedChannelIDs[:displayCount], func(channelID int, _ int) string {
			return fmt.Sprintf("%d", channelID)
		})
		builder.WriteString(fmt.Sprintf(
			"\n\n失败渠道 ID（展示 %d/%d）：%s",
			displayCount,
			failedChannels,
			strings.Join(displayIDs, ", "),
		))
		if failedChannels > displayCount {
			builder.WriteString(fmt.Sprintf("（其余 %d 个已省略）", failedChannels-displayCount))
		}
	}
	return builder.String()
}

func runChannelUpstreamModelUpdateTaskOnce() {
	if !channelUpstreamModelUpdateTaskRunning.CompareAndSwap(false, true) {
		return
	}
	defer channelUpstreamModelUpdateTaskRunning.Store(false)

	checkedChannels := 0
	failedChannels := 0
	failedChannelIDs := make([]int, 0)
	changedChannels := 0
	detectedAddModels := 0
	detectedRemoveModels := 0
	autoAddedModels := 0
	autoRemovedModels := 0
	channelSummaries := make([]upstreamModelUpdateChannelSummary, 0)
	addModelSamples := make([]string, 0)
	removeModelSamples := make([]string, 0)
	refreshNeeded := false

	lastID := 0
	for {
		var channels []*model.Channel
		query := model.DB.
			Select("id", "name", "type", "key", "status", "base_url", "models", "model_mapping", "settings", "setting", "other", "group", "priority", "weight", "tag", "channel_info", "header_override").
			Where("status = ?", common.ChannelStatusEnabled).
			Order("id asc").
			Limit(channelUpstreamModelUpdateTaskBatchSize)
		if lastID > 0 {
			query = query.Where("id > ?", lastID)
		}
		err := query.Find(&channels).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("upstream model update task query failed: %v", err))
			break
		}
		if len(channels) == 0 {
			break
		}
		lastID = channels[len(channels)-1].Id

		for _, channel := range channels {
			if channel == nil {
				continue
			}

			settings := channel.GetOtherSettings()
			if !isChannelUpstreamModelUpdateEnabled(channel, settings) {
				continue
			}

			checkedChannels++
			modelsChanged, autoApplyResult, err := checkAndPersistChannelUpstreamModelUpdates(channel, &settings, false, true)
			if err != nil {
				failedChannels++
				failedChannelIDs = append(failedChannelIDs, channel.Id)
				common.SysLog(fmt.Sprintf("upstream model update check failed: channel_id=%d channel_name=%s err=%v", channel.Id, channel.Name, err))
				continue
			}
			currentAddModels := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
			currentRemoveModels := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
			currentAddModels = mergeModelNames(currentAddModels, autoApplyResult.AddedModels)
			currentRemoveModels = mergeModelNames(currentRemoveModels, autoApplyResult.RemovedModels)
			currentAddCount := len(currentAddModels)
			currentRemoveCount := len(currentRemoveModels)
			detectedAddModels += currentAddCount
			detectedRemoveModels += currentRemoveCount
			if currentAddCount > 0 || currentRemoveCount > 0 {
				changedChannels++
				channelSummaries = append(channelSummaries, upstreamModelUpdateChannelSummary{
					ChannelName: channel.Name,
					AddCount:    currentAddCount,
					RemoveCount: currentRemoveCount,
				})
			}
			addModelSamples = mergeModelNames(addModelSamples, currentAddModels)
			removeModelSamples = mergeModelNames(removeModelSamples, currentRemoveModels)
			if modelsChanged {
				refreshNeeded = true
			}
			autoAddedModels += len(autoApplyResult.AddedModels)
			autoRemovedModels += len(autoApplyResult.RemovedModels)

			if common.RequestInterval > 0 {
				time.Sleep(common.RequestInterval)
			}
		}

		if len(channels) < channelUpstreamModelUpdateTaskBatchSize {
			break
		}
	}

	if refreshNeeded {
		refreshChannelRuntimeCache()
	}

	if checkedChannels > 0 || common.DebugEnabled {
		common.SysLog(fmt.Sprintf(
			"upstream model update task done: checked_channels=%d changed_channels=%d detected_add_models=%d detected_remove_models=%d failed_channels=%d auto_added_models=%d auto_removed_models=%d",
			checkedChannels,
			changedChannels,
			detectedAddModels,
			detectedRemoveModels,
			failedChannels,
			autoAddedModels,
			autoRemovedModels,
		))
	}
	if changedChannels > 0 || failedChannels > 0 {
		now := common.GetTimestamp()
		if !shouldSendUpstreamModelUpdateNotification(now, changedChannels, failedChannels) {
			common.SysLog(fmt.Sprintf(
				"upstream model update notification skipped in 24h window: changed_channels=%d failed_channels=%d",
				changedChannels,
				failedChannels,
			))
			return
		}
		service.NotifyUpstreamModelUpdateWatchers(
			"上游模型巡检通知",
			buildUpstreamModelUpdateTaskNotificationContent(
				checkedChannels,
				changedChannels,
				detectedAddModels,
				detectedRemoveModels,
				autoAddedModels,
				autoRemovedModels,
				failedChannelIDs,
				channelSummaries,
				addModelSamples,
				removeModelSamples,
			),
		)
	}
}

func StartChannelUpstreamModelUpdateTask() {
	channelUpstreamModelUpdateTaskOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		if !common.GetEnvOrDefaultBool("CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED", true) {
			common.SysLog("upstream model update task disabled by CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED")
			return
		}

		intervalMinutes := common.GetEnvOrDefault(
			"CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_INTERVAL_MINUTES",
			channelUpstreamModelUpdateTaskDefaultIntervalMinutes,
		)
		if intervalMinutes < 1 {
			intervalMinutes = channelUpstreamModelUpdateTaskDefaultIntervalMinutes
		}
		interval := time.Duration(intervalMinutes) * time.Minute

		go func() {
			common.SysLog(fmt.Sprintf("upstream model update task started: interval=%s", interval))
			runChannelUpstreamModelUpdateTaskOnce()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				runChannelUpstreamModelUpdateTaskOnce()
			}
		}()
	})
}

func ApplyChannelUpstreamModelUpdates(c *gin.Context) {
	var req applyChannelUpstreamModelUpdatesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ID <= 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "invalid channel id",
		})
		return
	}

	channel, err := model.GetChannelById(req.ID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	beforeSettings := channel.GetOtherSettings()
	ignoredModels := intersectModelNames(req.IgnoreModels, beforeSettings.UpstreamModelUpdateLastDetectedModels)

	addedModels, removedModels, remainingModels, remainingRemoveModels, modelsChanged, err := applyChannelUpstreamModelUpdates(
		channel,
		req.AddModels,
		req.IgnoreModels,
		req.RemoveModels,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if modelsChanged {
		refreshChannelRuntimeCache()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"id":                      channel.Id,
			"added_models":            addedModels,
			"removed_models":          removedModels,
			"ignored_models":          ignoredModels,
			"remaining_models":        remainingModels,
			"remaining_remove_models": remainingRemoveModels,
			"models":                  channel.Models,
			"settings":                channel.OtherSettings,
		},
	})
}

func DetectChannelUpstreamModelUpdates(c *gin.Context) {
	var req applyChannelUpstreamModelUpdatesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.ID <= 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "invalid channel id",
		})
		return
	}

	channel, err := model.GetChannelById(req.ID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	settings := channel.GetOtherSettings()
	modelsChanged, autoApplyResult, err := checkAndPersistChannelUpstreamModelUpdates(channel, &settings, true, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if modelsChanged {
		refreshChannelRuntimeCache()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": detectChannelUpstreamModelUpdatesResult{
			ChannelID:         channel.Id,
			ChannelName:       channel.Name,
			AddModels:         normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels),
			RemoveModels:      normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels),
			LastCheckTime:     settings.UpstreamModelUpdateLastCheckTime,
			AutoAddedModels:   len(autoApplyResult.AddedModels),
			AutoRemovedModels: len(autoApplyResult.RemovedModels),
		},
	})
}

func applyChannelUpstreamModelUpdates(
	channel *model.Channel,
	addModelsInput []string,
	ignoreModelsInput []string,
	removeModelsInput []string,
) (
	addedModels []string,
	removedModels []string,
	remainingModels []string,
	remainingRemoveModels []string,
	modelsChanged bool,
	err error,
) {
	settings := channel.GetOtherSettings()
	pendingAddModels := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
	pendingRemoveModels := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
	isOpenRouterManagedSync := isOpenRouterManagedModelSyncEnabled(channel, settings)
	currentManagedMappings := collectManagedOpenRouterFreeModelMappings(
		normalizeChannelModelMapping(channel),
		settings.OpenRouterFreeModelGeneratedMappings,
	)
	if isOpenRouterManagedSync {
		pendingAddModels = lo.Filter(pendingAddModels, func(modelName string, _ int) bool {
			_, isPendingAlias := settings.OpenRouterFreeModelPendingMappings[modelName]
			return isOpenRouterManagedFreeOrAlphaModel(modelName) || isPendingAlias
		})
		pendingRemoveModels = lo.Filter(pendingRemoveModels, func(modelName string, _ int) bool {
			_, isManagedAlias := currentManagedMappings[modelName]
			return isOpenRouterManagedFreeOrAlphaModel(modelName) || isManagedAlias
		})
	}
	addModels := intersectModelNames(addModelsInput, pendingAddModels)
	ignoreModels := intersectModelNames(ignoreModelsInput, pendingAddModels)
	removeModels := intersectModelNames(removeModelsInput, pendingRemoveModels)
	removeModels = subtractModelNames(removeModels, addModels)

	originModels := normalizeModelNames(channel.GetModels())
	nextModels := applySelectedModelChanges(originModels, addModels, removeModels)
	modelListChanged := !slices.Equal(originModels, nextModels)
	if modelListChanged {
		channel.Models = strings.Join(nextModels, ",")
	}
	mappingChanged := false
	if isOpenRouterManagedSync {
		nextMapping := cloneModelMapping(normalizeChannelModelMapping(channel))
		nextGeneratedMappings := cloneModelMapping(currentManagedMappings)
		for _, removedModel := range removeModels {
			if _, isManagedAlias := currentManagedMappings[removedModel]; isManagedAlias {
				delete(nextMapping, removedModel)
				delete(nextGeneratedMappings, removedModel)
			}
		}
		for _, addedModel := range addModels {
			if target := settings.OpenRouterFreeModelPendingMappings[addedModel]; target != "" {
				nextMapping[addedModel] = target
				nextGeneratedMappings[addedModel] = target
			}
		}
		var mappingErr error
		mappingChanged, mappingErr = setChannelModelMapping(channel, nextMapping)
		if mappingErr != nil {
			return nil, nil, nil, nil, false, mappingErr
		}
		settings.OpenRouterFreeModelGeneratedMappings = nextGeneratedMappings
	}
	modelsChanged = modelListChanged || mappingChanged

	settings.UpstreamModelUpdateIgnoredModels = mergeModelNames(settings.UpstreamModelUpdateIgnoredModels, ignoreModels)
	if len(addModels) > 0 {
		settings.UpstreamModelUpdateIgnoredModels = subtractModelNames(settings.UpstreamModelUpdateIgnoredModels, addModels)
	}
	remainingModels = subtractModelNames(pendingAddModels, append(addModels, ignoreModels...))
	remainingRemoveModels = subtractModelNames(pendingRemoveModels, removeModels)
	settings.UpstreamModelUpdateLastDetectedModels = remainingModels
	settings.UpstreamModelUpdateLastRemovedModels = remainingRemoveModels
	settings.OpenRouterFreeModelPendingMappings = filterModelMappingsBySources(
		settings.OpenRouterFreeModelPendingMappings,
		remainingModels,
	)
	settings.UpstreamModelUpdateLastCheckTime = common.GetTimestamp()

	if err := updateChannelUpstreamModelSettings(channel, settings, modelsChanged); err != nil {
		return nil, nil, nil, nil, false, err
	}

	if modelsChanged {
		if err := channel.UpdateAbilities(nil); err != nil {
			return addModels, removeModels, remainingModels, remainingRemoveModels, true, err
		}
	}
	return addModels, removeModels, remainingModels, remainingRemoveModels, modelsChanged, nil
}

func collectPendingApplyUpstreamModelChanges(channel *model.Channel, settings dto.ChannelOtherSettings) (pendingAddModels []string, pendingRemoveModels []string) {
	pendingAddModels = normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
	pendingRemoveModels = normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
	if isOpenRouterManagedModelSyncEnabled(channel, settings) {
		currentManagedMappings := collectManagedOpenRouterFreeModelMappings(
			normalizeChannelModelMapping(channel),
			settings.OpenRouterFreeModelGeneratedMappings,
		)
		pendingAddModels = lo.Filter(pendingAddModels, func(modelName string, _ int) bool {
			_, isPendingAlias := settings.OpenRouterFreeModelPendingMappings[modelName]
			return isOpenRouterManagedFreeOrAlphaModel(modelName) || isPendingAlias
		})
		pendingRemoveModels = lo.Filter(pendingRemoveModels, func(modelName string, _ int) bool {
			_, isManagedAlias := currentManagedMappings[modelName]
			return isOpenRouterManagedFreeOrAlphaModel(modelName) || isManagedAlias
		})
	}
	return pendingAddModels, pendingRemoveModels
}

func findEnabledChannelsAfterID(lastID int, batchSize int) ([]*model.Channel, error) {
	var channels []*model.Channel
	query := model.DB.
		Select("id", "name", "type", "key", "status", "base_url", "models", "model_mapping", "settings", "setting", "other", "group", "priority", "weight", "tag", "channel_info", "header_override").
		Where("status = ?", common.ChannelStatusEnabled).
		Order("id asc").
		Limit(batchSize)
	if lastID > 0 {
		query = query.Where("id > ?", lastID)
	}
	return channels, query.Find(&channels).Error
}

func ApplyAllChannelUpstreamModelUpdates(c *gin.Context) {
	results := make([]applyAllChannelUpstreamModelUpdatesResult, 0)
	failed := make([]int, 0)
	refreshNeeded := false
	addedModelCount := 0
	removedModelCount := 0

	lastID := 0
	for {
		channels, err := findEnabledChannelsAfterID(lastID, channelUpstreamModelUpdateTaskBatchSize)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if len(channels) == 0 {
			break
		}
		lastID = channels[len(channels)-1].Id

		for _, channel := range channels {
			if channel == nil {
				continue
			}

			settings := channel.GetOtherSettings()
			if !isChannelUpstreamModelUpdateEnabled(channel, settings) {
				continue
			}

			pendingAddModels, pendingRemoveModels := collectPendingApplyUpstreamModelChanges(channel, settings)
			if len(pendingAddModels) == 0 && len(pendingRemoveModels) == 0 {
				continue
			}

			addedModels, removedModels, remainingModels, remainingRemoveModels, modelsChanged, err := applyChannelUpstreamModelUpdates(
				channel,
				pendingAddModels,
				nil,
				pendingRemoveModels,
			)
			if err != nil {
				failed = append(failed, channel.Id)
				continue
			}
			if modelsChanged {
				refreshNeeded = true
			}
			addedModelCount += len(addedModels)
			removedModelCount += len(removedModels)
			results = append(results, applyAllChannelUpstreamModelUpdatesResult{
				ChannelID:             channel.Id,
				ChannelName:           channel.Name,
				AddedModels:           addedModels,
				RemovedModels:         removedModels,
				RemainingModels:       remainingModels,
				RemainingRemoveModels: remainingRemoveModels,
			})
		}

		if len(channels) < channelUpstreamModelUpdateTaskBatchSize {
			break
		}
	}

	if refreshNeeded {
		refreshChannelRuntimeCache()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"processed_channels": len(results),
			"added_models":       addedModelCount,
			"removed_models":     removedModelCount,
			"failed_channel_ids": failed,
			"results":            results,
		},
	})
}

func DetectAllChannelUpstreamModelUpdates(c *gin.Context) {
	results := make([]detectChannelUpstreamModelUpdatesResult, 0)
	failed := make([]int, 0)
	detectedAddCount := 0
	detectedRemoveCount := 0
	refreshNeeded := false

	lastID := 0
	for {
		channels, err := findEnabledChannelsAfterID(lastID, channelUpstreamModelUpdateTaskBatchSize)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if len(channels) == 0 {
			break
		}
		lastID = channels[len(channels)-1].Id

		for _, channel := range channels {
			if channel == nil {
				continue
			}
			settings := channel.GetOtherSettings()
			if !isChannelUpstreamModelUpdateEnabled(channel, settings) {
				continue
			}

			modelsChanged, autoApplyResult, err := checkAndPersistChannelUpstreamModelUpdates(channel, &settings, true, false)
			if err != nil {
				failed = append(failed, channel.Id)
				continue
			}
			if modelsChanged {
				refreshNeeded = true
			}

			addModels := normalizeModelNames(settings.UpstreamModelUpdateLastDetectedModels)
			removeModels := normalizeModelNames(settings.UpstreamModelUpdateLastRemovedModels)
			detectedAddCount += len(addModels)
			detectedRemoveCount += len(removeModels)
			results = append(results, detectChannelUpstreamModelUpdatesResult{
				ChannelID:         channel.Id,
				ChannelName:       channel.Name,
				AddModels:         addModels,
				RemoveModels:      removeModels,
				LastCheckTime:     settings.UpstreamModelUpdateLastCheckTime,
				AutoAddedModels:   len(autoApplyResult.AddedModels),
				AutoRemovedModels: len(autoApplyResult.RemovedModels),
			})
		}

		if len(channels) < channelUpstreamModelUpdateTaskBatchSize {
			break
		}
	}

	if refreshNeeded {
		refreshChannelRuntimeCache()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"processed_channels":       len(results),
			"failed_channel_ids":       failed,
			"detected_add_models":      detectedAddCount,
			"detected_remove_models":   detectedRemoveCount,
			"channel_detected_results": results,
		},
	})
}
