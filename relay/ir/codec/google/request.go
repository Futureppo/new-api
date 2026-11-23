package google

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/relay/ir"
)

// --- Request-specific wire types (camelCase + snake_case decode support) ---

type googleRequest struct {
	Model             string                  `json:"model,omitempty"`
	SystemInstruction *googleContent          `json:"systemInstruction,omitempty"`
	Contents          []googleContent         `json:"contents"`
	Tools             json.RawMessage         `json:"tools,omitempty"`
	ToolConfig        *googleToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *googleGenerationConfig `json:"generationConfig,omitempty"`
	SafetySettings    json.RawMessage         `json:"safetySettings,omitempty"`
	CachedContent     string                  `json:"cachedContent,omitempty"`
}

func (r *googleRequest) UnmarshalJSON(data []byte) error {
	type alias googleRequest
	var aux struct {
		alias
		SystemInstructionSnake *googleContent          `json:"system_instruction,omitempty"`
		ToolConfigSnake        *googleToolConfig       `json:"tool_config,omitempty"`
		GenerationConfigSnake  *googleGenerationConfig `json:"generation_config,omitempty"`
		SafetySettingsSnake    json.RawMessage         `json:"safety_settings,omitempty"`
		CachedContentSnake     string                  `json:"cached_content,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = googleRequest(aux.alias)
	if r.SystemInstruction == nil && aux.SystemInstructionSnake != nil {
		r.SystemInstruction = aux.SystemInstructionSnake
	}
	if r.ToolConfig == nil && aux.ToolConfigSnake != nil {
		r.ToolConfig = aux.ToolConfigSnake
	}
	if r.GenerationConfig == nil && aux.GenerationConfigSnake != nil {
		r.GenerationConfig = aux.GenerationConfigSnake
	}
	if isNullOrEmptyRaw(r.SafetySettings) && !isNullOrEmptyRaw(aux.SafetySettingsSnake) {
		r.SafetySettings = aux.SafetySettingsSnake
	}
	if r.CachedContent == "" && aux.CachedContentSnake != "" {
		r.CachedContent = aux.CachedContentSnake
	}
	return nil
}

type googleFunctionDeclaration struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	Parameters           json.RawMessage `json:"parameters,omitempty"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

func (f *googleFunctionDeclaration) UnmarshalJSON(data []byte) error {
	type alias googleFunctionDeclaration
	var aux struct {
		alias
		ParametersJSONSchemaSnake json.RawMessage `json:"parameters_json_schema,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*f = googleFunctionDeclaration(aux.alias)
	if isNullOrEmptyRaw(f.ParametersJSONSchema) && !isNullOrEmptyRaw(aux.ParametersJSONSchemaSnake) {
		f.ParametersJSONSchema = aux.ParametersJSONSchemaSnake
	}
	return nil
}

type googleToolConfig struct {
	FunctionCallingConfig *googleFunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

func (tc *googleToolConfig) UnmarshalJSON(data []byte) error {
	type alias googleToolConfig
	var aux struct {
		alias
		FunctionCallingConfigSnake *googleFunctionCallingConfig `json:"function_calling_config,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*tc = googleToolConfig(aux.alias)
	if tc.FunctionCallingConfig == nil && aux.FunctionCallingConfigSnake != nil {
		tc.FunctionCallingConfig = aux.FunctionCallingConfigSnake
	}
	return nil
}

type googleFunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

func (fcc *googleFunctionCallingConfig) UnmarshalJSON(data []byte) error {
	type alias googleFunctionCallingConfig
	var aux struct {
		alias
		AllowedFunctionNamesSnake []string `json:"allowed_function_names,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*fcc = googleFunctionCallingConfig(aux.alias)
	if len(fcc.AllowedFunctionNames) == 0 && len(aux.AllowedFunctionNamesSnake) > 0 {
		fcc.AllowedFunctionNames = aux.AllowedFunctionNamesSnake
	}
	return nil
}

type googleThinkingConfig struct {
	IncludeThoughts *bool  `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

func (tc *googleThinkingConfig) UnmarshalJSON(data []byte) error {
	type alias googleThinkingConfig
	var aux struct {
		alias
		IncludeThoughtsSnake *bool  `json:"include_thoughts,omitempty"`
		ThinkingBudgetSnake  *int   `json:"thinking_budget,omitempty"`
		ThinkingLevelSnake   string `json:"thinking_level,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*tc = googleThinkingConfig(aux.alias)
	if tc.IncludeThoughts == nil && aux.IncludeThoughtsSnake != nil {
		tc.IncludeThoughts = aux.IncludeThoughtsSnake
	}
	if tc.ThinkingBudget == nil && aux.ThinkingBudgetSnake != nil {
		tc.ThinkingBudget = aux.ThinkingBudgetSnake
	}
	if tc.ThinkingLevel == "" && aux.ThinkingLevelSnake != "" {
		tc.ThinkingLevel = aux.ThinkingLevelSnake
	}
	return nil
}

type googleGenerationConfig struct {
	MaxOutputTokens    *int                  `json:"maxOutputTokens,omitempty"`
	Temperature        *float64              `json:"temperature,omitempty"`
	TopP               *float64              `json:"topP,omitempty"`
	TopK               *int                  `json:"topK,omitempty"`
	StopSequences      []string              `json:"stopSequences,omitempty"`
	CandidateCount     *int                  `json:"candidateCount,omitempty"`
	PresencePenalty    *float64              `json:"presencePenalty,omitempty"`
	FrequencyPenalty   *float64              `json:"frequencyPenalty,omitempty"`
	Seed               *int                  `json:"seed,omitempty"`
	ResponseMimeType   string                `json:"responseMimeType,omitempty"`
	ResponseSchema     json.RawMessage       `json:"responseSchema,omitempty"`
	ResponseJSONSchema json.RawMessage       `json:"responseJsonSchema,omitempty"`
	ResponseModalities []string              `json:"responseModalities,omitempty"`
	ResponseLogprobs   *bool                 `json:"responseLogprobs,omitempty"`
	Logprobs           *int                  `json:"logprobs,omitempty"`
	ThinkingConfig     *googleThinkingConfig `json:"thinkingConfig,omitempty"`
}

func (gc *googleGenerationConfig) UnmarshalJSON(data []byte) error {
	var aux struct {
		MaxOutputTokens      *int                  `json:"maxOutputTokens,omitempty"`
		MaxOutputTokensSnake *int                  `json:"max_output_tokens,omitempty"`
		Temperature          *float64              `json:"temperature,omitempty"`
		TopP                 *float64              `json:"topP,omitempty"`
		TopPSnake            *float64              `json:"top_p,omitempty"`
		TopK                 *float64              `json:"topK,omitempty"`
		TopKSnake            *float64              `json:"top_k,omitempty"`
		StopSequences        []string              `json:"stopSequences,omitempty"`
		StopSequencesSnake   []string              `json:"stop_sequences,omitempty"`
		CandidateCount       *int                  `json:"candidateCount,omitempty"`
		CandidateCountSnake  *int                  `json:"candidate_count,omitempty"`
		PresencePenalty      *float64              `json:"presencePenalty,omitempty"`
		PresencePenaltySnake *float64              `json:"presence_penalty,omitempty"`
		FrequencyPenalty     *float64              `json:"frequencyPenalty,omitempty"`
		FrequencyPenaltySnk  *float64              `json:"frequency_penalty,omitempty"`
		Seed                 *int                  `json:"seed,omitempty"`
		ResponseMimeType     string                `json:"responseMimeType,omitempty"`
		ResponseMimeTypeSnk  string                `json:"response_mime_type,omitempty"`
		ResponseSchema       json.RawMessage       `json:"responseSchema,omitempty"`
		ResponseSchemaSnake  json.RawMessage       `json:"response_schema,omitempty"`
		ResponseJSONSchema   json.RawMessage       `json:"responseJsonSchema,omitempty"`
		ResponseJSONSchSnake json.RawMessage       `json:"response_json_schema,omitempty"`
		ResponseModalities   []string              `json:"responseModalities,omitempty"`
		ResponseModalitiesSn []string              `json:"response_modalities,omitempty"`
		ResponseLogprobs     *bool                 `json:"responseLogprobs,omitempty"`
		ResponseLogprobsSnk  *bool                 `json:"response_logprobs,omitempty"`
		Logprobs             *int                  `json:"logprobs,omitempty"`
		ThinkingConfig       *googleThinkingConfig `json:"thinkingConfig,omitempty"`
		ThinkingConfigSnake  *googleThinkingConfig `json:"thinking_config,omitempty"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	gc.MaxOutputTokens = aux.MaxOutputTokens
	if gc.MaxOutputTokens == nil {
		gc.MaxOutputTokens = aux.MaxOutputTokensSnake
	}
	gc.Temperature = aux.Temperature
	gc.TopP = aux.TopP
	if gc.TopP == nil {
		gc.TopP = aux.TopPSnake
	}
	if aux.TopK != nil {
		gc.TopK = floatToIntPtr(aux.TopK)
	} else if aux.TopKSnake != nil {
		gc.TopK = floatToIntPtr(aux.TopKSnake)
	}
	gc.StopSequences = aux.StopSequences
	if len(gc.StopSequences) == 0 {
		gc.StopSequences = aux.StopSequencesSnake
	}
	gc.CandidateCount = aux.CandidateCount
	if gc.CandidateCount == nil {
		gc.CandidateCount = aux.CandidateCountSnake
	}
	gc.PresencePenalty = aux.PresencePenalty
	if gc.PresencePenalty == nil {
		gc.PresencePenalty = aux.PresencePenaltySnake
	}
	gc.FrequencyPenalty = aux.FrequencyPenalty
	if gc.FrequencyPenalty == nil {
		gc.FrequencyPenalty = aux.FrequencyPenaltySnk
	}
	gc.Seed = aux.Seed
	gc.ResponseMimeType = aux.ResponseMimeType
	if gc.ResponseMimeType == "" {
		gc.ResponseMimeType = aux.ResponseMimeTypeSnk
	}
	gc.ResponseSchema = aux.ResponseSchema
	if isNullOrEmptyRaw(gc.ResponseSchema) {
		gc.ResponseSchema = aux.ResponseSchemaSnake
	}
	gc.ResponseJSONSchema = aux.ResponseJSONSchema
	if isNullOrEmptyRaw(gc.ResponseJSONSchema) {
		gc.ResponseJSONSchema = aux.ResponseJSONSchSnake
	}
	gc.ResponseModalities = aux.ResponseModalities
	if len(gc.ResponseModalities) == 0 {
		gc.ResponseModalities = aux.ResponseModalitiesSn
	}
	gc.ResponseLogprobs = aux.ResponseLogprobs
	if gc.ResponseLogprobs == nil {
		gc.ResponseLogprobs = aux.ResponseLogprobsSnk
	}
	gc.Logprobs = aux.Logprobs
	gc.ThinkingConfig = aux.ThinkingConfig
	if gc.ThinkingConfig == nil {
		gc.ThinkingConfig = aux.ThinkingConfigSnake
	}
	return nil
}

func (gc *googleGenerationConfig) isEmpty() bool {
	if gc == nil {
		return true
	}
	return gc.MaxOutputTokens == nil &&
		gc.Temperature == nil &&
		gc.TopP == nil &&
		gc.TopK == nil &&
		len(gc.StopSequences) == 0 &&
		gc.CandidateCount == nil &&
		gc.PresencePenalty == nil &&
		gc.FrequencyPenalty == nil &&
		gc.Seed == nil &&
		gc.ResponseMimeType == "" &&
		isNullOrEmptyRaw(gc.ResponseSchema) &&
		isNullOrEmptyRaw(gc.ResponseJSONSchema) &&
		len(gc.ResponseModalities) == 0 &&
		gc.ResponseLogprobs == nil &&
		gc.Logprobs == nil &&
		gc.ThinkingConfig == nil
}

// --- DecodeRequest ---

// DecodeRequest parses a Google GenAI generateContent/streamGenerateContent JSON payload into IR.
func (c *Codec) DecodeRequest(raw []byte, ctx *ir.ConversionContext) (*ir.Request, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("google: empty request body")
	}
	var gReq googleRequest
	if err := json.Unmarshal(raw, &gReq); err != nil {
		return nil, fmt.Errorf("google: decode request: %w", err)
	}

	req := &ir.Request{
		Model: gReq.Model,
	}

	// systemInstruction -> req.System
	if gReq.SystemInstruction != nil {
		for pi, p := range gReq.SystemInstruction.Parts {
			if part, ok := decodeBasicPart(p, -1, pi); ok {
				req.System = append(req.System, part)
			}
		}
	}

	// contents[] -> req.Messages with FIFO functionCall ID correlation for unnamed functionResponse parts
	pendingCallsByName := make(map[string][]string)
	lastCallIDByName := make(map[string]string)

	for mi, content := range gReq.Contents {
		baseRole := mapGoogleRole(content.Role)

		// Group consecutive tool-result parts into RoleTool messages while keeping
		// regular parts under baseRole so mixed user+functionResponse contents stay well-formed.
		var curRole ir.Role
		var curParts []ir.ContentPart
		flushMsg := func() {
			if len(curParts) == 0 {
				return
			}
			req.Messages = append(req.Messages, ir.Message{
				Role:  curRole,
				Parts: curParts,
			})
			curParts = nil
		}
		appendPart := func(role ir.Role, part ir.ContentPart) {
			if len(curParts) > 0 && curRole != role {
				flushMsg()
			}
			curRole = role
			curParts = append(curParts, part)
		}

		for pi, p := range content.Parts {
			switch {
			case p.FunctionCall != nil:
				id := p.FunctionCall.ID
				if id == "" {
					id = fmt.Sprintf("call_%d_%d", mi, pi)
				}
				args := normalizeJSONArgs(p.FunctionCall.Args)
				pendingCallsByName[p.FunctionCall.Name] = append(pendingCallsByName[p.FunctionCall.Name], id)
				lastCallIDByName[p.FunctionCall.Name] = id
				appendPart(baseRole, ir.ContentPart{
					Type: ir.PartToolCall,
					ToolCall: &ir.ToolCall{
						ID:               id,
						Name:             p.FunctionCall.Name,
						Arguments:        args,
						Kind:             "function",
						ThoughtSignature: p.ThoughtSignature,
					},
				})

			case p.FunctionResponse != nil:
				id := p.FunctionResponse.ID
				if id != "" {
					removePendingCallID(pendingCallsByName, p.FunctionResponse.Name, id)
				} else if queue := pendingCallsByName[p.FunctionResponse.Name]; len(queue) > 0 {
					id = queue[0]
					pendingCallsByName[p.FunctionResponse.Name] = queue[1:]
				} else if lastID := lastCallIDByName[p.FunctionResponse.Name]; lastID != "" {
					id = lastID
				} else {
					id = fmt.Sprintf("call_%d_%d", mi, pi)
				}
				respText, isErr := extractFunctionResponseContent(p.FunctionResponse.Response)
				var subParts []ir.ContentPart
				for spi, sp := range p.FunctionResponse.Parts {
					if sub, ok := decodeBasicPart(sp, mi, spi); ok {
						subParts = append(subParts, sub)
					}
				}
				appendPart(ir.RoleTool, ir.ContentPart{
					Type: ir.PartToolResult,
					ToolResult: &ir.ToolResult{
						ToolCallID: id,
						Name:       p.FunctionResponse.Name,
						Content:    respText,
						Parts:      subParts,
						IsError:    isErr,
					},
				})

			default:
				if part, ok := decodeBasicPart(p, mi, pi); ok {
					appendPart(baseRole, part)
				}
			}
		}
		flushMsg()
	}

	// tools[]
	if !isNullOrEmptyRaw(gReq.Tools) {
		tools, err := decodeGoogleTools(gReq.Tools)
		if err != nil {
			return nil, fmt.Errorf("google: decode tools: %w", err)
		}
		req.Tools = tools
	}

	// toolConfig
	if gReq.ToolConfig != nil && gReq.ToolConfig.FunctionCallingConfig != nil {
		req.ToolChoice = decodeGoogleToolConfig(gReq.ToolConfig.FunctionCallingConfig)
	}

	// generationConfig
	if gReq.GenerationConfig != nil {
		decodeGoogleGenerationConfig(gReq.GenerationConfig, req)
	}

	// Preserve Google-native optional request extensions for same-format roundtrips
	if !isNullOrEmptyRaw(gReq.SafetySettings) {
		if req.ProviderExtensions == nil {
			req.ProviderExtensions = make(map[string]json.RawMessage)
		}
		req.ProviderExtensions["google_safety_settings"] = bytes.Clone(gReq.SafetySettings)
	}
	if gReq.CachedContent != "" {
		if req.ProviderExtensions == nil {
			req.ProviderExtensions = make(map[string]json.RawMessage)
		}
		if b, err := json.Marshal(gReq.CachedContent); err == nil {
			req.ProviderExtensions["google_cached_content"] = b
		}
	}

	return req, nil
}

func mapGoogleRole(role string) ir.Role {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "", "user":
		return ir.RoleUser
	case "model", "assistant":
		return ir.RoleAssistant
	case "system":
		return ir.RoleSystem
	case "function", "tool":
		return ir.RoleTool
	default:
		return ir.RoleUser
	}
}

func removePendingCallID(pending map[string][]string, name, id string) {
	queue := pending[name]
	for i, qid := range queue {
		if qid == id {
			pending[name] = append(queue[:i], queue[i+1:]...)
			return
		}
	}
}

// decodeBasicPart decodes text, thought, inlineData, fileData, and code-execution parts.
func decodeBasicPart(p googlePart, _, _ int) (ir.ContentPart, bool) {
	if p.Thought {
		text := ""
		if p.Text != nil {
			text = *p.Text
		}
		return ir.ContentPart{
			Type: ir.PartReasoning,
			Reasoning: &ir.Reasoning{
				Text:         text,
				Signature:    p.ThoughtSignature,
				OriginFormat: ir.FormatGoogleGenAI,
			},
		}, true
	}
	if p.InlineData != nil && (p.InlineData.Data != "" || p.InlineData.MimeType != "") {
		mime := p.InlineData.MimeType
		lower := strings.ToLower(mime)
		switch {
		case strings.HasPrefix(lower, "image/"):
			return ir.ContentPart{
				Type: ir.PartImage,
				Image: &ir.ImageSource{
					Data:      p.InlineData.Data,
					MediaType: mime,
				},
			}, true
		case strings.HasPrefix(lower, "audio/"):
			return ir.ContentPart{
				Type: ir.PartAudio,
				Audio: &ir.AudioSource{
					Data:      p.InlineData.Data,
					MediaType: mime,
					Format:    audioFormatFromMime(mime),
				},
			}, true
		default:
			return ir.ContentPart{
				Type: ir.PartFile,
				File: &ir.FileSource{
					Data:      p.InlineData.Data,
					MediaType: mime,
				},
			}, true
		}
	}
	if p.FileData != nil && (p.FileData.FileURI != "" || p.FileData.MimeType != "") {
		mime := p.FileData.MimeType
		if strings.HasPrefix(strings.ToLower(mime), "image/") {
			return ir.ContentPart{
				Type: ir.PartImage,
				Image: &ir.ImageSource{
					URL:       p.FileData.FileURI,
					MediaType: mime,
				},
			}, true
		}
		return ir.ContentPart{
			Type: ir.PartFile,
			File: &ir.FileSource{
				URL:       p.FileData.FileURI,
				MediaType: mime,
			},
		}, true
	}
	if p.ExecutableCode != nil {
		return ir.ContentPart{
			Type: ir.PartText,
			Text: formatExecutableCodeText(p.ExecutableCode),
		}, true
	}
	if p.CodeExecutionResult != nil {
		return ir.ContentPart{
			Type: ir.PartText,
			Text: formatCodeExecutionResultText(p.CodeExecutionResult),
		}, true
	}
	if p.Text != nil {
		part := ir.ContentPart{
			Type: ir.PartText,
			Text: *p.Text,
		}
		if p.ThoughtSignature != "" {
			part.ProviderMetadata = map[string]any{"thought_signature": p.ThoughtSignature}
		}
		return part, true
	}
	if p.ThoughtSignature != "" {
		return ir.ContentPart{
			Type: ir.PartReasoning,
			Reasoning: &ir.Reasoning{
				Signature:    p.ThoughtSignature,
				OriginFormat: ir.FormatGoogleGenAI,
			},
		}, true
	}
	return ir.ContentPart{}, false
}

func extractFunctionResponseContent(raw json.RawMessage) (string, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s, false
		}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err == nil && len(obj) == 1 {
		for k, v := range obj {
			switch k {
			case "result", "content", "output":
				return unwrapSingleFieldValue(v), false
			case "error":
				return unwrapSingleFieldValue(v), true
			}
		}
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, trimmed); err == nil {
		return compacted.String(), false
	}
	return string(trimmed), false
}

func unwrapSingleFieldValue(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err == nil {
			return s
		}
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, trimmed); err == nil {
		return compacted.String()
	}
	return string(trimmed)
}

func decodeGoogleTools(raw json.RawMessage) ([]ir.ToolDefinition, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var toolObjects []map[string]json.RawMessage
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &toolObjects); err != nil {
			return nil, err
		}
	} else if trimmed[0] == '{' {
		var single map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return nil, err
		}
		toolObjects = []map[string]json.RawMessage{single}
	} else {
		return nil, fmt.Errorf("unexpected tools JSON token %q", string(trimmed[:1]))
	}

	var defs []ir.ToolDefinition
	for _, obj := range toolObjects {
		if len(obj) == 0 {
			continue
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			val := obj[k]
			if k == "functionDeclarations" || k == "function_declarations" {
				if isNullOrEmptyRaw(val) {
					continue
				}
				var decls []googleFunctionDeclaration
				if err := json.Unmarshal(val, &decls); err != nil {
					return nil, err
				}
				for _, d := range decls {
					params := d.Parameters
					if isNullOrEmptyRaw(params) {
						params = d.ParametersJSONSchema
					}
					if !isNullOrEmptyRaw(params) {
						params = ir.NormalizeJSONSchemaTypes(params)
					}
					defs = append(defs, ir.ToolDefinition{
						Type:        "function",
						Name:        d.Name,
						Description: d.Description,
						Parameters:  params,
					})
				}
				continue
			}

			// Built-in / Google-native tool (googleSearch, google_search, codeExecution, urlContext, etc.)
			singleObj, err := json.Marshal(map[string]json.RawMessage{k: val})
			if err != nil {
				return nil, err
			}
			defs = append(defs, ir.ToolDefinition{
				Type:         canonicalGoogleNativeToolKey(k),
				Name:         k,
				NativeRaw:    singleObj,
				OriginFormat: ir.FormatGoogleGenAI,
			})
		}
	}
	return defs, nil
}

