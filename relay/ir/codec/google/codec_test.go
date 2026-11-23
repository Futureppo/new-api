package google

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
)

func TestFormatAndInterface(t *testing.T) {
	c := New()
	if c.Format() != ir.FormatGoogleGenAI {
		t.Fatalf("expected format %q, got %q", ir.FormatGoogleGenAI, c.Format())
	}
}

func TestDecodeRequestCamelCaseAndSnakeCase(t *testing.T) {
	camelRaw := []byte(`{
		"systemInstruction": {"parts": [{"text": "You are a helpful assistant."}]},
		"contents": [
			{
				"parts": [
					{"text": "Analyze this image and audio"},
					{"inlineData": {"mimeType": "image/png", "data": "aW1n"}},
					{"inlineData": {"mimeType": "audio/wav", "data": "YXVk"}},
					{"inlineData": {"mimeType": "application/pdf", "data": "cGRm"}},
					{"fileData": {"mimeType": "image/jpeg", "fileUri": "gs://bucket/pic.jpg"}},
					{"fileData": {"mimeType": "application/pdf", "fileUri": "gs://bucket/doc.pdf"}}
				]
			},
			{
				"role": "model",
				"parts": [
					{"text": "Let me check weather", "thought": true, "thoughtSignature": "sig_thought_1"},
					{"functionCall": {"name": "get_weather", "args": {"city": "Tokyo"}}, "thoughtSignature": "sig_fc_1"},
					{"functionCall": {"name": "get_weather", "args": {"city": "Osaka"}}}
				]
			},
			{
				"role": "user",
				"parts": [
					{"functionResponse": {"name": "get_weather", "response": {"result": "Sunny 22C"}}},
					{"functionResponse": {"name": "get_weather", "response": {"temp": 20, "condition": "cloudy"}}}
				]
			}
		],
		"tools": [
			{
				"functionDeclarations": [
					{
						"name": "get_weather",
						"description": "Get city weather",
						"parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
					}
				]
			},
			{"googleSearch": {}},
			{"codeExecution": {}},
			{"urlContext": {}}
		],
		"toolConfig": {
			"functionCallingConfig": {
				"mode": "ANY",
				"allowedFunctionNames": ["get_weather"]
			}
		},
		"generationConfig": {
			"maxOutputTokens": 2048,
			"temperature": 0.7,
			"topP": 0.9,
			"topK": 40,
			"stopSequences": ["END"],
			"candidateCount": 2,
			"presencePenalty": 0.1,
			"frequencyPenalty": 0.2,
			"seed": 42,
			"responseMimeType": "application/json",
			"responseSchema": {"type": "object", "properties": {"answer": {"type": "string"}}},
			"thinkingConfig": {
				"includeThoughts": true,
				"thinkingBudget": 1024,
				"thinkingLevel": "HIGH"
			}
		}
	}`)

	ctx := ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI)
	c := New()
	req, err := c.DecodeRequest(camelRaw, ctx)
	if err != nil {
		t.Fatalf("DecodeRequest camelCase failed: %v", err)
	}

	if len(req.System) != 1 || req.System[0].Text != "You are a helpful assistant." {
		t.Fatalf("unexpected system parts: %+v", req.System)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}
	// Empty first role defaults to RoleUser
	if req.Messages[0].Role != ir.RoleUser {
		t.Fatalf("expected first message role user, got %q", req.Messages[0].Role)
	}
	if len(req.Messages[0].Parts) != 6 {
		t.Fatalf("expected 6 parts in user message, got %d", len(req.Messages[0].Parts))
	}
	if req.Messages[0].Parts[1].Type != ir.PartImage || req.Messages[0].Parts[1].Image.Data != "aW1n" {
		t.Fatalf("expected inline image part, got %+v", req.Messages[0].Parts[1])
	}
	if req.Messages[0].Parts[2].Type != ir.PartAudio || req.Messages[0].Parts[2].Audio.Format != "wav" {
		t.Fatalf("expected inline audio part, got %+v", req.Messages[0].Parts[2])
	}
	if req.Messages[0].Parts[3].Type != ir.PartFile || req.Messages[0].Parts[3].File.MediaType != "application/pdf" {
		t.Fatalf("expected inline file part, got %+v", req.Messages[0].Parts[3])
	}
	if req.Messages[0].Parts[4].Type != ir.PartImage || req.Messages[0].Parts[4].Image.URL != "gs://bucket/pic.jpg" {
		t.Fatalf("expected fileData image part, got %+v", req.Messages[0].Parts[4])
	}
	if req.Messages[0].Parts[5].Type != ir.PartFile || req.Messages[0].Parts[5].File.URL != "gs://bucket/doc.pdf" {
		t.Fatalf("expected fileData file part, got %+v", req.Messages[0].Parts[5])
	}

	// Model message with thought + 2 functionCalls
	modelMsg := req.Messages[1]
	if modelMsg.Role != ir.RoleAssistant || len(modelMsg.Parts) != 3 {
		t.Fatalf("unexpected model message: %+v", modelMsg)
	}
	if modelMsg.Parts[0].Type != ir.PartReasoning ||
		modelMsg.Parts[0].Reasoning.Signature != "sig_thought_1" ||
		modelMsg.Parts[0].Reasoning.OriginFormat != ir.FormatGoogleGenAI {
		t.Fatalf("unexpected reasoning part: %+v", modelMsg.Parts[0])
	}
	fc1 := modelMsg.Parts[1].ToolCall
	fc2 := modelMsg.Parts[2].ToolCall
	if fc1 == nil || fc1.ID != "call_1_1" || fc1.ThoughtSignature != "sig_fc_1" || fc1.Arguments != `{"city":"Tokyo"}` {
		t.Fatalf("unexpected fc1: %+v", fc1)
	}
	if fc2 == nil || fc2.ID != "call_1_2" || fc2.Arguments != `{"city":"Osaka"}` {
		t.Fatalf("unexpected fc2: %+v", fc2)
	}

	// Tool message with FIFO correlation
	toolMsg := req.Messages[2]
	if toolMsg.Role != ir.RoleTool || len(toolMsg.Parts) != 2 {
		t.Fatalf("unexpected tool message: %+v", toolMsg)
	}
	tr1 := toolMsg.Parts[0].ToolResult
	tr2 := toolMsg.Parts[1].ToolResult
	if tr1.ToolCallID != "call_1_1" || tr1.Name != "get_weather" || tr1.Content != "Sunny 22C" {
		t.Fatalf("unexpected tr1: %+v", tr1)
	}
	if tr2.ToolCallID != "call_1_2" || tr2.Name != "get_weather" || tr2.Content != `{"temp":20,"condition":"cloudy"}` {
		t.Fatalf("unexpected tr2: %+v", tr2)
	}

	// Tools & ToolChoice
	if len(req.Tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(req.Tools))
	}
	if req.Tools[0].Type != "function" || req.Tools[0].Name != "get_weather" {
		t.Fatalf("unexpected function tool: %+v", req.Tools[0])
	}
	if req.Tools[1].Type != "googleSearch" || req.Tools[1].OriginFormat != ir.FormatGoogleGenAI {
		t.Fatalf("unexpected googleSearch tool: %+v", req.Tools[1])
	}
	if req.ToolChoice == nil || req.ToolChoice.Mode != "tool" || req.ToolChoice.ToolName != "get_weather" {
		t.Fatalf("expected tool choice mode=tool name=get_weather, got %+v", req.ToolChoice)
	}

	// GenerationConfig & Reasoning & ResponseFormat
	if req.Generation.MaxTokens == nil || *req.Generation.MaxTokens != 2048 {
		t.Fatalf("unexpected maxTokens: %v", req.Generation.MaxTokens)
	}
	if req.Generation.TopK == nil || *req.Generation.TopK != 40 || req.Generation.N == nil || *req.Generation.N != 2 {
		t.Fatalf("unexpected generation params: %+v", req.Generation)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" {
		t.Fatalf("unexpected responseFormat: %+v", req.ResponseFormat)
	}
	if req.Reasoning == nil || req.Reasoning.BudgetTokens == nil || *req.Reasoning.BudgetTokens != 1024 || req.Reasoning.Effort != "high" {
		t.Fatalf("unexpected reasoning config: %+v", req.Reasoning)
	}

	// Now test full snake_case alias decoding!
	snakeRaw := []byte(`{
		"system_instruction": {"parts": [{"text": "Snake sys"}]},
		"contents": [
			{
				"role": "user",
				"parts": [
					{"inline_data": {"mime_type": "image/webp", "data": "d2VicA=="}},
					{"file_data": {"mime_type": "application/pdf", "file_uri": "gs://b/snake.pdf"}}
				]
			},
			{
				"role": "model",
				"parts": [
					{"function_call": {"id": "fc_snake_1", "name": "search", "args": {"q": "go"}}, "thought_signature": "sig_snake"}
				]
			},
			{
				"role": "user",
				"parts": [
					{"function_response": {"id": "fc_snake_1", "name": "search", "response": {"content": "found"}}}
				]
			}
		],
		"tools": [
			{"function_declarations": [{"name": "search", "description": "Search"}]},
			{"google_search": {}}
		],
		"tool_config": {
			"function_calling_config": {
				"mode": "ANY",
				"allowed_function_names": ["search", "other"]
			}
		},
		"generation_config": {
			"max_output_tokens": 512,
			"top_p": 0.85,
			"top_k": 32.0,
			"stop_sequences": ["STOP1"],
			"candidate_count": 1,
			"presence_penalty": 0.3,
			"frequency_penalty": 0.4,
			"response_mime_type": "application/json",
			"thinking_config": {
				"include_thoughts": true,
				"thinking_budget": 2048,
				"thinking_level": "MEDIUM"
			}
		}
	}`)

	sReq, err := c.DecodeRequest(snakeRaw, ctx)
	if err != nil {
		t.Fatalf("DecodeRequest snake_case failed: %v", err)
	}
	if len(sReq.System) != 1 || sReq.System[0].Text != "Snake sys" {
		t.Fatalf("snake system_instruction not decoded: %+v", sReq.System)
	}
	if sReq.Messages[0].Parts[0].Image == nil || sReq.Messages[0].Parts[0].Image.MediaType != "image/webp" {
		t.Fatalf("snake inline_data not decoded: %+v", sReq.Messages[0].Parts[0])
	}
	if sReq.Messages[0].Parts[1].File == nil || sReq.Messages[0].Parts[1].File.URL != "gs://b/snake.pdf" {
		t.Fatalf("snake file_data not decoded: %+v", sReq.Messages[0].Parts[1])
	}
	if sReq.Messages[1].Parts[0].ToolCall == nil ||
		sReq.Messages[1].Parts[0].ToolCall.ID != "fc_snake_1" ||
		sReq.Messages[1].Parts[0].ToolCall.ThoughtSignature != "sig_snake" {
		t.Fatalf("snake function_call/thought_signature not decoded: %+v", sReq.Messages[1].Parts[0])
	}
	if sReq.Messages[2].Role != ir.RoleTool || sReq.Messages[2].Parts[0].ToolResult.Content != "found" {
		t.Fatalf("snake function_response not decoded: %+v", sReq.Messages[2])
	}
	if len(sReq.Tools) != 2 || sReq.Tools[0].Name != "search" || sReq.Tools[1].Type != "googleSearch" || sReq.Tools[1].OriginFormat != ir.FormatGoogleGenAI {
		t.Fatalf("snake tools not decoded: %+v", sReq.Tools)
	}
	if sReq.ToolChoice == nil || sReq.ToolChoice.Mode != "required" || len(sReq.ToolChoice.AllowedTools) != 2 {
		t.Fatalf("snake tool_config not decoded: %+v", sReq.ToolChoice)
	}
	if sReq.Generation.MaxTokens == nil || *sReq.Generation.MaxTokens != 512 ||
		sReq.Generation.TopK == nil || *sReq.Generation.TopK != 32 ||
		sReq.ResponseFormat == nil || sReq.ResponseFormat.Type != "json_object" ||
		sReq.Reasoning == nil || *sReq.Reasoning.BudgetTokens != 2048 || sReq.Reasoning.Effort != "medium" {
		t.Fatalf("snake generation_config not decoded: gen=%+v fmt=%+v reasoning=%+v", sReq.Generation, sReq.ResponseFormat, sReq.Reasoning)
	}
}

