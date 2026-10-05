package selfhost

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

// NormalizeBaseURL keeps deployment prefixes, including opaque routing paths.
func NormalizeBaseURL(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/models"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	return strings.TrimSuffix(base, "/v1")
}

func ValidateBaseURL(base string) error {
	u, err := url.Parse(NormalizeBaseURL(base))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("vLLM/LiteLLM requires an HTTP(S) deployment URL without credentials, query or fragment")
	}
	return nil
}

func isQwen38(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name == "qwen3.8" || strings.HasPrefix(name, "qwen3.8-")
}

func qwenReasoningEffort(effort string) string {
	switch effort {
	case "minimal":
		return "low"
	case "high":
		return "medium"
	case "max":
		return "xhigh"
	default:
		return effort
	}
}

// NormalizeRequest runs on the final wire body, including overrides and raw
// passthrough. RawMessage retains zero values and fields this adapter does not own.
func NormalizeRequest(body []byte, channelType int, responses bool) ([]byte, string, string, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(body, &fields); err != nil || fields == nil {
		return nil, "", "", fmt.Errorf("request must be a JSON object")
	}
	if channelType == constant.ChannelTypeVLLM {
		if raw, ok := fields["extra_body"]; ok && string(raw) != "null" {
			var extra map[string]json.RawMessage
			if err := common.Unmarshal(raw, &extra); err != nil || extra == nil {
				return nil, "", "", fmt.Errorf("extra_body must be a JSON object")
			}
			for key, value := range extra {
				if _, explicit := fields[key]; !explicit {
					fields[key] = value
				}
			}
		}
		delete(fields, "extra_body")
		delete(fields, "allowed_openai_params")
	}
	var model, effort string
	if err := common.Unmarshal(fields["model"], &model); err != nil || strings.TrimSpace(model) == "" {
		return nil, "", "", fmt.Errorf("model must be a non-empty string")
	}
	if isQwen38(model) && channelType == constant.ChannelTypeLiteLLM {
		if err := normalizeLiteLLMReasoningFields(fields, responses); err != nil {
			return nil, "", "", err
		}
	}
	if responses {
		if raw := fields["reasoning"]; len(raw) > 0 && string(raw) != "null" {
			var reasoning map[string]json.RawMessage
			if err := common.Unmarshal(raw, &reasoning); err != nil || reasoning == nil {
				return nil, "", "", fmt.Errorf("reasoning must be a JSON object")
			}
			if raw := reasoning["effort"]; len(raw) > 0 {
				if err := common.Unmarshal(raw, &effort); err != nil {
					return nil, "", "", fmt.Errorf("reasoning.effort must be a string")
				}
			}
			if isQwen38(model) && qwenReasoningEffort(effort) != effort {
				effort = qwenReasoningEffort(effort)
				reasoning["effort"], _ = common.Marshal(effort)
				fields["reasoning"], _ = common.Marshal(reasoning)
			}
		}
	} else {
		if raw := fields["reasoning_effort"]; len(raw) > 0 {
			if err := common.Unmarshal(raw, &effort); err != nil {
				return nil, "", "", fmt.Errorf("reasoning_effort must be a string")
			}
		}
		if isQwen38(model) && effort != "" {
			if qwenReasoningEffort(effort) != effort {
				effort = qwenReasoningEffort(effort)
				fields["reasoning_effort"], _ = common.Marshal(effort)
			}
			if channelType == constant.ChannelTypeLiteLLM {
				var allowed []string
				if raw := fields["allowed_openai_params"]; len(raw) > 0 {
					if err := common.Unmarshal(raw, &allowed); err != nil {
						return nil, "", "", fmt.Errorf("allowed_openai_params must be an array of strings")
					}
				}
				seen := make(map[string]bool)
				merged := make([]string, 0, len(allowed)+1)
				for _, name := range append(allowed, "reasoning_effort") {
					if !seen[name] {
						merged = append(merged, name)
						seen[name] = true
					}
				}
				fields["allowed_openai_params"], _ = common.Marshal(merged)
			}
		}
	}
	if isQwen38(model) {
		if err := normalizeQwenMessages(fields, responses); err != nil {
			return nil, "", "", err
		}
	}
	var tools []json.RawMessage
	if raw := fields["tools"]; len(raw) > 0 {
		if err := common.Unmarshal(raw, &tools); err != nil {
			return nil, "", "", fmt.Errorf("tools must be an array")
		}
	}
	if len(tools) == 0 {
		delete(fields, "tools")
		if raw := fields["tool_choice"]; len(raw) > 0 && string(raw) != "null" {
			var choice string
			if err := common.Unmarshal(raw, &choice); err != nil || (choice != "none" && choice != "auto") {
				return nil, "", "", fmt.Errorf("tool_choice requires at least one tool")
			}
		}
		delete(fields, "tool_choice")
	}
	result, err := common.Marshal(fields)
	return result, model, effort, err
}

// LiteLLM's extra_body overrides the SDK reasoning field. Normalize that
// effective value at the top level so parameter validation, mapping and logs
// all see the value sent upstream. Other extension fields remain in extra_body.
func normalizeLiteLLMReasoningFields(fields map[string]json.RawMessage, responses bool) error {
	extra := make(map[string]json.RawMessage)
	if raw := fields["extra_body"]; len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" {
		if err := common.Unmarshal(raw, &extra); err != nil || extra == nil {
			return fmt.Errorf("extra_body must be a JSON object")
		}
	}
	key := "reasoning_effort"
	if responses {
		key = "reasoning"
	}
	changed := false
	if value, exists := extra[key]; exists {
		fields[key] = value
		delete(extra, key)
		changed = true
	}
	if responses {
		if kwargs, exists := fields["chat_template_kwargs"]; exists {
			// The Responses proxy ignores top-level template kwargs; the nested
			// form reaches vLLM. Preserve explicitly supplied nested kwargs.
			if _, overridden := extra["chat_template_kwargs"]; !overridden {
				extra["chat_template_kwargs"] = kwargs
			}
			delete(fields, "chat_template_kwargs")
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if len(extra) == 0 {
		delete(fields, "extra_body")
		return nil
	}
	var err error
	fields["extra_body"], err = common.Marshal(extra)
	return err
}
