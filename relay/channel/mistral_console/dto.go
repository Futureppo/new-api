package mistralconsole

import "encoding/json"

type boraConversationRequest struct {
	Model          string             `json:"model"`
	Instructions   string             `json:"instructions"`
	CompletionArgs boraCompletionArgs `json:"completion_args"`
	Tools          []boraTool         `json:"tools,omitempty"`
	Stream         bool               `json:"stream"`
	Inputs         []boraInput        `json:"inputs"`
}

type boraCompletionArgs struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxTokens       *uint    `json:"max_tokens,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort"`
}

type boraTool struct {
	Type     string        `json:"type"`
	Function *boraFunction `json:"function,omitempty"`
}

type boraFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
	Strict      *bool  `json:"strict,omitempty"`
}

// boraInput is the union of Bora conversation entry types used by the
// Chat Completions adapter. Pointer fields preserve required empty values
// without leaking fields into other union variants.
type boraInput struct {
	Object     string  `json:"object"`
	Type       string  `json:"type"`
	Role       string  `json:"role,omitempty"`
	Content    *string `json:"content,omitempty"`
	Prefix     *bool   `json:"prefix,omitempty"`
	Name       string  `json:"name,omitempty"`
	ToolCallID string  `json:"tool_call_id,omitempty"`
	Arguments  *string `json:"arguments,omitempty"`
	Result     *string `json:"result,omitempty"`
}

type boraStreamEvent struct {
	Type           string                 `json:"type"`
	ConversationID string                 `json:"conversation_id"`
	OutputIndex    *int                   `json:"output_index"`
	ID             string                 `json:"id"`
	Model          string                 `json:"model"`
	Name           string                 `json:"name"`
	ToolCallID     string                 `json:"tool_call_id"`
	Arguments      string                 `json:"arguments"`
	Content        json.RawMessage        `json:"content"`
	Info           *boraToolExecutionInfo `json:"info"`
	Usage          *boraUsage             `json:"usage"`
	Message        string                 `json:"message"`
	Error          any                    `json:"error"`
}

type boraThinkingContent struct {
	Type     string             `json:"type"`
	Thinking []boraThinkingPart `json:"thinking"`
	Closed   bool               `json:"closed"`
}

type boraThinkingPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type boraToolExecutionInfo struct {
	// Bora wraps the built-in tool result JSON in a JSON string.
	Result string `json:"result"`
}

type boraImageResult struct {
	URL string `json:"url"`
}

type boraUsage struct {
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	TotalTokens      int            `json:"total_tokens"`
	InputTokens      int            `json:"input_tokens"`
	OutputTokens     int            `json:"output_tokens"`
	Connectors       map[string]int `json:"connectors"`
}
