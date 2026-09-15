package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetReasoningEffortFromRequest(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"chat low", `{"reasoning_effort":"low"}`, "low"},
		{"chat high", `{"reasoning_effort":"high"}`, "high"},
		{"responses xhigh", `{"reasoning":{"effort":"xhigh"}}`, "xhigh"},
		{"explicit none", `{"reasoning_effort":"none"}`, "none"},
		{"future effort", `{"reasoning_effort":"custom-level"}`, "custom-level"},
		{"claude", `{"output_config":{"effort":"max"}}`, "max"},
		{"gemini camel", `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"HIGH"}}}`, "HIGH"},
		{"gemini snake", `{"generation_config":{"thinking_config":{"thinking_level":"low"}}}`, "low"},
		{"gemini mixed", `{"generationConfig":{"thinking_config":{"thinkingLevel":"medium"}}}`, "medium"},
		{"gemini alias precedence", `{"generationConfig":{"thinkingConfig":{"thinkingLevel":"high","thinking_level":"low"}}}`, "low"},
		{"absent", `{"model":"gpt-5-high"}`, ""},
		{"empty", `{"reasoning_effort":""}`, ""},
		{"whitespace", `{"reasoning_effort":"   "}`, ""},
		{"null", `{"reasoning_effort":null}`, ""},
		{"number", `{"reasoning_effort":123}`, ""},
		{"object", `{"reasoning_effort":{"effort":"high"}}`, ""},
		{"thinking budget", `{"thinking":{"type":"enabled","budget_tokens":4096}}`, ""},
		{"gemini budget", `{"generationConfig":{"thinkingConfig":{"thinkingBudget":0}}}`, ""},
		{"openrouter enabled", `{"reasoning":{"enabled":true,"max_tokens":4096}}`, ""},
		{"malformed", `{"reasoning_effort":"high",`, ""},
		{"no body", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{ReasoningEffort: "stale"}
			body := []byte(tc.body)
			SetReasoningEffortFromRequest(info, body)
			require.Equal(t, tc.want, info.ReasoningEffort)
			require.Equal(t, tc.body, string(body), "logging must not change upstream bytes")
		})
	}
	SetReasoningEffortFromRequest(nil, []byte(`{}`))
}

func TestReasoningEffortOpenRouterPrefersNestedEffort(t *testing.T) {
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}}
	SetReasoningEffortFromRequest(info, []byte(`{"reasoning_effort":"low","reasoning":{"effort":"high"}}`))
	require.Equal(t, "high", info.ReasoningEffort)
}

func TestReasoningEffortResetsAtChannelAttempt(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	info := &RelayInfo{ReasoningEffort: "xhigh"}
	info.InitChannelMeta(c)
	require.Empty(t, info.ReasoningEffort)
}
