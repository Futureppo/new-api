package all_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
	_ "github.com/QuantumNous/new-api/relay/ir/codec/all"
)

func sampleRequest(format ir.Format) []byte {
	switch format {
	case ir.FormatOpenAIChat:
		return []byte(`{
			"model": "test-model",
			"stream": false,
			"temperature": 0.4,
			"max_tokens": 512,
			"messages": [
				{"role": "system", "content": "You are concise."},
				{"role": "user", "content": "Check weather in Tokyo."},
				{"role": "assistant", "content": null, "tool_calls": [
					{"id": "call_tokyo_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"}}
				]},
				{"role": "tool", "tool_call_id": "call_tokyo_1", "name": "get_weather", "content": "{\"temp_c\":22}"}
			],
			"tools": [
				{"type": "function", "function": {
					"name": "get_weather",
					"description": "Fetch weather",
					"parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}
				}}
			],
			"tool_choice": "auto"
		}`)
	case ir.FormatOpenAIResponses:
		return []byte(`{
			"model": "test-model",
			"instructions": "You are concise.",
			"max_output_tokens": 512,
			"temperature": 0.4,
			"input": [
				{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Check weather in Tokyo."}]},
				{"type": "function_call", "id": "fc_tokyo_1", "call_id": "call_tokyo_1", "name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"},
				{"type": "function_call_output", "call_id": "call_tokyo_1", "output": "{\"temp_c\":22}"}
			],
			"tools": [
				{"type": "function", "name": "get_weather", "description": "Fetch weather", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}
			],
			"tool_choice": "auto"
		}`)
	case ir.FormatAnthropic:
		return []byte(`{
			"model": "test-model",
			"max_tokens": 512,
			"temperature": 0.4,
			"system": "You are concise.",
			"messages": [
				{"role": "user", "content": [{"type": "text", "text": "Check weather in Tokyo."}]},
				{"role": "assistant", "content": [{"type": "tool_use", "id": "call_tokyo_1", "name": "get_weather", "input": {"city": "Tokyo"}}]},
				{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call_tokyo_1", "content": "{\"temp_c\":22}"}]}
			],
			"tools": [
				{"name": "get_weather", "description": "Fetch weather", "input_schema": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}
			],
			"tool_choice": {"type": "auto"}
		}`)
	case ir.FormatGoogleGenAI:
		return []byte(`{
			"systemInstruction": {"parts": [{"text": "You are concise."}]},
			"contents": [
				{"role": "user", "parts": [{"text": "Check weather in Tokyo."}]},
				{"role": "model", "parts": [{"functionCall": {"id": "call_tokyo_1", "name": "get_weather", "args": {"city": "Tokyo"}}}]},
				{"role": "user", "parts": [{"functionResponse": {"id": "call_tokyo_1", "name": "get_weather", "response": {"temp_c": 22}}}]}
			],
			"tools": [
				{"functionDeclarations": [{"name": "get_weather", "description": "Fetch weather", "parameters": {"type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}]}
			],
			"toolConfig": {"functionCallingConfig": {"mode": "AUTO"}},
			"generationConfig": {"maxOutputTokens": 512, "temperature": 0.4}
		}`)
	default:
		panic("unexpected format")
	}
}