func canonicalGoogleNativeToolKey(key string) string {
	switch key {
	case "googleSearch", "google_search":
		return "googleSearch"
	case "googleSearchRetrieval", "google_search_retrieval":
		return "googleSearchRetrieval"
	case "codeExecution", "code_execution":
		return "codeExecution"
	case "urlContext", "url_context":
		return "urlContext"
	default:
		return key
	}
}

func decodeGoogleToolConfig(fcc *googleFunctionCallingConfig) *ir.ToolChoice {
	if fcc == nil {
		return nil
	}
	upperMode := strings.ToUpper(strings.TrimSpace(fcc.Mode))
	if upperMode == "ANY" && len(fcc.AllowedFunctionNames) == 1 {
		return &ir.ToolChoice{
			Mode:     "tool",
			ToolName: fcc.AllowedFunctionNames[0],
		}
	}

	var mode string
	switch upperMode {
	case "AUTO", "VALIDATED":
		mode = "auto"
	case "NONE":
		mode = "none"
	case "ANY":
		mode = "required"
	default:
		if len(fcc.AllowedFunctionNames) > 0 {
			mode = "auto"
		}
	}
	if mode == "" && len(fcc.AllowedFunctionNames) == 0 {
		return nil
	}
	return &ir.ToolChoice{
		Mode:         mode,
		AllowedTools: fcc.AllowedFunctionNames,
	}
}