func TestEncodeRequestSchemaSanitization(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatGoogleGenAI)

	rawSchema := json.RawMessage(`{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id": "https://example.com/post.schema.json",
		"title": "CreatePostParams",
		"type": "object",
		"additionalProperties": false,
		"$defs": {
			"Tag": {
				"type": "object",
				"title": "TagObj",
				"additionalProperties": false,
				"properties": {
					"label": {"type": "string", "title": "Label"}
				}
			}
		},
		"properties": {
			"title": {
				"type": "string",
				"title": "Post Title",
				"examples": ["Hello"]
			},
			"subtitle": {
				"type": ["string", "null"]
			},
			"status": {
				"const": "published"
			},
			"optional_note": {
				"anyOf": [{"type": "string"}, {"type": "null"}]
			},
			"tags": {
				"type": "array",
				"items": {"$ref": "#/$defs/Tag"}
			}
		},
		"required": ["title"]
	}`)

	req := &ir.Request{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Create a post"}}},
		},
		Tools: []ir.ToolDefinition{
			{
				Type:        "function",
				Name:        "create_post",
				Description: "Creates a blog post",
				Parameters:  rawSchema,
			},
		},
		ResponseFormat: &ir.ResponseFormat{
			Type:   "json_schema",
			Schema: rawSchema,
		},
	}

	out, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}

	var wire map[string]any
	if err := json.Unmarshal(out, &wire); err != nil {
		t.Fatalf("unmarshal encoded request: %v", err)
	}

	tools := wire["tools"].([]any)
	decls := tools[0].(map[string]any)["functionDeclarations"].([]any)
	params := decls[0].(map[string]any)["parameters"].(map[string]any)

	// Unsupported schema-level keys stripped
	for _, forbidden := range []string{"$schema", "$id", "title", "additionalProperties", "$defs"} {
		if _, exists := params[forbidden]; exists {
			t.Fatalf("expected schema key %q to be stripped, got %v", forbidden, params)
		}
	}

	// User property named "title" MUST survive inside properties, while its inner metadata "title" and "examples" are stripped!
	props := params["properties"].(map[string]any)
	titleProp, ok := props["title"].(map[string]any)
	if !ok {
		t.Fatalf("expected user property 'title' to be preserved, got %v", props)
	}
	if _, exists := titleProp["title"]; exists {
		t.Fatalf("expected inner schema 'title' metadata to be stripped: %v", titleProp)
	}
	if _, exists := titleProp["examples"]; exists {
		t.Fatalf("expected 'examples' to be stripped: %v", titleProp)
	}

	// Nullable array type normalized
	subProp := props["subtitle"].(map[string]any)
	if subProp["type"] != "string" || subProp["nullable"] != true {
		t.Fatalf("expected subtitle normalized to type=string nullable=true, got %v", subProp)
	}

	// const converted to enum
	statusProp := props["status"].(map[string]any)
	if _, hasConst := statusProp["const"]; hasConst {
		t.Fatalf("expected const stripped, got %v", statusProp)
	}
	enumVals, ok := statusProp["enum"].([]any)
	if !ok || len(enumVals) != 1 || enumVals[0] != "published" {
		t.Fatalf("expected const converted to enum=['published'], got %v", statusProp)
	}

	// anyOf with null simplified
	optNote := props["optional_note"].(map[string]any)
	if optNote["type"] != "string" || optNote["nullable"] != true {
		t.Fatalf("expected anyOf nullable simplified, got %v", optNote)
	}

	// $ref into $defs inlined & sanitized
	tagsItems := props["tags"].(map[string]any)["items"].(map[string]any)
	if _, hasRef := tagsItems["$ref"]; hasRef {
		t.Fatalf("expected $ref resolved and stripped, got %v", tagsItems)
	}
	if _, hasAP := tagsItems["additionalProperties"]; hasAP {
		t.Fatalf("expected inlined $defs additionalProperties stripped, got %v", tagsItems)
	}
	if tagsItems["type"] != "object" {
		t.Fatalf("expected inlined Tag object schema, got %v", tagsItems)
	}

	// ResponseFormat json_schema also sanitized
	genCfg := wire["generationConfig"].(map[string]any)
	if genCfg["responseMimeType"] != "application/json" {
		t.Fatalf("expected responseMimeType application/json, got %v", genCfg["responseMimeType"])
	}
	respSchema := genCfg["responseSchema"].(map[string]any)
	if _, hasSchema := respSchema["$schema"]; hasSchema {
		t.Fatalf("expected responseSchema $schema stripped, got %v", respSchema)
	}
}

