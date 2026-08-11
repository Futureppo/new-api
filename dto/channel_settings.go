package dto

import "fmt"

type ChannelRPMProtectionSettings struct {
	Enabled                    bool `json:"enabled"`
	RPMLimit                   int  `json:"rpm_limit"`
	ProtectionThresholdPercent int  `json:"protection_threshold_percent"`
	RampMinutes                int  `json:"ramp_minutes"`
}

func (s *ChannelRPMProtectionSettings) Validate() error {
	if s == nil {
		return nil
	}
	if s.RPMLimit < 0 {
		return fmt.Errorf("RPM 上限不能小于 0")
	}
	if s.ProtectionThresholdPercent < 1 || s.ProtectionThresholdPercent > 100 {
		return fmt.Errorf("RPM 保护阈值必须在 1 到 100 之间")
	}
	if s.RampMinutes <= 0 {
		return fmt.Errorf("RPM 爬坡时间必须大于 0")
	}
	return nil
}

type ChannelSettings struct {
	ForceFormat            bool                          `json:"force_format,omitempty"`
	ThinkingToContent      bool                          `json:"thinking_to_content,omitempty"`
	Proxy                  string                        `json:"proxy"`
	PassThroughBodyEnabled bool                          `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string                        `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool                          `json:"system_prompt_override,omitempty"`
	RPMProtection          *ChannelRPMProtectionSettings `json:"rpm_protection,omitempty"`
}

func (s ChannelSettings) Validate() error {
	return s.RPMProtection.Validate()
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool          `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	RemoveGifImagesEnabled                *bool         `json:"remove_gif_images_enabled,omitempty"` // 是否移除 Gemini 不支持的 image/gif 输入（未配置时默认开启）
	XAICodexCompatibilityEnabled          bool          `json:"xai_codex_compatibility_enabled,omitempty"`
	ConversationLogEnabled                bool          `json:"conversation_log_enabled,omitempty"` // Root-only: capture full conversation payloads for distillation
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	CustomModelListURL                    string        `json:"custom_model_list_url,omitempty"`                      // 自定义模型列表 API 地址
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型

	OpenRouterFreeAlphaSyncEnabled               bool              `json:"openrouter_auto_sync_free_and_alpha_models_enabled,omitempty"` // OpenRouter 是否自动维护免费及匿名 Alpha 模型
	OpenRouterFreeModelNameSimplificationEnabled bool              `json:"openrouter_free_model_name_simplification_enabled,omitempty"`  // 是否将 provider/model:free 简化为 model 并生成映射
	OpenRouterFreeModelGeneratedMappings         map[string]string `json:"openrouter_free_model_generated_mappings,omitempty"`           // 已自动生成的免费模型名称映射
	OpenRouterFreeModelPendingMappings           map[string]string `json:"openrouter_free_model_pending_mappings,omitempty"`             // 待应用的免费模型名称映射
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

func (s ChannelOtherSettings) ShouldRemoveGifImages() bool {
	return s.RemoveGifImagesEnabled == nil || *s.RemoveGifImagesEnabled
}