func decodeGoogleGenerationConfig(gc *googleGenerationConfig, req *ir.Request) {
	req.Generation.MaxTokens = gc.MaxOutputTokens
	req.Generation.Temperature = gc.Temperature
	req.Generation.TopP = gc.TopP
	req.Generation.TopK = gc.TopK
	req.Generation.StopSequences = gc.StopSequences
	req.Generation.N = gc.CandidateCount
	req.Generation.PresencePenalty = gc.PresencePenalty
	req.Generation.FrequencyPenalty = gc.FrequencyPenalty
	req.Generation.Seed = gc.Seed
	req.Generation.Modalities = gc.ResponseModalities
	req.Generation.LogProbs = gc.ResponseLogprobs
	req.Generation.TopLogProbs = gc.Logprobs

	// responseMimeType / responseSchema / responseJsonSchema
	schema := gc.ResponseSchema
	if isNullOrEmptyRaw(schema) {
		schema = gc.ResponseJSONSchema
	}
	if !isNullOrEmptyRaw(schema) {
		req.ResponseFormat = &ir.ResponseFormat{
			Type:   "json_schema",
			Schema: ir.NormalizeJSONSchemaTypes(schema),
		}
	} else if strings.EqualFold(gc.ResponseMimeType, "application/json") {
		req.ResponseFormat = &ir.ResponseFormat{
			Type: "json_object",
		}
	} else if strings.EqualFold(gc.ResponseMimeType, "text/plain") {
		req.ResponseFormat = &ir.ResponseFormat{
			Type: "text",
		}
	}

	// thinkingConfig -> ReasoningConfig
	if gc.ThinkingConfig != nil {
		tc := gc.ThinkingConfig
		rc := &ir.ReasoningConfig{
			BudgetTokens: tc.ThinkingBudget,
			Effort:       strings.ToLower(strings.TrimSpace(tc.ThinkingLevel)),
		}
		switch {
		case tc.ThinkingBudget != nil && *tc.ThinkingBudget == 0:
			rc.Enabled = boolPtr(false)
			rc.Mode = "disabled"
		case strings.EqualFold(tc.ThinkingLevel, "NONE"):
			rc.Enabled = boolPtr(false)
			rc.Mode = "disabled"
		case tc.ThinkingBudget != nil && *tc.ThinkingBudget < 0:
			rc.Enabled = boolPtr(true)
			rc.Mode = "adaptive"
		case (tc.ThinkingBudget != nil && *tc.ThinkingBudget > 0) || tc.ThinkingLevel != "":
			rc.Enabled = boolPtr(true)
			rc.Mode = "enabled"
		case tc.IncludeThoughts != nil:
			if *tc.IncludeThoughts {
				rc.Enabled = boolPtr(true)
				rc.Mode = "enabled"
			} else {
				// includeThoughts=false only hides thoughts from output; do NOT synthesize Mode="disabled"
				// (which would emit thinkingBudget=0 and fail on Gemini 2.5 Pro / 3 Pro).
				rc.Display = "omitted"
			}
		}
		if tc.IncludeThoughts != nil && !*tc.IncludeThoughts && rc.Mode != "disabled" {
			rc.Display = "omitted"
		}
		req.Reasoning = rc
	}
}