func TestEncodeRequestGemini3ThoughtSignature(t *testing.T) {
	c := New()

	// Case 1: Same-provider preserves real thoughtSignature on first call, omits dummy on 2nd parallel call.
	sameReq := &ir.Request{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Weather in Tokyo and Paris?"}}},
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "Need two tool calls",
							Signature:    "sig_reason_1",
							OriginFormat: ir.FormatGoogleGenAI,
						},
					},
					{
						Type: ir.PartToolCall,
						ToolCall: &ir.ToolCall{
							ID:               "call_1",
							Name:             "get_weather",
							Arguments:        `{"city":"Tokyo"}`,
							ThoughtSignature: "sig_real_gemini_1",
						},
					},
					{
						Type: ir.PartToolCall,
						ToolCall: &ir.ToolCall{
							ID:        "call_2",
							Name:      "get_weather",
							Arguments: `{"city":"Paris"}`,
						},
					},
				},
			},
			{
				Role: ir.RoleTool,
				Parts: []ir.ContentPart{
					{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "call_1", Content: "22C"}},
					{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "call_2", Content: `{"temp":18}`}},
				},
			},
		},
	}

	ctxSame := ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI)
	outSame, err := c.EncodeRequest(sameReq, ctxSame)
	if err != nil {
		t.Fatalf("EncodeRequest same-provider failed: %v", err)
	}
	var wireSame googleRequest
	if err := json.Unmarshal(outSame, &wireSame); err != nil {
		t.Fatalf("unmarshal wireSame: %v", err)
	}
	modelParts := wireSame.Contents[1].Parts
	if len(modelParts) != 3 {
		t.Fatalf("expected 3 parts in model turn (thought + 2 fc), got %d", len(modelParts))
	}
	if !modelParts[0].Thought || modelParts[0].ThoughtSignature != "sig_reason_1" {
		t.Fatalf("expected Google reasoning preserved, got %+v", modelParts[0])
	}
	if modelParts[1].ThoughtSignature != "sig_real_gemini_1" {
		t.Fatalf("expected real thoughtSignature preserved on fc1, got %q", modelParts[1].ThoughtSignature)
	}
	if modelParts[2].ThoughtSignature != "" {
		t.Fatalf("expected empty thoughtSignature on 2nd parallel fc2, got %q", modelParts[2].ThoughtSignature)
	}
	// Tool result name correlated from prior tool call even when omitted!
	userToolParts := wireSame.Contents[2].Parts
	if userToolParts[0].FunctionResponse.Name != "get_weather" || string(userToolParts[0].FunctionResponse.Response) != `{"result":"22C"}` {
		t.Fatalf("unexpected first functionResponse: %+v %s", userToolParts[0].FunctionResponse, string(userToolParts[0].FunctionResponse.Response))
	}
	if userToolParts[1].FunctionResponse.Name != "get_weather" || string(userToolParts[1].FunctionResponse.Response) != `{"temp":18}` {
		t.Fatalf("unexpected second functionResponse: %+v %s", userToolParts[1].FunctionResponse, string(userToolParts[1].FunctionResponse.Response))
	}

	// Case 2: Cross-provider (no thoughtSignature, foreign reasoning) -> injects GoogleDummyThoughtSignature on first fc of EACH model turn & drops foreign reasoning with warning.
	crossReq := &ir.Request{
		Messages: []ir.Message{
			{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Do step 1"}}},
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{
						Type: ir.PartReasoning,
						Reasoning: &ir.Reasoning{
							Text:         "Claude internal thoughts",
							Signature:    "anthropic_sig",
							OriginFormat: ir.FormatAnthropic,
						},
					},
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "c1", Name: "step_one", Arguments: `{}`}},
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "c2", Name: "step_one_b", Arguments: `{}`}},
				},
			},
			{Role: ir.RoleTool, Parts: []ir.ContentPart{
				{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "c1", Content: "ok"}},
				{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "c2", Content: "ok"}},
			}},
			{
				Role: ir.RoleAssistant,
				Parts: []ir.ContentPart{
					{Type: ir.PartToolCall, ToolCall: &ir.ToolCall{ID: "c3", Name: "step_two", Arguments: `{}`}},
				},
			},
			{Role: ir.RoleTool, Parts: []ir.ContentPart{
				{Type: ir.PartToolResult, ToolResult: &ir.ToolResult{ToolCallID: "c3", Content: "done"}},
			}},
		},
	}

	ctxCross := ir.NewConversionContext(ir.FormatAnthropic, ir.FormatGoogleGenAI)
	outCross, err := c.EncodeRequest(crossReq, ctxCross)
	if err != nil {
		t.Fatalf("EncodeRequest cross-provider failed: %v", err)
	}
	if len(ctxCross.Warnings) == 0 || ctxCross.Warnings[0].Code != "dropped_cross_provider_reasoning" {
		t.Fatalf("expected warning for dropped cross-provider reasoning, got %+v", ctxCross.Warnings)
	}
	var wireCross googleRequest
	if err := json.Unmarshal(outCross, &wireCross); err != nil {
		t.Fatalf("unmarshal wireCross: %v", err)
	}
	// First model turn (index 1): reasoning dropped, fc1 gets dummy sig, fc2 has no sig
	if len(wireCross.Contents[1].Parts) != 2 {
		t.Fatalf("expected 2 fc parts after dropping cross-provider reasoning, got %d", len(wireCross.Contents[1].Parts))
	}
	if wireCross.Contents[1].Parts[0].ThoughtSignature != ir.GoogleDummyThoughtSignature {
		t.Fatalf("expected dummy thoughtSignature on turn 1 fc1, got %q", wireCross.Contents[1].Parts[0].ThoughtSignature)
	}
	if wireCross.Contents[1].Parts[1].ThoughtSignature != "" {
		t.Fatalf("expected empty thoughtSignature on turn 1 fc2, got %q", wireCross.Contents[1].Parts[1].ThoughtSignature)
	}
	// Second model turn (index 3): fc3 also gets dummy sig!
	if wireCross.Contents[3].Parts[0].ThoughtSignature != ir.GoogleDummyThoughtSignature {
		t.Fatalf("expected dummy thoughtSignature on turn 2 fc3, got %q", wireCross.Contents[3].Parts[0].ThoughtSignature)
	}

	// Case 3: Explicit opt-out attach_thought_signature = false -> no dummy injected
	ctxNoDummy := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatGoogleGenAI)
	ctxNoDummy.Options["attach_thought_signature"] = false
	outNoDummy, err := c.EncodeRequest(crossReq, ctxNoDummy)
	if err != nil {
		t.Fatalf("EncodeRequest opt-out failed: %v", err)
	}
	var wireNoDummy googleRequest
	if err := json.Unmarshal(outNoDummy, &wireNoDummy); err != nil {
		t.Fatalf("unmarshal wireNoDummy: %v", err)
	}
	if wireNoDummy.Contents[1].Parts[0].ThoughtSignature != "" {
		t.Fatalf("expected no dummy thoughtSignature when opted out, got %q", wireNoDummy.Contents[1].Parts[0].ThoughtSignature)
	}
}

