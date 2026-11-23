package google

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/ir"
)

func TestMultiTurnSynthesizedToolIDsNoCollision(t *testing.T) {
	c := New()
	// Two separate model/user turns with id-less functionCall + functionResponse.
	raw := []byte(`{
		"contents": [
			{"role": "user", "parts": [{"text": "Turn 1"}]},
			{"role": "model", "parts": [{"functionCall": {"name": "search", "args": {"q": "a"}}}]},
			{"role": "user", "parts": [{"functionResponse": {"name": "search", "response": {"r": "1"}}}]},
			{"role": "model", "parts": [{"functionCall": {"name": "fetch", "args": {"url": "b"}}}]},
			{"role": "user", "parts": [{"functionResponse": {"name": "fetch", "response": {"r": "2"}}}]}
		]
	}`)
	req, err := c.DecodeRequest(raw, ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatAnthropic))
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	id1 := req.Messages[1].Parts[0].ToolCall.ID
	res1 := req.Messages[2].Parts[0].ToolResult.ToolCallID
	id2 := req.Messages[3].Parts[0].ToolCall.ID
	res2 := req.Messages[4].Parts[0].ToolResult.ToolCallID

	if id1 == "" || id2 == "" || id1 == id2 {
		t.Fatalf("expected unique synthesized IDs across turns, got id1=%q id2=%q", id1, id2)
	}
	if res1 != id1 || res2 != id2 {
		t.Fatalf("expected functionResponse IDs to match preceding turn calls: (%q==%q, %q==%q)", res1, id1, res2, id2)
	}
}

func TestSanitizeGeminiSchemaEdgeCasesAndDAGCap(t *testing.T) {
	// 1. allOf flattening, oneOf -> anyOf, Pydantic nullable anyOf with title, non-string const dropped.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"opt_str": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "null", "title": "Null"},
				},
			},
			"merged": map[string]any{
				"allOf": []any{
					map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string"}}, "required": []any{"a"}},
					map[string]any{"properties": map[string]any{"b": map[string]any{"type": "integer"}}},
				},
			},
			"choice": map[string]any{
				"oneOf": []any{
					map[string]any{"type": "string"},
					map[string]any{"type": "integer"},
				},
			},
			"num_const": map[string]any{
				"const": 42,
			},
			"str_const": map[string]any{
				"const": "fixed",
			},
		},
	}

	rawSchema, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	cleanedRaw := sanitizeGeminiSchema(rawSchema)
	var cleaned map[string]any
	if err := json.Unmarshal(cleanedRaw, &cleaned); err != nil {
		t.Fatalf("unmarshal cleaned schema: %v", err)
	}
	props := cleaned["properties"].(map[string]any)

	optStr := props["opt_str"].(map[string]any)
	if !strings.EqualFold(optStr["type"].(string), "string") || optStr["nullable"] != true {
		t.Fatalf("expected Pydantic nullable anyOf with title unwrapped to string+nullable, got: %+v", optStr)
	}
	if _, hasAnyOf := optStr["anyOf"]; hasAnyOf {
		t.Fatalf("expected anyOf removed after nullable unwrap: %+v", optStr)
	}

	merged := props["merged"].(map[string]any)
	if _, hasAllOf := merged["allOf"]; hasAllOf {
		t.Fatalf("expected allOf flattened, got: %+v", merged)
	}
	mergedProps := merged["properties"].(map[string]any)
	if mergedProps["a"] == nil || mergedProps["b"] == nil {
		t.Fatalf("expected flattened allOf to contain both properties a and b, got: %+v", mergedProps)
	}

	choice := props["choice"].(map[string]any)
	if _, hasOneOf := choice["oneOf"]; hasOneOf {
		t.Fatalf("expected oneOf converted to anyOf, got: %+v", choice)
	}
	if _, hasAnyOf := choice["anyOf"]; !hasAnyOf {
		t.Fatalf("expected anyOf present after oneOf conversion, got: %+v", choice)
	}

	numConst := props["num_const"].(map[string]any)
	if _, hasEnum := numConst["enum"]; hasEnum {
		t.Fatalf("non-string const must NOT be turned into invalid Gemini enum: %+v", numConst)
	}
	strConst := props["str_const"].(map[string]any)
	if enumVal, ok := strConst["enum"].([]any); !ok || len(enumVal) != 1 || enumVal[0] != "fixed" {
		t.Fatalf("string const should become single-value enum, got: %+v", strConst)
	}

	// 2. Exponential DAG $ref expansion must be bounded by node/depth caps (must complete fast without OOM).
	defs := map[string]any{
		"leaf": map[string]any{"type": "string"},
	}
	prev := "leaf"
	for i := 0; i < 25; i++ {
		name := "node_" + string(rune('a'+i))
		defs[name] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"left":  map[string]any{"$ref": "#/$defs/" + prev},
				"right": map[string]any{"$ref": "#/$defs/" + prev},
			},
		}
		prev = name
	}
	dagRaw, _ := json.Marshal(map[string]any{
		"$defs": defs,
		"$ref":  "#/$defs/" + prev,
	})
	res := sanitizeGeminiSchema(dagRaw)
	if len(res) == 0 {
		t.Fatalf("expected bounded DAG schema result, got empty")
	}
}