// --- EncodeRequest ---

// EncodeRequest serializes an IR Request into a canonical Google GenAI generateContent JSON payload.
func (c *Codec) EncodeRequest(req *ir.Request, ctx *ir.ConversionContext) ([]byte, error) {
	if req == nil {
		return nil, errors.New("google: nil IR request")
	}

	// Fallback full-conversation toolNameByID map (used only if a tool_result references an ID
	// not yet seen chronologically); live map updates chronologically so duplicate IDs across turns
	// resolve to their own turn's tool call rather than a later turn's overwrite.
	fallbackToolNameByID := make(map[string]string)
	for _, msg := range req.Messages {
		for _, part := range msg.Parts {
			if part.Type == ir.PartToolCall && part.ToolCall != nil && part.ToolCall.ID != "" && part.ToolCall.Name != "" {
				if _, exists := fallbackToolNameByID[part.ToolCall.ID]; !exists {
					fallbackToolNameByID[part.ToolCall.ID] = part.ToolCall.Name
				}
			}
		}
	}
	toolNameByID := make(map[string]string)

	// Build systemInstruction from req.System and system/developer messages.
	var sysParts []googlePart
	for _, p := range req.System {
		if p.Type == ir.PartText && p.Text != "" {
			sysParts = append(sysParts, googlePart{Text: strPtr(p.Text)})
		}
	}
	for _, msg := range req.Messages {
		if msg.Role != ir.RoleSystem && msg.Role != ir.RoleDeveloper {
			continue
		}
		for _, p := range msg.Parts {
			if p.Type == ir.PartText && p.Text != "" {
				sysParts = append(sysParts, googlePart{Text: strPtr(p.Text)})
			}
		}
	}

	gReq := googleRequest{
		Contents: make([]googleContent, 0, len(req.Messages)),
	}
	if len(sysParts) > 0 {
		gReq.SystemInstruction = &googleContent{Parts: sysParts}
	}

	// Encode messages chronologically and merge adjacent same-role contents to preserve Gemini role alternation.
	for _, msg := range req.Messages {
		if msg.Role == ir.RoleSystem || msg.Role == ir.RoleDeveloper {
			continue
		}
		role := "user"
		if msg.Role == ir.RoleAssistant {
			role = "model"
		}

		// Record assistant tool calls as we encounter them chronologically.
		for _, p := range msg.Parts {
			if p.Type == ir.PartToolCall && p.ToolCall != nil && p.ToolCall.ID != "" && p.ToolCall.Name != "" {
				toolNameByID[p.ToolCall.ID] = p.ToolCall.Name
			}
		}

		parts := make([]googlePart, 0, len(msg.Parts))
		for _, p := range msg.Parts {
			encoded, ok := encodeRequestPart(p, toolNameByID, fallbackToolNameByID, ctx)
			if ok {
				parts = append(parts, encoded)
			}
		}
		if len(parts) == 0 {
			continue
		}

		if n := len(gReq.Contents); n > 0 && gReq.Contents[n-1].Role == role {
			gReq.Contents[n-1].Parts = append(gReq.Contents[n-1].Parts, parts...)
		} else {
			gReq.Contents = append(gReq.Contents, googleContent{
				Role:  role,
				Parts: parts,
			})
		}
	}

	// Gemini requires non-empty contents starting with a "user" turn.
	if len(gReq.Contents) == 0 {
		gReq.Contents = []googleContent{{
			Role:  "user",
			Parts: []googlePart{{Text: strPtr("...")}},
		}}
	} else if gReq.Contents[0].Role != "user" {
		gReq.Contents = append([]googleContent{{
			Role:  "user",
			Parts: []googlePart{{Text: strPtr("...")}},
		}}, gReq.Contents...)
	}

	// Enforce Gemini 3 thoughtSignature requirement on the first functionCall in each model turn.
	attachDummyThoughtSig := ctx.GetBoolOption("attach_thought_signature", true)
	applyGemini3ThoughtSignatures(gReq.Contents, attachDummyThoughtSig)

	// Encode tools
	if len(req.Tools) > 0 {
		toolsJSON, err := encodeGoogleTools(req.Tools, ctx)
		if err != nil {
			return nil, fmt.Errorf("google: encode tools: %w", err)
		}
		gReq.Tools = toolsJSON
	}

	// Encode toolConfig
	if req.ToolChoice != nil {
		gReq.ToolConfig = encodeGoogleToolConfig(req.ToolChoice)
	}

	// Encode generationConfig
	genCfg := encodeGoogleGenerationConfig(req, ctx)
	if !genCfg.isEmpty() {
		gReq.GenerationConfig = genCfg
	}

	// Restore optional Google-native extensions when present
	if len(req.ProviderExtensions) > 0 && (ctx == nil || ctx.ShouldPreserveMetadata(ir.FormatGoogleGenAI)) {
		if rawSafety, ok := req.ProviderExtensions["google_safety_settings"]; ok && !isNullOrEmptyRaw(rawSafety) {
			gReq.SafetySettings = rawSafety
		}
		if rawCached, ok := req.ProviderExtensions["google_cached_content"]; ok && !isNullOrEmptyRaw(rawCached) {
			var cached string
			if err := json.Unmarshal(rawCached, &cached); err == nil {
				gReq.CachedContent = cached
			}
		}
	}

	return json.Marshal(gReq)
}

