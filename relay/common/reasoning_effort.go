package common

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/tidwall/gjson"
)

// SetReasoningEffortFromRequest records only an explicit effort in the final
// upstream JSON, after conversion and overrides. It never modifies the body or
// infers a level from a token budget, model name, or provider default.
func SetReasoningEffortFromRequest(info *RelayInfo, body []byte) {
	if info == nil {
		return
	}
	// An override may remove the field, or a retry may use a different channel.
	info.ReasoningEffort = ""
	if !gjson.ValidBytes(body) {
		return
	}

	paths := []string{"reasoning_effort", "reasoning.effort", "output_config.effort"}
	if info.ChannelMeta != nil && info.ChannelType == constant.ChannelTypeOpenRouter {
		paths[0], paths[1] = paths[1], paths[0]
	}
	// Gemini accepts both spellings, including mixed casing in passthrough bodies.
	for _, config := range []string{"generationConfig", "generation_config"} {
		for _, thinking := range []string{"thinkingConfig", "thinking_config"} {
			for _, level := range []string{"thinking_level", "thinkingLevel"} {
				paths = append(paths, config+"."+thinking+"."+level)
			}
		}
	}
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			info.ReasoningEffort = value.String()
			return
		}
	}
}
