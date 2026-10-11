package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

const decisionQuestions = `[{"type":"predicate","name":"test","instructions":"Is this a greeting?"},{"type":"choice","instructions":"Choose","choices":[{"value":false},{"value":"false"}]},{"type":"score","instructions":"Rate","levels":[{"label":"low"},{"label":"high","description":"Important"}]}]`

func TestDecisionsRequestPreservesNativeFields(t *testing.T) {
	for _, input := range []string{
		`"Hello"`,
		`[{"role":"user","content":"Hello"}]`,
		`[{"role":"user","type":"message","content":[{"type":"input_text","text":"Hello"},{"type":"input_image","image_url":"data:image/png;base64,AA==","detail":"auto"},{"type":"input_image","image_url":"https://example.com/image.png"}]}]`,
	} {
		body := `{"model":"alias","input":` + input + `,"questions":` + decisionQuestions + `,"stream":false,"safety_identifier":null,"future":{"zero":0,"flag":false}}`
		var request OpenAIDecisionsRequest
		require.NoError(t, common.Unmarshal([]byte(body), &request))
		encoded, err := common.Marshal(request)
		require.NoError(t, err)
		require.JSONEq(t, body, string(encoded))
		require.False(t, request.IsStream(nil))
		meta := request.GetTokenCountMeta()
		require.Contains(t, meta.CombineText, "Hello")
		require.Contains(t, meta.CombineText, "Important")
		require.NotContains(t, meta.CombineText, "base64")
		require.NotContains(t, meta.CombineText, "example.com")
		require.Empty(t, meta.Files, "Decisions must not fetch images during token estimation")
		copy := request
		copy.SetModelName("gpt-6-luna")
		mapped, err := common.Marshal(copy)
		require.NoError(t, err)
		require.Contains(t, string(mapped), `"model":"gpt-6-luna"`)
		require.Equal(t, "alias", request.Model)
	}
}

func TestDecisionsRequestValidation(t *testing.T) {
	valid := `{"model":"gpt-6-luna","input":"hello","questions":` + decisionQuestions + `}`
	for _, body := range []string{
		`null`, `{}`, `[]`,
		strings.Replace(valid, `"model":"gpt-6-luna"`, `"model":""`, 1),
		strings.Replace(valid, `"input":"hello"`, `"input":null`, 1),
		strings.Replace(valid, `"input":"hello"`, `"input":123`, 1),
		strings.Replace(valid, `"input":"hello"`, `"input":[{"role":"assistant","content":"hi"}]`, 1),
		strings.Replace(valid, `"input":"hello"`, `"input":[{"role":"user","content":[{"type":"input_file","file_id":"f"}]}]`, 1),
		strings.Replace(valid, decisionQuestions, `[]`, 1),
		strings.Replace(valid, `"predicate"`, `"unknown"`, 1),
		strings.Replace(valid, `"instructions":"Is this a greeting?"`, `"instructions":null`, 1),
		strings.Replace(valid, `{"value":false}`, `{"value":0}`, 1),
		strings.Replace(valid, `{"value":"false"}`, `{"value":false}`, 1),
		strings.Replace(valid, `{"label":"low"}`, `{}`, 1),
		strings.TrimSuffix(valid, "}") + `,"stream":true}`,
		strings.TrimSuffix(valid, "}") + `,"stream":"false"}`,
		strings.TrimSuffix(valid, "}") + `,"stream_options":{"include_usage":false}}`,
	} {
		var request OpenAIDecisionsRequest
		require.Error(t, common.Unmarshal([]byte(body), &request), body)
	}
}