func encodeRequestPart(p ir.ContentPart, toolNameByID map[string]string, fallbackToolNameByID map[string]string, ctx *ir.ConversionContext) (googlePart, bool) {
	switch p.Type {
	case ir.PartText:
		var sig string
		if p.ProviderMetadata != nil {
			if s, ok := p.ProviderMetadata["thought_signature"].(string); ok && s != "" {
				if ctx == nil || ctx.ShouldPreserveMetadata(ir.FormatGoogleGenAI) {
					sig = s
				}
			}
		}
		if p.Text == "" && sig == "" {
			return googlePart{}, false
		}
		gp := googlePart{ThoughtSignature: sig}
		if p.Text != "" {
			gp.Text = strPtr(p.Text)
		}
		return gp, true

	case ir.PartRefusal:
		if p.Refusal == "" {
			return googlePart{}, false
		}
		return googlePart{Text: strPtr(p.Refusal)}, true

	case ir.PartReasoning:
		if p.Reasoning == nil {
			return googlePart{}, false
		}
		if p.Reasoning.OriginFormat == ir.FormatGoogleGenAI && (p.Reasoning.Text != "" || p.Reasoning.Signature != "") {
			gp := googlePart{
				Thought:          true,
				ThoughtSignature: p.Reasoning.Signature,
			}
			if p.Reasoning.Text != "" {
				gp.Text = strPtr(p.Reasoning.Text)
			}
			return gp, true
		}
		if p.Reasoning.OriginFormat != ir.FormatGoogleGenAI {
			ctx.AddWarning(
				"dropped_cross_provider_reasoning",
				"messages.parts.reasoning",
				"dropped non-Google reasoning block when encoding Google GenAI request",
			)
		}
		return googlePart{}, false

	case ir.PartImage:
		if p.Image == nil {
			return googlePart{}, false
		}
		if p.Image.Data != "" {
			mime := p.Image.MediaType
			if mime == "" {
				mime = "image/png"
			}
			return googlePart{
				InlineData: &googleInlineData{
					MimeType: mime,
					Data:     p.Image.Data,
				},
			}, true
		}
		if p.Image.URL != "" {
			if mime, data, ok := ir.ParseDataURL(p.Image.URL); ok {
				return googlePart{
					InlineData: &googleInlineData{
						MimeType: mime,
						Data:     data,
					},
				}, true
			}
			if !strings.HasPrefix(p.Image.URL, "gs://") && !strings.Contains(p.Image.URL, "generativelanguage.googleapis.com/") {
				ctx.AddWarning(
					"remote_image_url",
					"messages.parts.image.url",
					"non-Google Storage image URL encoded as fileData; upstream may require inline fetching",
				)
			}
			return googlePart{
				FileData: &googleFileData{
					MimeType: p.Image.MediaType,
					FileURI:  p.Image.URL,
				},
			}, true
		}
		return googlePart{}, false

	case ir.PartAudio:
		if p.Audio == nil {
			return googlePart{}, false
		}
		mime := p.Audio.MediaType
		if mime == "" {
			mime = mimeFromAudioFormat(p.Audio.Format)
		}
		if p.Audio.Data != "" {
			return googlePart{
				InlineData: &googleInlineData{
					MimeType: mime,
					Data:     p.Audio.Data,
				},
			}, true
		}
		if p.Audio.URL != "" {
			return googlePart{
				FileData: &googleFileData{
					MimeType: mime,
					FileURI:  p.Audio.URL,
				},
			}, true
		}
		return googlePart{}, false

	case ir.PartFile:
		if p.File == nil {
			return googlePart{}, false
		}
		if p.File.Text != "" && p.File.Data == "" && p.File.URL == "" && p.File.FileID == "" {
			return googlePart{Text: strPtr(p.File.Text)}, true
		}
		if p.File.Data != "" {
			mime := p.File.MediaType
			if mime == "" {
				mime = "application/octet-stream"
			}
			return googlePart{
				InlineData: &googleInlineData{
					MimeType: mime,
					Data:     p.File.Data,
				},
			}, true
		}
		uri := p.File.URL
		if uri == "" {
			uri = p.File.FileID
		}
		if uri != "" {
			return googlePart{
				FileData: &googleFileData{
					MimeType: p.File.MediaType,
					FileURI:  uri,
				},
			}, true
		}
		return googlePart{}, false

	case ir.PartToolCall:
		if p.ToolCall == nil {
			return googlePart{}, false
		}
		return googlePart{
			FunctionCall: &googleFunctionCall{
				ID:   p.ToolCall.ID,
				Name: p.ToolCall.Name,
				Args: parseToolArgsToObject(p.ToolCall.Arguments),
			},
			ThoughtSignature: p.ToolCall.ThoughtSignature,
		}, true

	case ir.PartToolResult:
		if p.ToolResult == nil {
			return googlePart{}, false
		}
		name := resolveToolResultName(p.ToolResult, toolNameByID, fallbackToolNameByID, ctx)
		respObj := buildFunctionResponseObject(p.ToolResult)
		var subParts []googlePart
		for _, sp := range p.ToolResult.Parts {
			if sp.Type == ir.PartText {
				continue
			}
			if encodedSub, ok := encodeRequestPart(sp, toolNameByID, fallbackToolNameByID, ctx); ok {
				subParts = append(subParts, encodedSub)
			}
		}
		return googlePart{
			FunctionResponse: &googleFunctionResponse{
				ID:       p.ToolResult.ToolCallID,
				Name:     name,
				Response: respObj,
				Parts:    subParts,
			},
		}, true

	default:
		return googlePart{}, false
	}
}