func sampleResponse(format ir.Format) []byte {
	switch format {
	case ir.FormatOpenAIChat:
		return []byte(`{
			"id": "chatcmpl_1",
			"object": "chat.completion",
			"created": 1700000000,
			"model": "test-model",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "It is 22C in Tokyo.",
					"reasoning_content": "The tool returned 22C.",
					"tool_calls": [{"id": "call_next_1", "type": "function", "function": {"name": "format_card", "arguments": "{\"city\":\"Tokyo\"}"}}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 120, "completion_tokens": 45, "total_tokens": 165}
		}`)
	case ir.FormatOpenAIResponses:
		return []byte(`{
			"id": "resp_1",
			"object": "response",
			"created_at": 1700000000,
			"status": "completed",
			"model": "test-model",
			"output": [
				{"type": "reasoning", "id": "rs_1", "summary": [{"type": "summary_text", "text": "The tool returned 22C."}], "encrypted_content": "enc_blob_xyz"},
				{"type": "message", "id": "msg_1", "role": "assistant", "status": "completed", "content": [{"type": "output_text", "text": "It is 22C in Tokyo."}]},
				{"type": "function_call", "id": "fc_next_1", "call_id": "call_next_1", "name": "format_card", "arguments": "{\"city\":\"Tokyo\"}", "status": "completed"}
			],
			"usage": {"input_tokens": 120, "output_tokens": 45, "total_tokens": 165}
		}`)
	case ir.FormatAnthropic:
		return []byte(`{
			"id": "msg_1",
			"type": "message",
			"role": "assistant",
			"model": "test-model",
			"content": [
				{"type": "thinking", "thinking": "The tool returned 22C.", "signature": "anth_sig_abc"},
				{"type": "text", "text": "It is 22C in Tokyo."},
				{"type": "tool_use", "id": "call_next_1", "name": "format_card", "input": {"city": "Tokyo"}}
			],
			"stop_reason": "tool_use",
			"stop_sequence": null,
			"usage": {"input_tokens": 100, "output_tokens": 45, "cache_read_input_tokens": 20}
		}`)
	case ir.FormatGoogleGenAI:
		return []byte(`{
			"responseId": "gem_1",
			"modelVersion": "test-model",
			"candidates": [{
				"index": 0,
				"content": {
					"role": "model",
					"parts": [
						{"text": "The tool returned 22C.", "thought": true},
						{"text": "It is 22C in Tokyo."},
						{"functionCall": {"id": "call_next_1", "name": "format_card", "args": {"city": "Tokyo"}}, "thoughtSignature": "gem_sig_real_99"}
					]
				},
				"finishReason": "STOP"
			}],
			"usageMetadata": {"promptTokenCount": 120, "candidatesTokenCount": 35, "thoughtsTokenCount": 10, "totalTokenCount": 165}
		}`)
	default:
		panic("unexpected format")
	}
}