func TestIncludeThoughtsFalseNotDisabledAndValidatedToolChoiceAndUserFirstContents(t *testing.T) {
	c := New()

	// includeThoughts: false must NOT decode as Mode="disabled" / BudgetTokens=0.
	inJSON := []byte(`{
		"contents": [{"role": "user", "parts": [{"text": "hello", "thoughtSignature": "sig_txt"}]}],
		"generationConfig": {"thinkingConfig": {"includeThoughts": false}}
	}`)
	dec, err := c.DecodeRequest(inJSON, ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI))
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if dec.Reasoning == nil || dec.Reasoning.Mode == "disabled" || dec.Reasoning.Display != "omitted" {
		t.Fatalf("expected includeThoughts:false -> Display=omitted (not Mode=disabled), got: %+v", dec.Reasoning)
	}
	if sig, _ := dec.Messages[0].Parts[0].ProviderMetadata["thought_signature"].(string); sig != "sig_txt" {
		t.Fatalf("expected thoughtSignature preserved on text part metadata, got: %+v", dec.Messages[0].Parts[0].ProviderMetadata)
	}

	reEnc, err := c.EncodeRequest(dec, ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatGoogleGenAI))
	if err != nil {
		t.Fatalf("EncodeRequest error: %v", err)
	}
	if strings.Contains(string(reEnc), `"thinkingBudget":0`) {
		t.Fatalf("includeThoughts:false must NOT emit thinkingBudget:0 (breaks Gemini 2.5/3 Pro), got: %s", string(reEnc))
	}
	if !strings.Contains(string(reEnc), `"thoughtSignature":"sig_txt"`) {
		t.Fatalf("expected thoughtSignature round-tripped on text part, got: %s", string(reEnc))
	}

	// AllowedTools with Mode=auto must emit VALIDATED (not AUTO); model-only contents must be prepended with user.
	reqTools := &ir.Request{
		Model: "gemini-2.5-pro",
		Messages: []ir.Message{{
			Role:  ir.RoleAssistant,
			Parts: []ir.ContentPart{{Type: ir.PartText, Text: "assistant preface"}},
		}},
		Tools: []ir.ToolDefinition{{Type: "function", Name: "fn_a"}},
		ToolChoice: &ir.ToolChoice{
			Mode:         "auto",
			AllowedTools: []string{"fn_a"},
		},
	}
	encTools, err := c.EncodeRequest(reqTools, ir.NewConversionContext(ir.FormatOpenAIChat, ir.FormatGoogleGenAI))
	if err != nil {
		t.Fatalf("EncodeRequest tools error: %v", err)
	}
	var wire map[string]any
	_ = json.Unmarshal(encTools, &wire)
	contents := wire["contents"].([]any)
	if contents[0].(map[string]any)["role"] != "user" {
		t.Fatalf("expected contents normalized to start with user role, got: %+v", contents)
	}
	fcc := wire["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
	if fcc["mode"] != "VALIDATED" {
		t.Fatalf("expected VALIDATED mode when allowedFunctionNames is set, got: %+v", fcc)
	}
}

