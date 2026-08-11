package mistral

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2MistralPreservesReasoningEffort(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:           "zai-glm-5-2",
		ReasoningEffort: "max",
	}

	converted := requestOpenAI2Mistral(request)

	require.Equal(t, "max", converted.ReasoningEffort)
}

func TestNormalizeMistralNonStreamThinkingContent(t *testing.T) {
	input := []byte(`{
		"id":"response-id",
		"choices":[{
			"message":{
				"role":"assistant",
				"content":[
					{"type":"thinking","thinking":[{"type":"text","text":"先分析"},{"type":"text","text":"，再作答"}],"closed":true},
					{"type":"text","text":"最终答案"}
				]
			},
			"finish_reason":"stop"
		}],
		"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}
	}`)

	output, err := normalizeMistralResponseData(input)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(output, &response))
	choice := response["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	require.Equal(t, "最终答案", message["content"])
	require.Equal(t, "先分析，再作答", message["reasoning_content"])
	require.Equal(t, "stop", choice["finish_reason"])
	require.Equal(t, "response-id", response["id"])
}

func TestNormalizeMistralStreamThinkingContent(t *testing.T) {
	input := `{"id":"response-id","choices":[{"index":0,"delta":{"index":0,"content":[{"type":"thinking","thinking":[{"type":"text","text":"思考片段"}],"closed":true}]},"finish_reason":null}]}`

	output, err := normalizeMistralStreamData(input)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.UnmarshalJsonStr(output, &response))
	choice := response["choices"].([]any)[0].(map[string]any)
	delta := choice["delta"].(map[string]any)
	require.Equal(t, "", delta["content"])
	require.Equal(t, "思考片段", delta["reasoning_content"])
}

func TestNormalizeMistralMixedAndStringContent(t *testing.T) {
	mixedInput := []byte(`{"choices":[{"delta":{"content":["前缀",{"type":"thinking","thinking":"推理"},{"type":"text","text":"正文"},{"type":"custom","text":"保留文本"}]}}]}`)

	mixedOutput, err := normalizeMistralResponseData(mixedInput)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(mixedOutput, &response))
	delta := response["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	require.Equal(t, "前缀正文保留文本", delta["content"])
	require.Equal(t, "推理", delta["reasoning_content"])

	stringInput := []byte(`{"choices":[{"delta":{"content":"普通文本"}}]}`)
	stringOutput, err := normalizeMistralResponseData(stringInput)
	require.NoError(t, err)
	require.Equal(t, stringInput, stringOutput)
}

func TestNormalizeMistralResponseRejectsInvalidJSON(t *testing.T) {
	_, err := normalizeMistralResponseData([]byte(`{"choices":`))
	require.Error(t, err)
}
