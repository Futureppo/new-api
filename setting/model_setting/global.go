package model_setting

import (
	"slices"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/setting/config"
)

type ChatCompletionsToResponsesPolicy struct {
	Enabled       bool     `json:"enabled"`
	AllChannels   bool     `json:"all_channels"`
	ChannelIDs    []int    `json:"channel_ids,omitempty"`
	ChannelTypes  []int    `json:"channel_types,omitempty"`
	ModelPatterns []string `json:"model_patterns,omitempty"`
}

func (p ChatCompletionsToResponsesPolicy) IsChannelEnabled(channelID int, channelType int) bool {
	if !p.Enabled {
		return false
	}
	if p.AllChannels {
		return true
	}

	if channelID > 0 && len(p.ChannelIDs) > 0 && slices.Contains(p.ChannelIDs, channelID) {
		return true
	}
	if channelType > 0 && len(p.ChannelTypes) > 0 && slices.Contains(p.ChannelTypes, channelType) {
		return true
	}
	return false
}

type GlobalSettings struct {
	PassThroughRequestEnabled        bool                             `json:"pass_through_request_enabled"`
	IRConversionEnabled              bool                             `json:"ir_conversion_enabled"`
	ThinkingModelBlacklist           []string                         `json:"thinking_model_blacklist"`
	ChatCompletionsToResponsesPolicy ChatCompletionsToResponsesPolicy `json:"chat_completions_to_responses_policy"`
}

// globalSettingsMu protects concurrent access to globalSettings while keeping GlobalSettings
// a value-copyable struct (preserving compatibility with callers/tests that snapshot *GetGlobalSettings()).
var globalSettingsMu sync.RWMutex

func (*GlobalSettings) ConfigRLock()   { globalSettingsMu.RLock() }
func (*GlobalSettings) ConfigRUnlock() { globalSettingsMu.RUnlock() }
func (*GlobalSettings) ConfigLock()    { globalSettingsMu.Lock() }
func (*GlobalSettings) ConfigUnlock()  { globalSettingsMu.Unlock() }

// 全局实例
var globalSettings = GlobalSettings{
	PassThroughRequestEnabled: false,
	IRConversionEnabled:       true,
	ThinkingModelBlacklist: []string{
		"moonshotai/kimi-k2-thinking",
		"kimi-k2-thinking",
	},
	ChatCompletionsToResponsesPolicy: ChatCompletionsToResponsesPolicy{
		Enabled:     false,
		AllChannels: true,
	},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("global", &globalSettings)
}

func GetGlobalSettings() *GlobalSettings {
	return &globalSettings
}

// IsIRConversionEnabled 判断是否启用 IR 跨协议转换引擎
func IsIRConversionEnabled() bool {
	globalSettingsMu.RLock()
	enabled := globalSettings.IRConversionEnabled
	globalSettingsMu.RUnlock()
	return enabled
}

// SetIRConversionEnabled 并发安全地切换 IR 跨协议转换引擎开关
func SetIRConversionEnabled(enabled bool) {
	globalSettingsMu.Lock()
	globalSettings.IRConversionEnabled = enabled
	globalSettingsMu.Unlock()
}

// ShouldPreserveThinkingSuffix 判断模型是否配置为保留 thinking/-nothinking/-low/-high/-medium 后缀
func ShouldPreserveThinkingSuffix(modelName string) bool {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return false
	}

	globalSettingsMu.RLock()
	defer globalSettingsMu.RUnlock()
	for _, entry := range globalSettings.ThinkingModelBlacklist {
		if strings.TrimSpace(entry) == target {
			return true
		}
	}
	return false
}