func resolveToolResultName(tr *ir.ToolResult, toolNameByID map[string]string, fallbackToolNameByID map[string]string, ctx *ir.ConversionContext) string {
	if tr.Name != "" {
		return tr.Name
	}
	if tr.ToolCallID != "" {
		if name := toolNameByID[tr.ToolCallID]; name != "" {
			return name
		}
		if fallbackToolNameByID != nil {
			if name := fallbackToolNameByID[tr.ToolCallID]; name != "" {
				return name
			}
		}
		if ctx != nil && ctx.BridgeState != nil {
			if entry, ok := ctx.BridgeState.ToolCalls[tr.ToolCallID]; ok && entry.ToolName != "" {
				return entry.ToolName
			}
		}
	}
	ctx.AddWarning(
		"missing_tool_name",
		"messages.parts.tool_result.name",
		"tool result omitted function name and no matching tool call was found; using fallback name",
	)
	return "unknown_function"
}

func buildFunctionResponseObject(tr *ir.ToolResult) json.RawMessage {
	content := tr.Content
	if content == "" && len(tr.Parts) > 0 {
		content = ir.JoinTextParts(tr.Parts, "\n")
	}
	trimmed := strings.TrimSpace(content)
	if trimmed != "" && trimmed[0] == '{' {
		var obj map[string]any
		if err := json.Unmarshal([]byte(trimmed), &obj); err == nil && obj != nil {
			if tr.IsError {
				if _, hasErr := obj["error"]; !hasErr {
					if b, err := json.Marshal(map[string]any{"error": obj}); err == nil {
						return json.RawMessage(b)
					}
				}
			}
			var compacted bytes.Buffer
			if err := json.Compact(&compacted, []byte(trimmed)); err == nil {
				return json.RawMessage(compacted.Bytes())
			}
		}
	}
	key := "result"
	if tr.IsError {
		key = "error"
	}
	b, err := json.Marshal(map[string]any{key: content})
	if err != nil {
		return json.RawMessage(`{"result":""}`)
	}
	return json.RawMessage(b)
}

// applyGemini3ThoughtSignatures guarantees that the first functionCall part in every
// model turn carries a thoughtSignature — preserving an existing signature or injecting
// GoogleDummyThoughtSignature when enabled.
func applyGemini3ThoughtSignatures(contents []googleContent, attachDummy bool) {
	if !attachDummy {
		return
	}
	for ci := range contents {
		if contents[ci].Role != "model" {
			continue
		}
		for pi := range contents[ci].Parts {
			if contents[ci].Parts[pi].FunctionCall != nil {
				if contents[ci].Parts[pi].ThoughtSignature == "" {
					contents[ci].Parts[pi].ThoughtSignature = ir.GoogleDummyThoughtSignature
				}
				break
			}
		}
	}
}

func encodeGoogleTools(tools []ir.ToolDefinition, ctx *ir.ConversionContext) (json.RawMessage, error) {
	var funcDecls []googleFunctionDeclaration
	var toolEntries []json.RawMessage

	for _, t := range tools {
		if t.Type == "" || t.Type == "function" {
			decl := googleFunctionDeclaration{
				Name:        t.Name,
				Description: t.Description,
			}
			if !isNullOrEmptyRaw(t.Parameters) {
				decl.Parameters = sanitizeGeminiSchema(t.Parameters)
			}
			funcDecls = append(funcDecls, decl)
			continue
		}

		// Google native built-in tools
		canonicalKey := canonicalGoogleNativeToolKey(t.Type)
		isKnownGoogleNative := canonicalKey == "googleSearch" ||
			canonicalKey == "googleSearchRetrieval" ||
			canonicalKey == "codeExecution" ||
			canonicalKey == "urlContext"

		if t.OriginFormat == ir.FormatGoogleGenAI && !isNullOrEmptyRaw(t.NativeRaw) {
			normalized, err := normalizeGoogleNativeToolRaw(t.NativeRaw)
			if err == nil {
				toolEntries = append(toolEntries, normalized)
				continue
			}
		}
		if isKnownGoogleNative {
			entry, err := json.Marshal(map[string]any{canonicalKey: map[string]any{}})
			if err == nil {
				toolEntries = append(toolEntries, entry)
			}
			continue
		}

		ctx.AddWarning(
			"unsupported_tool",
			"tools",
			fmt.Sprintf("skipped non-Google native tool type %q when encoding Google GenAI request", t.Type),
		)
	}

	var result []json.RawMessage
	if len(funcDecls) > 0 {
		wrapper, err := json.Marshal(map[string]any{
			"functionDeclarations": funcDecls,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, wrapper)
	}
	result = append(result, toolEntries...)

	if len(result) == 0 {
		return nil, nil
	}
	return json.Marshal(result)
}

func normalizeGoogleNativeToolRaw(raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(obj))
	for k, v := range obj {
		out[canonicalGoogleNativeToolKey(k)] = v
	}
	return json.Marshal(out)
}