func TestEncodeRequestSystemMergeMediaAndConfig(t *testing.T) {
	c := New()
	ctx := ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatGoogleGenAI)
	req := &ir.Request{
		System: []ir.ContentPart{{Type: ir.PartText, Text: "Sys A"}},
		Messages: []ir.Message{
			{Role: ir.RoleDeveloper, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Dev B"}}},
			{Role: ir.RoleUser, Parts: []ir.ContentPart{
				{Type: ir.PartText, Text: "Hi"},
				{Type: ir.PartImage, Image: &ir.ImageSource{URL: "https://example.com/cat.png", MediaType: "image/png"}},
			}},
			{Role: ir.RoleUser, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "Follow up"}}},
		},
		Tools: []ir.ToolDefinition{
			{Type: "googleSearch", OriginFormat: ir.FormatGoogleGenAI, NativeRaw: json.RawMessage(`{"google_search":{}}`)},
			{Type: "computer_use", OriginFormat: ir.FormatAnthropic},
		},
		ToolChoice: &ir.ToolChoice{Mode: "tool", ToolName: "my_fn"},
		Generation: ir.GenerationConfig{
			MaxTokens:     intPtr(1000),
			StopSequences: []string{"1", "2", "3", "4", "5", "6"},
		},
		Reasoning: &ir.ReasoningConfig{
			Effort: "high",
		},
	}

	out, err := c.EncodeRequest(req, ctx)
	if err != nil {
		t.Fatalf("EncodeRequest failed: %v", err)
	}

	var wire googleRequest
	if err := json.Unmarshal(out, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wire.SystemInstruction == nil || len(wire.SystemInstruction.Parts) != 2 {
		t.Fatalf("expected 2 systemInstruction parts, got %+v", wire.SystemInstruction)
	}
	// Adjacent user messages merged into 1 user content with 3 parts
	if len(wire.Contents) != 1 || wire.Contents[0].Role != "user" || len(wire.Contents[0].Parts) != 3 {
		t.Fatalf("expected merged single user content with 3 parts, got %+v", wire.Contents)
	}
	// StopSequences truncated to 5
	if len(wire.GenerationConfig.StopSequences) != 5 {
		t.Fatalf("expected 5 stop sequences, got %d", len(wire.GenerationConfig.StopSequences))
	}
	// Reasoning effort -> thinkingLevel HIGH
	if wire.GenerationConfig.ThinkingConfig == nil ||
		wire.GenerationConfig.ThinkingConfig.ThinkingLevel != "HIGH" ||
		wire.GenerationConfig.ThinkingConfig.IncludeThoughts == nil ||
		!*wire.GenerationConfig.ThinkingConfig.IncludeThoughts {
		t.Fatalf("unexpected thinkingConfig: %+v", wire.GenerationConfig.ThinkingConfig)
	}
	// ToolConfig mapped
	if wire.ToolConfig == nil || wire.ToolConfig.FunctionCallingConfig.Mode != "ANY" ||
		len(wire.ToolConfig.FunctionCallingConfig.AllowedFunctionNames) != 1 ||
		wire.ToolConfig.FunctionCallingConfig.AllowedFunctionNames[0] != "my_fn" {
		t.Fatalf("unexpected toolConfig: %+v", wire.ToolConfig)
	}
	// Google native tool normalized to googleSearch; unsupported tool warned
	if !strings.Contains(string(wire.Tools), `"googleSearch"`) {
		t.Fatalf("expected googleSearch passed through, got %s", string(wire.Tools))
	}
	// Verify warnings for remote_image_url, unsupported_tool, truncated_stop_sequences
	warnCodes := map[string]bool{}
	for _, w := range ctx.Warnings {
		warnCodes[w.Code] = true
	}
	for _, want := range []string{"remote_image_url", "unsupported_tool", "truncated_stop_sequences"} {
		if !warnCodes[want] {
			t.Fatalf("expected warning %q, got warnings: %+v", want, ctx.Warnings)
		}
	}
}

