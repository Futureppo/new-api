package service

import (
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestDeriveConversationLogPayloadOpenAIStream(t *testing.T) {
	body := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"world\",\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\"}]}}]}\n\n" +
		"data: [DONE]\n\n")

	text, toolCalls := deriveConversationLogPayload(body, types.RelayFormatOpenAI, true)

	require.Equal(t, "hello world", text)
	require.Contains(t, toolCalls, "call_1")
}

func TestDeriveConversationLogPayloadClaudeNonStream(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"answer"},{"type":"tool_use","id":"tool_1","name":"lookup"}]}`)

	text, toolCalls := deriveConversationLogPayload(body, types.RelayFormatClaude, false)

	require.Equal(t, "answer", text)
	require.Contains(t, toolCalls, "tool_1")
}