func encodeGoogleToolConfig(tc *ir.ToolChoice) *googleToolConfig {
	if tc == nil {
		return nil
	}
	fcc := &googleFunctionCallingConfig{}
	switch strings.ToLower(strings.TrimSpace(tc.Mode)) {
	case "auto":
		// Gemini rejects allowedFunctionNames when mode == "AUTO"; use "VALIDATED" if restricted.
		if len(tc.AllowedTools) > 0 {
			fcc.Mode = "VALIDATED"
			fcc.AllowedFunctionNames = tc.AllowedTools
		} else {
			fcc.Mode = "AUTO"
		}
	case "none":
		fcc.Mode = "NONE"
	case "required", "any":
		fcc.Mode = "ANY"
		if len(tc.AllowedTools) > 0 {
			fcc.AllowedFunctionNames = tc.AllowedTools
		}
	case "tool":
		fcc.Mode = "ANY"
		if tc.ToolName != "" {
			fcc.AllowedFunctionNames = []string{tc.ToolName}
		} else if len(tc.AllowedTools) > 0 {
			fcc.AllowedFunctionNames = tc.AllowedTools
		}
	default:
		if tc.ToolName != "" {
			fcc.Mode = "ANY"
			fcc.AllowedFunctionNames = []string{tc.ToolName}
		} else if len(tc.AllowedTools) > 0 {
			fcc.Mode = "VALIDATED"
			fcc.AllowedFunctionNames = tc.AllowedTools
		} else {
			return nil
		}
	}
	return &googleToolConfig{FunctionCallingConfig: fcc}
}

func encodeGoogleGenerationConfig(req *ir.Request, ctx *ir.ConversionContext) *googleGenerationConfig {
	gc := &googleGenerationConfig{
		MaxOutputTokens:  req.Generation.MaxTokens,
		Temperature:      req.Generation.Temperature,
		TopP:             req.Generation.TopP,
		TopK:             req.Generation.TopK,
		CandidateCount:   req.Generation.N,
		PresencePenalty:  req.Generation.PresencePenalty,
		FrequencyPenalty: req.Generation.FrequencyPenalty,
		Seed:             req.Generation.Seed,
		ResponseLogprobs: req.Generation.LogProbs,
		Logprobs:         req.Generation.TopLogProbs,
	}

	if len(req.Generation.StopSequences) > 0 {
		stops := req.Generation.StopSequences
		if len(stops) > 5 {
			stops = stops[:5]
			ctx.AddWarning(
				"truncated_stop_sequences",
				"generation.stop_sequences",
				"Gemini supports at most 5 stop sequences; truncated excess entries",
			)
		}
		gc.StopSequences = stops
	}

	if len(req.Generation.Modalities) > 0 {
		mods := make([]string, 0, len(req.Generation.Modalities))
		for _, m := range req.Generation.Modalities {
			if trimmed := strings.TrimSpace(m); trimmed != "" {
				mods = append(mods, strings.ToUpper(trimmed))
			}
		}
		gc.ResponseModalities = mods
	}

	if req.ResponseFormat != nil {
		switch strings.ToLower(strings.TrimSpace(req.ResponseFormat.Type)) {
		case "json_object":
			gc.ResponseMimeType = "application/json"
		case "json_schema":
			gc.ResponseMimeType = "application/json"
			if !isNullOrEmptyRaw(req.ResponseFormat.Schema) {
				gc.ResponseSchema = sanitizeGeminiSchema(req.ResponseFormat.Schema)
			}
		case "text":
			gc.ResponseMimeType = "text/plain"
		}
	}

	if req.Reasoning != nil {
		gc.ThinkingConfig = encodeGoogleThinkingConfig(req.Reasoning)
	}

	return gc
}

func encodeGoogleThinkingConfig(rc *ir.ReasoningConfig) *googleThinkingConfig {
	if rc == nil {
		return nil
	}
	effortLower := strings.ToLower(strings.TrimSpace(rc.Effort))
	if strings.EqualFold(rc.Mode, "disabled") ||
		(rc.Enabled != nil && !*rc.Enabled) ||
		effortLower == "none" {
		return &googleThinkingConfig{
			IncludeThoughts: boolPtr(false),
			ThinkingBudget:  intPtr(0),
		}
	}

	tc := &googleThinkingConfig{}
	if strings.EqualFold(rc.Display, "omitted") {
		tc.IncludeThoughts = boolPtr(false)
	} else if (rc.Enabled != nil && *rc.Enabled) ||
		strings.EqualFold(rc.Mode, "enabled") ||
		strings.EqualFold(rc.Mode, "adaptive") ||
		rc.BudgetTokens != nil ||
		effortLower != "" {
		tc.IncludeThoughts = boolPtr(true)
	}
	if rc.BudgetTokens != nil {
		b := *rc.BudgetTokens
		tc.ThinkingBudget = &b
	} else if effortLower != "" {
		tc.ThinkingLevel = mapEffortToGoogleThinkingLevel(effortLower)
	}
	if tc.IncludeThoughts == nil && tc.ThinkingBudget == nil && tc.ThinkingLevel == "" {
		return nil
	}
	return tc
}

func mapEffortToGoogleThinkingLevel(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal":
		return "MINIMAL"
	case "low":
		return "LOW"
	case "medium":
		return "MEDIUM"
	case "high", "max", "xhigh":
		return "HIGH"
	default:
		return strings.ToUpper(strings.TrimSpace(effort))
	}
}

// --- JSON Schema Sanitization for Gemini ---

var geminiUnsupportedSchemaKeys = map[string]struct{}{
	"$schema":               {},
	"$id":                   {},
	"$defs":                 {},
	"definitions":           {},
	"$ref":                  {},
	"$comment":              {},
	"$anchor":               {},
	"$vocabulary":           {},
	"additionalProperties":  {},
	"title":                 {},
	"exclusiveMinimum":      {},
	"exclusiveMaximum":      {},
	"examples":              {},
	"propertyNames":         {},
	"patternProperties":     {},
	"dependentRequired":     {},
	"dependentSchemas":      {},
	"if":                    {},
	"then":                  {},
	"else":                  {},
	"not":                   {},
	"readOnly":              {},
	"writeOnly":             {},
	"contentMediaType":      {},
	"contentEncoding":       {},
	"strict":                {},
	"prefixItems":           {},
	"unevaluatedProperties": {},
	"unevaluatedItems":      {},
}