func TestResponseDecodeEncodeAndUsageMetadata(t *testing.T) {
	c := New()
	rawResp := []byte(`{
		"responseId": "resp_gem_1",
		"modelVersion": "gemini-3-pro",
		"candidates": [
			{
				"index": 0,
				"content": {
					"role": "model",
					"parts": [
						{"text": "Thinking deeply...", "thought": true, "thoughtSignature": "sig_think_99"},
						{"text": "Let me look that up."},
						{"functionCall": {"id": "call_gem_1", "name": "lookup", "args": {"id": 7}}, "thoughtSignature": "sig_fc_99"},
						{"inlineData": {"mimeType": "image/png", "data": "iVBORw0="}}
					]
				},
				"finishReason": "STOP"
			}
		],
		"usageMetadata": {
			"promptTokenCount": 120,
			"toolUsePromptTokenCount": 30,
			"candidatesTokenCount": 50,
			"thoughtsTokenCount": 40,
			"cachedContentTokenCount": 60,
			"totalTokenCount": 240,
			"promptTokensDetails": [
				{"modality": "TEXT", "tokenCount": 100},
				{"modality": "IMAGE", "tokenCount": 15},
				{"modality": "AUDIO", "tokenCount": 5}
			],
			"candidatesTokensDetails": [
				{"modality": "TEXT", "tokenCount": 45},
				{"modality": "IMAGE", "tokenCount": 5}
			]
		}
	}`)

	ctx := ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI)
	resp, err := c.DecodeResponse(rawResp, ctx)
	if err != nil {
		t.Fatalf("DecodeResponse failed: %v", err)
	}
	if resp.ID != "resp_gem_1" || resp.Model != "gemini-3-pro" {
		t.Fatalf("unexpected envelope: id=%q model=%q", resp.ID, resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	ch := resp.Choices[0]
	// STOP upgraded to FinishToolCalls because a functionCall part is present
	if ch.FinishReason != ir.FinishToolCalls {
		t.Fatalf("expected FinishToolCalls, got %q", ch.FinishReason)
	}
	if len(ch.Message.Parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(ch.Message.Parts))
	}
	if ch.Message.Parts[0].Type != ir.PartReasoning || ch.Message.Parts[0].Reasoning.Signature != "sig_think_99" {
		t.Fatalf("unexpected reasoning part: %+v", ch.Message.Parts[0])
	}
	if ch.Message.Parts[2].Type != ir.PartToolCall || ch.Message.Parts[2].ToolCall.ThoughtSignature != "sig_fc_99" {
		t.Fatalf("unexpected toolCall part: %+v", ch.Message.Parts[2])
	}
	if ch.Message.Parts[3].Type != ir.PartImage || ch.Message.Parts[3].Image.Data != "iVBORw0=" {
		t.Fatalf("unexpected image part: %+v", ch.Message.Parts[3])
	}

	// Verify BridgeState captured tool call thoughtSignature
	bridgeEntry, ok := ctx.BridgeState.ToolCalls["call_gem_1"]
	if !ok || bridgeEntry.ThoughtSignature != "sig_fc_99" || bridgeEntry.OriginFormat != ir.FormatGoogleGenAI {
		t.Fatalf("unexpected BridgeState entry: %+v", bridgeEntry)
	}

	// Verify exact usageMetadata mapping
	u := resp.Usage
	if u == nil {
		t.Fatalf("expected non-nil Usage")
	}
	if u.InputTokens != 150 || u.OutputTokens != 90 || u.TotalTokens != 240 ||
		u.ReasoningTokens != 40 || u.CachedReadTokens != 60 || u.InputExclusive {
		t.Fatalf("unexpected usage totals: %+v", u)
	}
	if u.TextInputTokens != 100 || u.ImageInputTokens != 15 || u.AudioInputTokens != 5 ||
		u.TextOutputTokens != 45 || u.ImageOutputTokens != 5 {
		t.Fatalf("unexpected modality breakdown: %+v", u)
	}

	// Round-trip EncodeResponse
	encoded, err := c.EncodeResponse(resp, ctx)
	if err != nil {
		t.Fatalf("EncodeResponse failed: %v", err)
	}
	var wire googleResponse
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal encoded response: %v", err)
	}
	if wire.ResponseID != "resp_gem_1" || wire.ModelVersion != "gemini-3-pro" {
		t.Fatalf("unexpected wire envelope: %+v", wire)
	}
	if wire.Candidates[0].FinishReason != "STOP" {
		t.Fatalf("expected STOP finishReason, got %q", wire.Candidates[0].FinishReason)
	}
	// candidatesTokenCount = OutputTokens(90) - ReasoningTokens(40) = 50
	if wire.UsageMetadata.PromptTokenCount != 150 ||
		wire.UsageMetadata.CandidatesTokenCount != 50 ||
		wire.UsageMetadata.ThoughtsTokenCount != 40 ||
		wire.UsageMetadata.CachedContentTokenCount != 60 ||
		wire.UsageMetadata.TotalTokenCount != 240 {
		t.Fatalf("unexpected encoded usageMetadata: %+v", wire.UsageMetadata)
	}

	// Also verify safety/length finish reasons and Anthropic-exclusive usage encoding!
	safetyRaw := []byte(`{"candidates":[{"index":0,"content":{"role":"model","parts":[]},"finishReason":"PROHIBITED_CONTENT"}]}`)
	safetyResp, err := c.DecodeResponse(safetyRaw, ctx)
	if err != nil || safetyResp.Choices[0].FinishReason != ir.FinishContentFilter {
		t.Fatalf("expected FinishContentFilter, got resp=%+v err=%v", safetyResp, err)
	}
	anthResp := &ir.Response{
		ID:    "msg_1",
		Model: "claude-sonnet-4-5",
		Choices: []ir.Choice{
			{Index: 0, Message: ir.Message{Role: ir.RoleAssistant, Parts: []ir.ContentPart{{Type: ir.PartText, Text: "ok"}}}, FinishReason: ir.FinishLength},
		},
		Usage: &ir.Usage{
			InputTokens:         100,
			CachedReadTokens:    40,
			CacheCreationTokens: 20,
			OutputTokens:        30,
			InputExclusive:      true,
		},
	}
	encodedAnth, err := c.EncodeResponse(anthResp, ir.NewConversionContext(ir.FormatAnthropic, ir.FormatGoogleGenAI))
	if err != nil {
		t.Fatalf("EncodeResponse anthropic->google failed: %v", err)
	}
	var wireAnth googleResponse
	if err := json.Unmarshal(encodedAnth, &wireAnth); err != nil {
		t.Fatalf("unmarshal wireAnth: %v", err)
	}
	if wireAnth.Candidates[0].FinishReason != "MAX_TOKENS" {
		t.Fatalf("expected MAX_TOKENS, got %q", wireAnth.Candidates[0].FinishReason)
	}
	if wireAnth.UsageMetadata.PromptTokenCount != 160 || wireAnth.UsageMetadata.CandidatesTokenCount != 30 {
		t.Fatalf("expected inclusive promptTokenCount=160 candidatesTokenCount=30, got %+v", wireAnth.UsageMetadata)
	}
}