func sampleStream(format ir.Format) string {
	switch format {
	case ir.FormatOpenAIChat:
		return strings.Join([]string{
			`data: {"id":"chatcmpl_s1","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"Thinking..."},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl_s1","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl_s1","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{"content":"Tokyo!"},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl_s1","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_s1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl_s1","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"ok\"}"}}]},"finish_reason":null}]}`,
			`data: {"id":"chatcmpl_s1","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: {"id":"chatcmpl_s1","object":"chat.completion.chunk","created":1700000000,"model":"test-model","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":20,"total_tokens":70}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"
	case ir.FormatOpenAIResponses:
		return strings.Join([]string{
			"event: response.created\ndata: {\"type\":\"response.created\",\"sequence_number\":0,\"response\":{\"id\":\"resp_s1\",\"model\":\"test-model\",\"created_at\":1700000000,\"status\":\"in_progress\",\"output\":[]}}",
			"event: response.reasoning_summary_text.delta\ndata: {\"type\":\"response.reasoning_summary_text.delta\",\"sequence_number\":1,\"delta\":\"Thinking...\"}",
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":2,\"delta\":\"Hello \"}",
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"sequence_number\":3,\"delta\":\"Tokyo!\"}",
			"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"sequence_number\":4,\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_s1\",\"call_id\":\"call_s1\",\"name\":\"lookup\",\"arguments\":\"\",\"status\":\"in_progress\"}}",
			"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"sequence_number\":5,\"output_index\":1,\"item_id\":\"fc_s1\",\"delta\":\"{\\\"q\\\":\\\"ok\\\"}\"}",
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"sequence_number\":6,\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"fc_s1\",\"call_id\":\"call_s1\",\"name\":\"lookup\",\"arguments\":\"{\\\"q\\\":\\\"ok\\\"}\",\"status\":\"completed\"}}",
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":7,\"response\":{\"id\":\"resp_s1\",\"model\":\"test-model\",\"status\":\"completed\",\"usage\":{\"input_tokens\":50,\"output_tokens\":20,\"total_tokens\":70}}}",
		}, "\n\n") + "\n\n"
	case ir.FormatAnthropic:
		return strings.Join([]string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_s1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"test-model\",\"content\":[],\"usage\":{\"input_tokens\":40,\"cache_read_input_tokens\":10,\"output_tokens\":0}}}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"Thinking...\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Tokyo!\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_s1\",\"name\":\"lookup\",\"input\":{}}}",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":2,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"q\\\":\\\"ok\\\"}\"}}",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":2}",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":20}}",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}",
		}, "\n\n") + "\n\n"
	case ir.FormatGoogleGenAI:
		return strings.Join([]string{
			`data: {"responseId":"gem_s1","modelVersion":"test-model","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Thinking...","thought":true}]}}]}`,
			`data: {"responseId":"gem_s1","modelVersion":"test-model","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Hello "}]}}]}`,
			`data: {"responseId":"gem_s1","modelVersion":"test-model","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Tokyo!"}]}}]}`,
			`data: {"responseId":"gem_s1","modelVersion":"test-model","candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"id":"call_s1","name":"lookup","args":{"q":"ok"}},"thoughtSignature":"gem_stream_sig"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":15,"thoughtsTokenCount":5,"totalTokenCount":70}}`,
		}, "\n\n") + "\n\n"
	default:
		panic("unexpected format")
	}
}

func TestFull4x4MatrixRequestsResponsesAndStreams(t *testing.T) {
	formats := []ir.Format{
		ir.FormatOpenAIChat,
		ir.FormatOpenAIResponses,
		ir.FormatAnthropic,
		ir.FormatGoogleGenAI,
	}

	for _, src := range formats {
		for _, dst := range formats {
			name := string(src) + "_to_" + string(dst)
			t.Run(name, func(t *testing.T) {
				tb := ir.NewTurnBridge(0, 100)
				p, err := ir.NewPipeline(ir.PipelineConfig{
					Bridge:       tb,
					BridgeScope:  name,
					SourceFormat: src,
					TargetFormat: dst,
				})
				if err != nil {
					t.Fatalf("NewPipeline(%s): %v", name, err)
				}

				// 1. Request translation
				outReq, irReq, _, err := p.ConvertRequest(sampleRequest(src))
				if err != nil {
					t.Fatalf("ConvertRequest(%s): %v", name, err)
				}
				if !json.Valid(outReq) {
					t.Fatalf("ConvertRequest(%s) produced invalid JSON: %s", name, string(outReq))
				}
				if len(irReq.Tools) != 1 || irReq.Tools[0].Name != "get_weather" {
					t.Fatalf("ConvertRequest(%s) lost tool definition: %+v", name, irReq.Tools)
				}
				if !strings.Contains(string(outReq), "get_weather") || !strings.Contains(string(outReq), "Tokyo") {
					t.Fatalf("ConvertRequest(%s) missing expected payload content: %s", name, string(outReq))
				}

				// 2. Non-stream response translation
				outResp, irResp, _, err := p.ConvertResponse(sampleResponse(dst), "test-model")
				if err != nil {
					t.Fatalf("ConvertResponse(%s): %v", name, err)
				}
				if !json.Valid(outResp) {
					t.Fatalf("ConvertResponse(%s) produced invalid JSON: %s", name, string(outResp))
				}
				if irResp.Usage == nil || irResp.Usage.InclusiveInputTokens() != 120 || irResp.Usage.OutputTokens != 45 {
					t.Fatalf("ConvertResponse(%s) usage mismatch: %+v", name, irResp.Usage)
				}
				if !strings.Contains(string(outResp), "22C in Tokyo") || !strings.Contains(string(outResp), "format_card") {
					t.Fatalf("ConvertResponse(%s) missing text or tool call: %s", name, string(outResp))
				}

				// 3. Streaming SSE translation
				tr := p.NewStreamTranslator("test-model")
				var emitted strings.Builder
				frameCount := 0
				err = tr.TranslateReader(strings.NewReader(sampleStream(dst)), func(f ir.SSEFrame) error {
					frameCount++
					emitted.Write(ir.FormatSSEFrame(f))
					return nil
				})
				if err != nil {
					t.Fatalf("TranslateReader(%s): %v", name, err)
				}
				if frameCount == 0 {
					t.Fatalf("TranslateReader(%s) emitted 0 frames", name)
				}
				wire := emitted.String()
				if !strings.Contains(wire, "Hello ") || !strings.Contains(wire, "Tokyo!") || !strings.Contains(wire, "lookup") {
					t.Fatalf("TranslateReader(%s) missing streamed text or tool call:\n%s", name, wire)
				}
				if tr.Usage() == nil || tr.Usage().InclusiveInputTokens() != 50 || tr.Usage().OutputTokens != 20 {
					t.Fatalf("TranslateReader(%s) usage mismatch: %+v", name, tr.Usage())
				}
			})
		}
	}
}

func TestTurnBridgeMultiTurnCrossProtocolSignatures(t *testing.T) {
	tb := ir.NewTurnBridge(0, 100)

	// Scenario A: OpenAI Chat client -> OpenAI Responses upstream (bridges fc_* item_id across turns)
	pResp, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       tb,
		BridgeScope:  "chan:responses",
		SourceFormat: ir.FormatOpenAIChat,
		TargetFormat: ir.FormatOpenAIResponses,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := pResp.ConvertResponse(sampleResponse(ir.FormatOpenAIResponses), "gpt-5"); err != nil {
		t.Fatal(err)
	}
	turn2Chat := []byte(`{
		"model": "gpt-5",
		"messages": [
			{"role": "user", "content": "Hi"},
			{"role": "assistant", "content": null, "tool_calls": [{"id": "call_next_1", "type": "function", "function": {"name": "format_card", "arguments": "{\"city\":\"Tokyo\"}"}}]},
			{"role": "tool", "tool_call_id": "call_next_1", "content": "done"}
		]
	}`)
	targetTurn2, _, _, err := pResp.ConvertRequest(turn2Chat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(targetTurn2), `"fc_next_1"`) {
		t.Fatalf("expected TurnBridge to restore Responses item id fc_next_1, got: %s", string(targetTurn2))
	}

	// Scenario B: Anthropic client -> Google GenAI upstream (bridges real thoughtSignature across turns)
	pGem, err := ir.NewPipeline(ir.PipelineConfig{
		Bridge:       tb,
		BridgeScope:  "chan:gemini",
		SourceFormat: ir.FormatAnthropic,
		TargetFormat: ir.FormatGoogleGenAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := pGem.ConvertResponse(sampleResponse(ir.FormatGoogleGenAI), "gemini-3-pro"); err != nil {
		t.Fatal(err)
	}
	turn2Claude := []byte(`{
		"model": "gemini-3-pro",
		"max_tokens": 512,
		"messages": [
			{"role": "user", "content": "Hi"},
			{"role": "assistant", "content": [{"type": "tool_use", "id": "call_next_1", "name": "format_card", "input": {"city": "Tokyo"}}]},
			{"role": "user", "content": [{"type": "tool_result", "tool_use_id": "call_next_1", "content": "done"}]}
		]
	}`)
	targetGem2, _, _, err := pGem.ConvertRequest(turn2Claude)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(targetGem2), `"gem_sig_real_99"`) {
		t.Fatalf("expected TurnBridge to restore real Gemini thoughtSignature gem_sig_real_99, got: %s", string(targetGem2))
	}

	// Scenario C: OpenAI Responses encrypted_content -> Anthropic must NOT leak redacted_thinking
	pAnth, err := ir.NewPipeline(ir.PipelineConfig{
		SourceFormat: ir.FormatOpenAIResponses,
		TargetFormat: ir.FormatAnthropic,
	})
	if err != nil {
		t.Fatal(err)
	}
	respWithEncrypted := []byte(`{
		"model": "claude-sonnet-4-5",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hi"}]},
			{"type": "reasoning", "id": "rs_9", "encrypted_content": "foreign_encrypted_blob", "summary": [{"type": "summary_text", "text": "secret"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "Hello"}]},
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Follow up"}]}
		]
	}`)
	anthOut, _, convCtx, err := pAnth.ConvertRequest(respWithEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(anthOut), "redacted_thinking") || strings.Contains(string(anthOut), "foreign_encrypted_blob") {
		t.Fatalf("foreign encrypted_content must be stripped for Anthropic, got: %s", string(anthOut))
	}
	if len(convCtx.Warnings) == 0 {
		t.Fatalf("expected warning when foreign reasoning is stripped")
	}
}

func TestVertexAndBedrockShims(t *testing.T) {
	// OpenAI Responses -> Vertex Claude
	pVertexClaude, err := ir.NewPipeline(ir.PipelineConfig{
		SourceFormat: ir.FormatOpenAIResponses,
		TargetFormat: ir.FormatAnthropic,
		TargetShim:   ir.VertexClaudeShim,
	})
	if err != nil {
		t.Fatal(err)
	}
	vcReq, _, _, err := pVertexClaude.ConvertRequest(sampleRequest(ir.FormatOpenAIResponses))
	if err != nil {
		t.Fatal(err)
	}
	var vcMap map[string]any
	if err := json.Unmarshal(vcReq, &vcMap); err != nil {
		t.Fatal(err)
	}
	if _, hasModel := vcMap["model"]; hasModel {
		t.Fatalf("VertexClaudeShim must strip model, got: %s", string(vcReq))
	}
	if vcMap["anthropic_version"] != "vertex-2023-10-16" {
		t.Fatalf("VertexClaudeShim missing vertex anthropic_version: %v", vcMap["anthropic_version"])
	}

	// Google GenAI -> AWS Bedrock Claude
	pBedrock, err := ir.NewPipeline(ir.PipelineConfig{
		SourceFormat: ir.FormatGoogleGenAI,
		TargetFormat: ir.FormatAnthropic,
		TargetShim:   ir.BedrockClaudeShim,
	})
	if err != nil {
		t.Fatal(err)
	}
	bcReq, _, _, err := pBedrock.ConvertRequest(sampleRequest(ir.FormatGoogleGenAI))
	if err != nil {
		t.Fatal(err)
	}
	var bcMap map[string]any
	if err := json.Unmarshal(bcReq, &bcMap); err != nil {
		t.Fatal(err)
	}
	if _, hasModel := bcMap["model"]; hasModel {
		t.Fatalf("BedrockClaudeShim must strip model")
	}
	if _, hasStream := bcMap["stream"]; hasStream {
		t.Fatalf("BedrockClaudeShim must strip stream")
	}
	if bcMap["anthropic_version"] != "bedrock-2023-05-31" {
		t.Fatalf("BedrockClaudeShim missing bedrock anthropic_version: %v", bcMap["anthropic_version"])
	}

	// OpenAI Responses -> Vertex Gemini
	pVertexGem, err := ir.NewPipeline(ir.PipelineConfig{
		SourceFormat: ir.FormatOpenAIResponses,
		TargetFormat: ir.FormatGoogleGenAI,
		TargetShim:   ir.VertexGeminiShim,
	})
	if err != nil {
		t.Fatal(err)
	}
	vgReq, _, _, err := pVertexGem.ConvertRequest(sampleRequest(ir.FormatOpenAIResponses))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(vgReq), ir.GoogleDummyThoughtSignature) {
		t.Fatalf("expected Gemini 3 dummy thoughtSignature injected on cross-provider functionCall, got: %s", string(vgReq))
	}
	if strings.Contains(string(vgReq), `"functionResponse":{"id"`) {
		t.Fatalf("VertexGeminiShim must strip functionResponse.id, got: %s", string(vgReq))
	}
}
