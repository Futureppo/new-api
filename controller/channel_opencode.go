package controller

import (
	"fmt"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

func isOpenCodeManagedModelSyncEnabled(channel *model.Channel, settings dto.ChannelOtherSettings) bool {
	return channel != nil && channel.Type == constant.ChannelTypeOpenCode && settings.OpenCodeFreeModelSyncEnabled
}

func filterOpenCodeFreeChatModels(models []string) []string {
	filtered := make([]string, 0, len(models))
	for _, name := range normalizeModelNames(models) {
		if constant.IsOpenCodeFreeChatModel(name) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func fetchOpenCodeFreeModelIDs(channel *model.Channel, fetchURL, key string) ([]string, error) {
	models, err := fetchOpenAICompatibleModelIDs(channel, fetchURL, key)
	if err != nil {
		return nil, err
	}
	models = filterOpenCodeFreeChatModels(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("OpenCode 模型列表未返回任何可用的免费对话模型")
	}
	return models, nil
}

// Re-check manual mappings when applying a saved detection snapshot: an
// administrator may have added a mapping after the upstream check.
func filterOpenCodePendingModelChanges(channel *model.Channel, add, remove []string) ([]string, []string) {
	add = filterOpenCodeFreeChatModels(add)
	remove = filterOpenCodeFreeChatModels(remove)
	for source := range normalizeChannelModelMapping(channel) {
		remove = subtractModelNames(remove, []string{source})
	}
	return add, remove
}