func TestStreamDecodeAndBridgeState(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatGoogleGenAI, ir.FormatOpenAIChat)

	chunks := []string{
		`{"responseId":"r_stream_1","modelVersion":"gemini-3-pro","candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Step 1...","thought":true,"thoughtSignature":"sig_s1"}]}}]}`,
		`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Calling tool now."}]}}]}`,
		`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"id":"call_s_1","name":"run_sql","args":{"sql":"SELECT 1"}},"thoughtSignature":"sig_fc_stream"}]}}]}`,
		`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"YWJj"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":25,"thoughtsTokenCount":10,"totalTokenCount":85}}`,
	}

	var allEvents []ir.StreamEvent
	for _, chunk := range chunks {
		evs, err := c.DecodeStreamEvent("", []byte(chunk), ctx)
		if err != nil {
			t.Fatalf("DecodeStreamEvent failed: %v", err)
		}
		allEvents = append(allEvents, evs...)
	}

	// Verify event sequence contains stream_start, reasoning_delta, reasoning_signature_delta, text_delta, tool_call_start/delta/end, image markdown, usage, finish(tool_calls)
	if len(allEvents) == 0 || allEvents[0].Type != ir.EventStreamStart || allEvents[0].ResponseID != "r_stream_1" {
		t.Fatalf("expected first event EventStreamStart with r_stream_1, got %+v", allEvents)
	}

	var sawReason, sawSig, sawText, sawTCStart, sawTCDelta, sawTCEnd, sawImg, sawUsage, sawFinishTool bool
	for _, ev := range allEvents {
		switch ev.Type {
		case ir.EventReasoningDelta:
			if ev.Reasoning == "Step 1..." && ev.OriginFormat == ir.FormatGoogleGenAI {
				sawReason = true
			}
		case ir.EventReasoningSignatureDelta:
			if ev.Signature == "sig_s1" {
				sawSig = true
			}
		case ir.EventTextDelta:
			if ev.Text == "Calling tool now." {
				sawText = true
			}
		case ir.EventImage:
			if ev.Image != nil && ev.Image.MediaType == "image/png" && ev.Image.Data == "YWJj" {
				sawImg = true
			}
		case ir.EventToolCallStart:
			if ev.ToolCallID == "call_s_1" && ev.ToolName == "run_sql" && ev.ThoughtSignature == "sig_fc_stream" {
				sawTCStart = true
			}
		case ir.EventToolCallDelta:
			if ev.ToolArguments == `{"sql":"SELECT 1"}` {
				sawTCDelta = true
			}
		case ir.EventContentBlockEnd:
			if ev.BlockType == ir.PartToolCall && ev.ToolCallID == "call_s_1" {
				sawTCEnd = true
			}
		case ir.EventUsage:
			if ev.Usage != nil && ev.Usage.InputTokens == 50 && ev.Usage.OutputTokens == 35 && ev.Usage.ReasoningTokens == 10 {
				sawUsage = true
			}
		case ir.EventFinish:
			if ev.FinishReason == ir.FinishToolCalls {
				sawFinishTool = true
			}
		}
	}
	if !sawReason || !sawSig || !sawText || !sawTCStart || !sawTCDelta || !sawTCEnd || !sawImg || !sawUsage || !sawFinishTool {
		t.Fatalf("missing expected stream events: reason=%v sig=%v text=%v tcStart=%v tcDelta=%v tcEnd=%v img=%v usage=%v finishTool=%v",
			sawReason, sawSig, sawText, sawTCStart, sawTCDelta, sawTCEnd, sawImg, sawUsage, sawFinishTool)
	}

	// Verify BridgeState recorded Google origin and thoughtSignature
	entry, ok := ctx.BridgeState.ToolCalls["call_s_1"]
	if !ok || entry.ThoughtSignature != "sig_fc_stream" || entry.OriginFormat != ir.FormatGoogleGenAI {
		t.Fatalf("expected BridgeState entry with Google origin + sig_fc_stream, got %+v", entry)
	}

	// Embedded error chunk
	errEvs, err := c.DecodeStreamEvent("", []byte(`{"error":{"code":429,"message":"quota exceeded","status":"RESOURCE_EXHAUSTED"}}`), ctx)
	if err != nil || len(errEvs) != 1 || errEvs[0].Type != ir.EventError || errEvs[0].ErrorCode != "429" {
		t.Fatalf("expected EventError, got evs=%+v err=%v", errEvs, err)
	}
}

