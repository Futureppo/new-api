// Package openaimodel describes documented OpenAI model capabilities. Model IDs
// and snapshots are exact matches: provider names and future models are opaque.
package openaimodel

import (
	"fmt"
	"slices"
	"strings"
)

type SamplingPolicy uint8

const (
	SamplingUnspecified SamplingPolicy = iota
	SamplingNever
	SamplingWithoutReasoning
)

type ToolPolicy uint8

const (
	ChatTools ToolPolicy = iota
	ResponsesTools
	ResponsesReasoningTools
)

type Capabilities struct {
	CompletionTokens    bool
	DeveloperRole       bool
	Reasoning           bool
	Efforts             string // comma-separated; empty means not documented here
	DefaultEffort       string
	Sampling            SamplingPolicy
	ResponsesOnly       bool
	Tools               ToolPolicy
	LegacyGPT5          bool // preserves existing non-OpenAI adaptor behavior
	LegacyO             bool
	LegacyResponsesOnly bool
}

func (c Capabilities) EffectiveEffort(effort string) string {
	if effort == "" {
		return c.DefaultEffort
	}
	return effort
}

func (c Capabilities) ValidateEffort(model, field, effort string) error {
	if effort == "" || c.Efforts == "" {
		return nil
	}
	if !slices.Contains(strings.Split(c.Efforts, ","), effort) {
		return fmt.Errorf("model %q does not support %s=%q; supported values: %s", model, field, effort, c.Efforts)
	}
	return nil
}

func (c Capabilities) RemoveSampling(effort string) bool {
	return c.Sampling == SamplingNever ||
		(c.Sampling == SamplingWithoutReasoning && c.EffectiveEffort(effort) != "none")
}

func (c Capabilities) RequiresResponses(effort string, hasTools bool) bool {
	return c.ResponsesOnly || (hasTools && (c.Tools == ResponsesTools ||
		(c.Tools == ResponsesReasoningTools && c.EffectiveEffort(effort) != "none")))
}

// Verified 2026-09-24. Sources and deliberately unspecified rules are recorded
// in docs/channel/openai-model-compatibility.md.
var models = func() map[string]Capabilities {
	m := make(map[string]Capabilities)
	add := func(c Capabilities, names ...string) {
		for _, name := range names {
			m[name] = c
		}
	}
	o := Capabilities{CompletionTokens: true, DeveloperRole: true, Reasoning: true, Efforts: "low,medium,high", DefaultEffort: "medium", Sampling: SamplingNever, LegacyO: true}
	add(o, "o1", "o1-2024-12-17", "o3", "o3-2025-04-16", "o3-mini", "o3-mini-2025-01-31", "o4-mini", "o4-mini-2025-04-16")
	oldO := o
	oldO.DeveloperRole, oldO.Efforts = false, ""
	add(oldO, "o1-mini", "o1-mini-2024-09-12", "o1-preview", "o1-preview-2024-09-12")
	proO := o
	proO.ResponsesOnly, proO.Efforts = true, ""
	add(proO, "o1-pro", "o1-pro-2025-03-19")
	proO.LegacyResponsesOnly = true
	add(proO, "o3-pro", "o3-pro-2025-06-10", "o3-deep-research", "o3-deep-research-2025-06-26", "o4-mini-deep-research", "o4-mini-deep-research-2025-06-26")

	gpt5 := Capabilities{CompletionTokens: true, DeveloperRole: true, Reasoning: true, Efforts: "minimal,low,medium,high", DefaultEffort: "medium", Sampling: SamplingNever, LegacyGPT5: true}
	add(gpt5, "gpt-5", "gpt-5-2025-08-07", "gpt-5-mini", "gpt-5-mini-2025-08-07", "gpt-5-nano", "gpt-5-nano-2025-08-07")
	gpt51 := gpt5
	gpt51.Efforts, gpt51.DefaultEffort, gpt51.Sampling = "none,low,medium,high", "none", SamplingWithoutReasoning
	add(gpt51, "gpt-5.1", "gpt-5.1-2025-11-13")
	gpt52 := gpt51
	gpt52.Efforts = "none,low,medium,high,xhigh"
	add(gpt52, "gpt-5.2", "gpt-5.2-2025-12-11", "gpt-5.4", "gpt-5.4-2026-03-05")
	gpt55 := gpt52
	// These model pages document efforts but do not specify sampling support.
	// Do not infer additional restrictions from another model's name or guide.
	gpt55.DefaultEffort, gpt55.Sampling = "medium", SamplingUnspecified
	add(gpt55, "gpt-5.5", "gpt-5.5-2026-04-23")
	gpt56 := gpt55
	gpt56.Efforts = "none,low,medium,high,xhigh,max"
	add(gpt56, "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna")
	gpt6 := gpt56
	gpt6.LegacyGPT5 = false
	gpt6.Sampling, gpt6.Tools = SamplingWithoutReasoning, ResponsesReasoningTools
	add(gpt6, "gpt-6-sol", "gpt-6-luna")
	astra := gpt6
	astra.Efforts, astra.DefaultEffort, astra.Sampling, astra.Tools = "low,medium,high,xhigh,max", "", SamplingNever, ResponsesTools
	add(astra, "gpt-6-astra")

	pro := gpt5
	pro.ResponsesOnly, pro.Efforts, pro.DefaultEffort = true, "high", "high"
	add(pro, "gpt-5-pro", "gpt-5-pro-2025-10-06")
	pro.Efforts, pro.DefaultEffort = "medium,high,xhigh", "medium"
	add(pro, "gpt-5.2-pro", "gpt-5.2-pro-2025-12-11", "gpt-5.4-pro", "gpt-5.4-pro-2026-03-05")
	pro.DefaultEffort, pro.Sampling = "high", SamplingUnspecified
	add(pro, "gpt-5.5-pro", "gpt-5.5-pro-2026-04-23")
	codex := gpt5
	codex.ResponsesOnly, codex.Efforts, codex.Sampling = true, "", SamplingUnspecified
	add(codex, "gpt-5-codex", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5.1-codex-max")
	codex.Efforts = "low,medium,high,xhigh"
	add(codex, "gpt-5.2-codex", "gpt-5.3-codex")
	// Chat/search variants are distinct models, not reasoning family aliases.
	chat := Capabilities{CompletionTokens: true, DeveloperRole: true, LegacyGPT5: true}
	add(chat, "gpt-5-chat-latest", "gpt-5.1-chat-latest", "gpt-5.2-chat-latest", "gpt-5.3-chat-latest", "gpt-5-search-api", "gpt-5-search-api-2025-10-14")
	return m
}()

func Lookup(model string) (Capabilities, bool) {
	c, ok := models[model]
	return c, ok
}

func ResponseOnlyModels() []string {
	names := make([]string, 0)
	for name, capabilities := range models {
		if capabilities.ResponsesOnly {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Resolve gives real model IDs priority over synthetic effort suffixes. In
// particular, gpt-5.1-codex-max is a model, not gpt-5.1-codex with effort=max.
func Resolve(model string) (base, effort string, capabilities Capabilities, known bool) {
	if c, ok := Lookup(model); ok {
		return model, "", c, true
	}
	for _, suffix := range []string{"max", "xhigh", "high", "medium", "low", "minimal", "none"} {
		base, found := strings.CutSuffix(model, "-"+suffix)
		if !found {
			continue
		}
		if c, ok := Lookup(base); ok && c.Reasoning {
			return base, suffix, c, true
		}
	}
	return model, "", Capabilities{}, false
}