func TestFinishReasonUnspecifiedSkippedAndInlineImageEventAndRareFinishReason(t *testing.T) {
	c := New()
	decCtx := ir.NewStreamContext(ir.FormatGoogleGenAI, ir.FormatOpenAIChat)

	// Intermediate chunk with FINISH_REASON_UNSPECIFIED + inlineData image must NOT emit EventFinish and MUST emit EventImage.
	chunk := []byte(`{
		"candidates": [{
			"index": 0,
			"content": {
				"role": "model",
				"parts": [
					{"text": "Here is your image:"},
					{"inlineData": {"mimeType": "image/png", "data": "AQID"}, "thoughtSignature": "sig_img"}
				]
			},
			"finishReason": "FINISH_REASON_UNSPECIFIED"
		}]
	}`)
	evs, err := c.DecodeStreamEvent("", chunk, decCtx)
	if err != nil {
		t.Fatalf("DecodeStreamEvent error: %v", err)
	}
	foundImage := false
	for _, ev := range evs {
		if ev.Type == ir.EventFinish {
			t.Fatalf("FINISH_REASON_UNSPECIFIED must NOT emit EventFinish, got events: %+v", evs)
		}
		if ev.Type == ir.EventImage && ev.Image != nil && ev.Image.Data == "AQID" && ev.ThoughtSignature == "sig_img" {
			foundImage = true
		}
	}
	if !foundImage {
		t.Fatalf("expected EventImage with inlineData and thoughtSignature, got events: %+v", evs)
	}

	// Rare finishReason IMAGE_SAFETY round-trips cleanly.
	if got := encodeGoogleFinishReason(ir.FinishContentFilter, "IMAGE_SAFETY"); got != "IMAGE_SAFETY" {
		t.Fatalf("expected IMAGE_SAFETY preserved on round-trip, got %q", got)
	}
}

func TestDecodeGoogleUppercaseSchemaTypesNormalizedToJSONSchema(t *testing.T) {
	c := New()
	raw := []byte(`{
		"model": "deepseek-flash",
		"contents": [{"role": "user", "parts": [{"text": "Return JSON"}]}],
		"tools": [{
			"functionDeclarations": [{
				"name": "save_item",
				"description": "Save an item",
				"parameters": {
					"type": "OBJECT",
					"properties": {
						"type": {"type": "STRING"},
						"tags": {"type": "ARRAY", "items": {"type": "STRING"}},
						"count": {"type": "INTEGER"},
						"ratio": {"type": "NUMBER"},
						"nullable_field": {"type": ["STRING", "NULL"]}
					},
					"required": ["type"]
				}
			}]
		}],
		"generationConfig": {
			"responseMimeType": "application/json",
			"responseSchema": {
				"type": "OBJECT",
				"properties": {
					"ok": {"type": "BOOLEAN"},
					"nested": {
						"type": "OBJECT",
						"properties": {
							"items": {"type": "ARRAY", "items": {"type": "INTEGER"}}
						}
					}
				},
				"required": ["ok"]
			}
		}
	}`)

	req, err := c.DecodeRequest(raw, ir.NewConversionContext(ir.FormatGoogleGenAI, ir.FormatOpenAIChat))
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}
	if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_schema" {
		t.Fatalf("expected json_schema ResponseFormat, got %+v", req.ResponseFormat)
	}
	var respSchema map[string]any
	if err := json.Unmarshal(req.ResponseFormat.Schema, &respSchema); err != nil {
		t.Fatalf("unmarshal response schema: %v", err)
	}
	if respSchema["type"] != "object" {
		t.Fatalf("expected responseSchema root type=object, got %v", respSchema["type"])
	}
	props := respSchema["properties"].(map[string]any)
	if props["ok"].(map[string]any)["type"] != "boolean" {
		t.Fatalf("expected ok.type=boolean, got %v", props["ok"])
	}
	nestedItems := props["nested"].(map[string]any)["properties"].(map[string]any)["items"].(map[string]any)
	if nestedItems["type"] != "array" || nestedItems["items"].(map[string]any)["type"] != "integer" {
		t.Fatalf("expected nested array/integer lowercase, got %+v", nestedItems)
	}

	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	var toolSchema map[string]any
	if err := json.Unmarshal(req.Tools[0].Parameters, &toolSchema); err != nil {
		t.Fatalf("unmarshal tool parameters: %v", err)
	}
	if toolSchema["type"] != "object" {
		t.Fatalf("expected tool parameters root type=object, got %v", toolSchema["type"])
	}
	toolProps := toolSchema["properties"].(map[string]any)
	// User property literally named "type" MUST be preserved as a property with lowercase type="string".
	if typeProp, ok := toolProps["type"].(map[string]any); !ok || typeProp["type"] != "string" {
		t.Fatalf("expected user property 'type' preserved with type=string, got %+v", toolProps["type"])
	}
	if toolProps["count"].(map[string]any)["type"] != "integer" || toolProps["ratio"].(map[string]any)["type"] != "number" {
		t.Fatalf("expected integer/number lowercase, got %+v", toolProps)
	}
	nullableTypes := toolProps["nullable_field"].(map[string]any)["type"].([]any)
	if len(nullableTypes) != 2 || nullableTypes[0] != "string" || nullableTypes[1] != "null" {
		t.Fatalf("expected union type [string, null], got %+v", nullableTypes)
	}
}