const (
	maxGeminiSchemaDepth = 20
	maxGeminiSchemaNodes = 2000
)

type schemaSanitizeState struct {
	defs     map[string]map[string]any
	visiting map[string]bool
	nodes    int
}

// sanitizeGeminiSchema recursively removes JSON Schema keywords rejected by Gemini's
// OpenAPI 3.0 schema validator, inlines local $ref/$defs pointers with depth/node limits,
// flattens unsupported oneOf/allOf composites, and preserves user property keys inside "properties".
func sanitizeGeminiSchema(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(trimmed, &root); err != nil {
		return raw
	}
	state := &schemaSanitizeState{
		defs:     collectSchemaDefs(root),
		visiting: make(map[string]bool),
	}
	cleaned := sanitizeSchemaNode(root, state, 0)
	out, err := json.Marshal(cleaned)
	if err != nil {
		return raw
	}
	return out
}

func collectSchemaDefs(root map[string]any) map[string]map[string]any {
	defs := make(map[string]map[string]any)
	for _, key := range []string{"$defs", "definitions"} {
		if m, ok := root[key].(map[string]any); ok {
			for name, val := range m {
				if sub, ok := val.(map[string]any); ok {
					defs["#/"+key+"/"+name] = sub
				}
			}
		}
	}
	return defs
}

func sanitizeSchemaNode(node map[string]any, state *schemaSanitizeState, depth int) map[string]any {
	if node == nil {
		return nil
	}
	state.nodes++
	if depth > maxGeminiSchemaDepth || state.nodes > maxGeminiSchemaNodes {
		return map[string]any{"type": "object"}
	}

	// Inline local $ref if present and resolvable without cycles.
	if refStr, ok := node["$ref"].(string); ok && refStr != "" && !state.visiting[refStr] {
		if target, exists := state.defs[refStr]; exists {
			state.visiting[refStr] = true
			resolved := sanitizeSchemaNode(target, state, depth+1)
			delete(state.visiting, refStr)
			if desc, ok := node["description"].(string); ok && desc != "" {
				resolved["description"] = desc
			}
			return resolved
		}
	}

	out := make(map[string]any, len(node))
	for k, v := range node {
		if _, unsupported := geminiUnsupportedSchemaKeys[k]; unsupported {
			continue
		}

		switch k {
		case "const":
			// Gemini only permits string values in enum; convert string const to single-element enum.
			if _, hasEnum := node["enum"]; !hasEnum && v != nil {
				if s, ok := v.(string); ok {
					out["enum"] = []any{s}
				}
			}

		case "type":
			// Normalize JSON Schema nullable union types like ["string", "null"].
			if arr, ok := v.([]any); ok {
				var nonNull []string
				nullable := false
				for _, item := range arr {
					s, _ := item.(string)
					if s == "null" {
						nullable = true
					} else if s != "" {
						nonNull = append(nonNull, s)
					}
				}
				if len(nonNull) > 0 {
					out["type"] = nonNull[0]
				}
				if nullable {
					out["nullable"] = true
				}
			} else {
				out["type"] = v
			}

		case "properties":
			// Recurse into each property schema WITHOUT removing user property names (e.g. a field named "title").
			if props, ok := v.(map[string]any); ok {
				cleanProps := make(map[string]any, len(props))
				for propName, propVal := range props {
					if propMap, ok := propVal.(map[string]any); ok {
						cleanProps[propName] = sanitizeSchemaNode(propMap, state, depth+1)
					} else {
						cleanProps[propName] = propVal
					}
				}
				out["properties"] = cleanProps
			}

		case "items":
			if itemMap, ok := v.(map[string]any); ok {
				out["items"] = sanitizeSchemaNode(itemMap, state, depth+1)
			} else if itemList, ok := v.([]any); ok && len(itemList) > 0 {
				if firstMap, ok := itemList[0].(map[string]any); ok {
					out["items"] = sanitizeSchemaNode(firstMap, state, depth+1)
				}
			}

		case "anyOf", "oneOf", "allOf":
			if arr, ok := v.([]any); ok {
				sanitizedBranch, simplified := sanitizeCompositeSchema(k, arr, state, depth+1)
				if simplified != nil {
					for sk, sv := range simplified {
						if _, exists := out[sk]; !exists {
							out[sk] = sv
						}
					}
				} else if len(sanitizedBranch) > 0 {
					// Gemini supports anyOf but rejects oneOf/allOf; map surviving oneOf branches to anyOf.
					out["anyOf"] = sanitizedBranch
				}
			}

		default:
			if subMap, ok := v.(map[string]any); ok {
				out[k] = sanitizeSchemaNode(subMap, state, depth+1)
			} else {
				out[k] = v
			}
		}
	}

	return out
}

func isNullSchemaBranch(bm map[string]any) bool {
	t, ok := bm["type"].(string)
	if !ok || !strings.EqualFold(t, "null") {
		return false
	}
	for k := range bm {
		switch k {
		case "type", "title", "description", "default", "examples", "$comment":
			continue
		default:
			return false
		}
	}
	return true
}

func sanitizeCompositeSchema(keyword string, branches []any, state *schemaSanitizeState, depth int) ([]any, map[string]any) {
	var nonNull []map[string]any
	hasNull := false
	for _, b := range branches {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		if isNullSchemaBranch(bm) {
			hasNull = true
			continue
		}
		nonNull = append(nonNull, sanitizeSchemaNode(bm, state, depth))
	}

	// Flatten allOf into a single merged schema (Gemini rejects allOf).
	if keyword == "allOf" && len(nonNull) > 0 {
		merged := make(map[string]any)
		mergedProps := make(map[string]any)
		var mergedReq []any
		reqSeen := make(map[string]bool)
		for _, m := range nonNull {
			for k, v := range m {
				switch k {
				case "properties":
					if pm, ok := v.(map[string]any); ok {
						for pk, pv := range pm {
							mergedProps[pk] = pv
						}
					}
				case "required":
					if rl, ok := v.([]any); ok {
						for _, rv := range rl {
							if rs, ok := rv.(string); ok && !reqSeen[rs] {
								reqSeen[rs] = true
								mergedReq = append(mergedReq, rs)
							}
						}
					}
				default:
					if _, exists := merged[k]; !exists {
						merged[k] = v
					}
				}
			}
		}
		if len(mergedProps) > 0 {
			merged["properties"] = mergedProps
			if _, hasType := merged["type"]; !hasType {
				merged["type"] = "object"
			}
		}
		if len(mergedReq) > 0 {
			merged["required"] = mergedReq
		}
		if hasNull {
			merged["nullable"] = true
		}
		return nil, merged
	}

	// Simplify common nullable union: anyOf/oneOf: [<schema>, {"type": "null", ...}] -> <schema> + nullable: true
	if (keyword == "anyOf" || keyword == "oneOf") && hasNull && len(nonNull) == 1 {
		merged := nonNull[0]
		merged["nullable"] = true
		return nil, merged
	}
	out := make([]any, 0, len(nonNull))
	for _, m := range nonNull {
		out = append(out, m)
	}
	return out, nil
}