func TestStreamEncodeBufferedToolCallsAndFlush(t *testing.T) {
	c := New()
	ctx := ir.NewStreamContext(ir.FormatOpenAIChat, ir.FormatGoogleGenAI)

	// 1. StreamStart emits no frame
	frames, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type:       ir.EventStreamStart,
		ResponseID: "chatcmpl_1",
		Model:      "gpt-5",
	}, ctx)
	if err != nil || len(frames) != 0 {
		t.Fatalf("expected 0 frames on stream_start, got %d err=%v", len(frames), err)
	}

	// 2. Reasoning + Text deltas emit immediately with NO SSE event: header
	frames, err = c.EncodeStreamEvent(ir.StreamEvent{Type: ir.EventReasoningDelta, Reasoning: "hmm"}, ctx)
	if err != nil || len(frames) != 1 || frames[0].Event != "" {
		t.Fatalf("unexpected reasoning frame: %+v err=%v", frames, err)
	}
	var chunk googleResponse
	if err := json.Unmarshal(frames[0].Data, &chunk); err != nil {
		t.Fatalf("unmarshal reasoning chunk: %v", err)
	}
	if !chunk.Candidates[0].Content.Parts[0].Thought || *chunk.Candidates[0].Content.Parts[0].Text != "hmm" {
		t.Fatalf("unexpected reasoning chunk content: %+v", chunk)
	}

	frames, err = c.EncodeStreamEvent(ir.StreamEvent{Type: ir.EventTextDelta, Text: "Hello "}, ctx)
	if err != nil || len(frames) != 1 || frames[0].Event != "" {
		t.Fatalf("unexpected text frame: %+v err=%v", frames, err)
	}

	// 3. Tool call start + split deltas MUST BE BUFFERED (0 frames emitted mid-arguments!)
	for _, ev := range []ir.StreamEvent{
		{
			Type:             ir.EventToolCallStart,
			BlockIndex:       1,
			ToolCallIndex:    0,
			ToolCallID:       "call_buf_1",
			ToolName:         "get_weather",
			ThoughtSignature: "foreign_sig_should_drop",
			OriginFormat:     ir.FormatOpenAIChat,
		},
		{Type: ir.EventToolCallDelta, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "call_buf_1", ToolArguments: `{"city":`},
		{Type: ir.EventToolCallDelta, BlockIndex: 1, ToolCallIndex: 0, ToolCallID: "call_buf_1", ToolArguments: `"Berlin"}`},
	} {
		f, err := c.EncodeStreamEvent(ev, ctx)
		if err != nil {
			t.Fatalf("EncodeStreamEvent(%s) failed: %v", ev.Type, err)
		}
		if len(f) != 0 {
			t.Fatalf("expected buffered tool call to emit 0 frames during %s, got %d", ev.Type, len(f))
		}
	}

	// 4. EventContentBlockEnd flushes complete functionCall in a single part (without foreign signature)
	frames, err = c.EncodeStreamEvent(ir.StreamEvent{
		Type:       ir.EventContentBlockEnd,
		BlockIndex: 1,
		BlockType:  ir.PartToolCall,
		ToolCallID: "call_buf_1",
	}, ctx)
	if err != nil || len(frames) != 1 || frames[0].Event != "" {
		t.Fatalf("expected 1 flushed functionCall frame, got %d err=%v", len(frames), err)
	}
	var fcChunk googleResponse
	if err := json.Unmarshal(frames[0].Data, &fcChunk); err != nil {
		t.Fatalf("unmarshal fc chunk: %v", err)
	}
	fcPart := fcChunk.Candidates[0].Content.Parts[0]
	if fcPart.FunctionCall == nil || fcPart.FunctionCall.ID != "call_buf_1" ||
		fcPart.FunctionCall.Name != "get_weather" ||
		string(fcPart.FunctionCall.Args) != `{"city":"Berlin"}` {
		t.Fatalf("unexpected flushed functionCall: %+v", fcPart.FunctionCall)
	}
	if fcPart.ThoughtSignature != "" {
		t.Fatalf("expected foreign thoughtSignature dropped, got %q", fcPart.ThoughtSignature)
	}

	// 5. Parallel OpenAI-style tool calls without EventContentBlockEnd (only ToolCallIndex differs)
	for _, ev := range []ir.StreamEvent{
		{Type: ir.EventToolCallStart, ToolCallIndex: 1, ToolCallID: "call_par_1", ToolName: "fn_a", ThoughtSignature: "sig_same_google", OriginFormat: ir.FormatGoogleGenAI},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 1, ToolArguments: `{"a":`},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 1, ToolArguments: `1}`},
		{Type: ir.EventToolCallStart, ToolCallIndex: 2, ToolCallID: "call_par_2", ToolName: "fn_b"},
		{Type: ir.EventToolCallDelta, ToolCallIndex: 2, ToolArguments: `{"b":2}`},
	} {
		f, err := c.EncodeStreamEvent(ev, ctx)
		if err != nil || len(f) != 0 {
			t.Fatalf("expected parallel tool calls buffered until finish/flush, got %d err=%v", len(f), err)
		}
	}

	// 6. EventFinish flushes the two buffered parallel tool calls; usage arrives afterwards (OpenAI order!)
	finishFrames, err := c.EncodeStreamEvent(ir.StreamEvent{Type: ir.EventFinish, FinishReason: ir.FinishToolCalls}, ctx)
	if err != nil || len(finishFrames) != 2 {
		t.Fatalf("expected 2 flushed tool call frames on EventFinish, got %d err=%v", len(finishFrames), err)
	}
	var par1, par2 googleResponse
	_ = json.Unmarshal(finishFrames[0].Data, &par1)
	_ = json.Unmarshal(finishFrames[1].Data, &par2)
	if par1.Candidates[0].Content.Parts[0].FunctionCall.Name != "fn_a" ||
		string(par1.Candidates[0].Content.Parts[0].FunctionCall.Args) != `{"a":1}` ||
		par1.Candidates[0].Content.Parts[0].ThoughtSignature != "sig_same_google" {
		t.Fatalf("unexpected par1: %+v", par1.Candidates[0].Content.Parts[0])
	}
	if par2.Candidates[0].Content.Parts[0].FunctionCall.Name != "fn_b" ||
		string(par2.Candidates[0].Content.Parts[0].FunctionCall.Args) != `{"b":2}` {
		t.Fatalf("unexpected par2: %+v", par2.Candidates[0].Content.Parts[0])
	}

	// Usage arrives after Finish
	usageFrames, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type: ir.EventUsage,
		Usage: &ir.Usage{
			InputTokens:     80,
			OutputTokens:    45,
			ReasoningTokens: 15,
			TotalTokens:     125,
		},
	}, ctx)
	if err != nil || len(usageFrames) != 0 {
		t.Fatalf("expected usage buffered for final flush chunk, got %d err=%v", len(usageFrames), err)
	}

	// 7. FlushStream emits ONE final chunk with finishReason + usageMetadata, NO event header, NO [DONE]
	tailFrames, err := c.FlushStream(ctx)
	if err != nil || len(tailFrames) != 1 {
		t.Fatalf("expected 1 final chunk from FlushStream, got %d err=%v", len(tailFrames), err)
	}
	if tailFrames[0].Event != "" || string(tailFrames[0].Data) == "[DONE]" {
		t.Fatalf("unexpected SSE event or [DONE] in Gemini stream: %+v", tailFrames[0])
	}
	var finalChunk googleResponse
	if err := json.Unmarshal(tailFrames[0].Data, &finalChunk); err != nil {
		t.Fatalf("unmarshal finalChunk: %v", err)
	}
	if finalChunk.Candidates[0].FinishReason != "STOP" {
		t.Fatalf("expected final finishReason STOP, got %q", finalChunk.Candidates[0].FinishReason)
	}
	if finalChunk.UsageMetadata == nil ||
		finalChunk.UsageMetadata.PromptTokenCount != 80 ||
		finalChunk.UsageMetadata.CandidatesTokenCount != 30 ||
		finalChunk.UsageMetadata.ThoughtsTokenCount != 15 ||
		finalChunk.UsageMetadata.TotalTokenCount != 125 {
		t.Fatalf("unexpected final usageMetadata: %+v", finalChunk.UsageMetadata)
	}

	// 8. Second FlushStream is idempotent
	dupFrames, err := c.FlushStream(ctx)
	if err != nil || len(dupFrames) != 0 {
		t.Fatalf("expected idempotent second FlushStream to emit 0 frames, got %d err=%v", len(dupFrames), err)
	}
}

