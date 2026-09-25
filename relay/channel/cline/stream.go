package cline

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

type chatChoice struct {
	message  map[string]any
	finish   string
	tools    map[int]map[string]any
	details  map[int]map[string]any
	logprobs map[string]any
}

// ChatAccumulator collects OpenAI SSE deltas, retaining Cline's response
// metadata and usage fields rather than narrowing them to a provider DTO.
type ChatAccumulator struct {
	response map[string]json.RawMessage
	choices  map[int]*chatChoice
}

func (a *ChatAccumulator) Add(data string) error {
	var chunk struct {
		Choices []struct {
			Index        int            `json:"index"`
			Delta        map[string]any `json:"delta"`
			FinishReason *string        `json:"finish_reason"`
			Logprobs     map[string]any `json:"logprobs"`
		} `json:"choices"`
		Error any `json:"error"`
	}
	if err := common.UnmarshalJsonStr(data, &chunk); err != nil {
		return fmt.Errorf("invalid Cline stream chunk: %w", err)
	}
	if upstreamErr := dto.GetOpenAIError(chunk.Error); upstreamErr != nil {
		return fmt.Errorf("Cline stream error: %s", upstreamErr.Message)
	}
	if chunk.Choices == nil {
		return fmt.Errorf("Cline stream chunk is missing choices")
	}
	if a.response == nil {
		a.response = make(map[string]json.RawMessage)
		a.choices = make(map[int]*chatChoice)
	}
	var fields map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(data, &fields); err != nil {
		return err
	}
	for key, value := range fields {
		if key != "choices" && key != "object" && key != "error" && key != "obfuscation" && string(value) != "null" {
			a.response[key] = value
		}
	}
	for _, delta := range chunk.Choices {
		if delta.Index < 0 {
			return fmt.Errorf("invalid Cline choice index")
		}
		choice := a.choices[delta.Index]
		if choice == nil {
			choice = &chatChoice{message: map[string]any{"role": "assistant", "content": nil}, tools: make(map[int]map[string]any), details: make(map[int]map[string]any)}
			a.choices[delta.Index] = choice
		}
		for key, value := range delta.Delta {
			switch key {
			case "tool_calls":
				if err := mergeIndexedDeltas(choice.tools, value); err != nil {
					return err
				}
			case "reasoning_details":
				if err := mergeIndexedDeltas(choice.details, value); err != nil {
					return err
				}
			default:
				mergeDeltaField(choice.message, key, value)
			}
		}
		if delta.FinishReason != nil {
			choice.finish = *delta.FinishReason
		}
		if delta.Logprobs != nil {
			if choice.logprobs == nil {
				choice.logprobs = make(map[string]any)
			}
			for key, value := range delta.Logprobs {
				if items, ok := value.([]any); ok {
					previous, _ := choice.logprobs[key].([]any)
					choice.logprobs[key] = append(previous, items...)
				} else if value != nil {
					choice.logprobs[key] = value
				}
			}
		}
	}
	return nil
}

func mergeDeltaField(target map[string]any, key string, value any) {
	if value == nil {
		return
	}
	switch key {
	case "content", "reasoning", "reasoning_content", "refusal", "name", "arguments", "text", "signature", "data":
		if fragment, ok := value.(string); ok {
			previous, _ := target[key].(string)
			target[key] = previous + fragment
			return
		}
	case "function", "function_call":
		if fields, ok := value.(map[string]any); ok {
			nested, _ := target[key].(map[string]any)
			if nested == nil {
				nested = make(map[string]any)
			}
			for name, field := range fields {
				mergeDeltaField(nested, name, field)
			}
			target[key] = nested
			return
		}
	}
	target[key] = value
}

func mergeIndexedDeltas(target map[int]map[string]any, value any) error {
	if value == nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("invalid Cline indexed delta")
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid Cline indexed delta item")
		}
		number, ok := item["index"].(float64)
		if !ok || number < 0 || number > 1<<31-1 || float64(int(number)) != number {
			return fmt.Errorf("invalid Cline delta index")
		}
		index := int(number)
		if target[index] == nil {
			target[index] = make(map[string]any)
		}
		for key, field := range item {
			mergeDeltaField(target[index], key, field)
		}
	}
	return nil
}

func sortedDeltaItems(items map[int]map[string]any, removeIndex bool) []map[string]any {
	indexes := make([]int, 0, len(items))
	for index := range items {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	result := make([]map[string]any, 0, len(items))
	for _, index := range indexes {
		item := items[index]
		if removeIndex {
			delete(item, "index")
		}
		result = append(result, item)
	}
	return result
}

func (a *ChatAccumulator) JSON() ([]byte, error) {
	if len(a.choices) == 0 {
		return nil, fmt.Errorf("Cline stream returned no choices")
	}
	choices := make(map[int]map[string]any, len(a.choices))
	for index, choice := range a.choices {
		if choice.finish == "" {
			return nil, fmt.Errorf("Cline stream ended before choice %d finished", index)
		}
		if len(choice.tools) > 0 {
			choice.message["tool_calls"] = sortedDeltaItems(choice.tools, true)
		}
		if len(choice.details) > 0 {
			choice.message["reasoning_details"] = sortedDeltaItems(choice.details, false)
		}
		choices[index] = map[string]any{"index": index, "message": choice.message, "finish_reason": choice.finish, "logprobs": choice.logprobs}
	}
	var err error
	a.response["choices"], err = common.Marshal(sortedDeltaItems(choices, false))
	if err != nil {
		return nil, err
	}
	a.response["object"] = json.RawMessage(`"chat.completion"`)
	return common.Marshal(a.response)
}