func TestStreamNextBlockFlushesBufferedToolCallAndPipelineTurnBridge(t *testing.T) {
	c := New()

	// Next block (EventTextDelta) without EventContentBlockEnd flushes buffered tool call first.
	ctx := ir.NewStreamContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI)
	if f, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type:             ir.EventToolCallStart,
		ToolCallIndex:    0,
		ToolCallID:       "call_next_1",
		ToolName:         "ping",
		ThoughtSignature: "sig_google_1",
		OriginFormat:     ir.FormatGoogleGenAI,
	}, ctx); err != nil || len(f) != 0 {
		t.Fatalf("expected buffered start, got %d err=%v", len(f), err)
	}
	if f, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type:          ir.EventToolCallDelta,
		ToolCallIndex: 0,
		ToolArguments: `{"host":"localhost"}`,
	}, ctx); err != nil || len(f) != 0 {
		t.Fatalf("expected buffered delta, got %d err=%v", len(f), err)
	}
	frames, err := c.EncodeStreamEvent(ir.StreamEvent{
		Type: ir.EventTextDelta,
		Text: "Pinging...",
	}, ctx)
	if err != nil || len(frames) != 2 {
		t.Fatalf("expected 2 frames (flushed fc + text), got %d err=%v", len(frames), err)
	}
	var fcResp, txtResp googleResponse
	_ = json.Unmarshal(frames[0].Data, &fcResp)
	_ = json.Unmarshal(frames[1].Data, &txtResp)
	if fcResp.Candidates[0].Content.Parts[0].FunctionCall == nil ||
		fcResp.Candidates[0].Content.Parts[0].FunctionCall.Name != "ping" ||
		fcResp.Candidates[0].Content.Parts[0].ThoughtSignature != "sig_google_1" {
		t.Fatalf("unexpected flushed fc before text: %+v", fcResp.Candidates[0].Content.Parts[0])
	}
	if *txtResp.Candidates[0].Content.Parts[0].Text != "Pinging..." {
		t.Fatalf("unexpected text chunk after fc flush: %+v", txtResp)
	}

	// End-to-end Pipeline + TurnBridge + StreamTranslator + VertexGeminiShim test
	reg := ir.NewRegistry()
	reg.Register(c)
	tb := ir.NewTurnBridge(0, 0)
	pipe, err := ir.NewPipeline(ir.PipelineConfig{
		Registry:     reg,
		Bridge:       tb,
		BridgeScope:  "chan:vertex",
		SourceFormat: ir.FormatGoogleGenAI,
		TargetFormat: ir.FormatGoogleGenAI,
		TargetShim:   ir.VertexGeminiShim,
	})
	if err != nil {
		t.Fatalf("NewPipeline failed: %v", err)
	}

	// Turn 1: stream response emits functionCall with thoughtSignature -> persisted to TurnBridge
	st := pipe.NewStreamTranslator("gemini-3-pro")
	sseInput := "data: {\"responseId\":\"r1\",\"modelVersion\":\"gemini-3-pro\",\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"fc_bridge_1\",\"name\":\"fetch\",\"args\":{\"url\":\"https://a.test\"}},\"thoughtSignature\":\"sig_persisted_1\"}]}}]}\n\n" +
		"data: {\"candidates\":[{\"index\":0,\"content\":{\"role\":\"model\",\"parts\":[]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":10,\"candidatesTokenCount\":5,\"totalTokenCount\":15}}\n\n"

	var emitted []ir.SSEFrame
	if err := st.TranslateReader(strings.NewReader(sseInput), func(frame ir.SSEFrame) error {
		emitted = append(emitted, frame)
		return nil
	}); err != nil {
		t.Fatalf("TranslateReader failed: %v", err)
	}
	if len(emitted) != 2 {
		t.Fatalf("expected 2 emitted SSE frames (fc + final), got %d", len(emitted))
	}
	if st.Usage() == nil || st.Usage().InputTokens != 10 || st.Usage().OutputTokens != 5 {
		t.Fatalf("unexpected stream usage: %+v", st.Usage())
	}

	// Turn 2: client replays history without thoughtSignature -> TurnBridge injects sig_persisted_1 AND VertexGeminiShim strips functionResponse.id!
	turn2Raw := []byte(`{
		"contents": [
			{"role": "user", "parts": [{"text": "Fetch a.test"}]},
			{"role": "model", "parts": [{"functionCall": {"id": "fc_bridge_1", "name": "fetch", "args": {"url": "https://a.test"}}}]},
			{"role": "user", "parts": [{"functionResponse": {"id": "fc_bridge_1", "name": "fetch", "response": {"result": "200 OK"}}}]}
		]
	}`)
	outTurn2, _, _, err := pipe.ConvertRequest(turn2Raw)
	if err != nil {
		t.Fatalf("ConvertRequest turn 2 failed: %v", err)
	}
	if !strings.Contains(string(outTurn2), `"sig_persisted_1"`) {
		t.Fatalf("expected TurnBridge to inject sig_persisted_1, got %s", string(outTurn2))
	}
	var turn2Wire map[string]any
	if err := json.Unmarshal(outTurn2, &turn2Wire); err != nil {
		t.Fatalf("unmarshal turn2Wire: %v", err)
	}
	frPart := turn2Wire["contents"].([]any)[2].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if _, hasID := frPart["id"]; hasID {
		t.Fatalf("expected VertexGeminiShim to strip functionResponse.id, got %v", frPart)
	}
}
