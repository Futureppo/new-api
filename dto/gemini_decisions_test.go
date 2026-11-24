package dto

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const geminiDecisionsQuestions = `{"urgent":{"type":"noul","instructions":"Is it urgent?","criteria":{"true_description":"urgent"}},"team":{"type":"choice","instructions":{"type":"text","text":"Choose a team"},"criteria":{"support":"","sales":"Sales"}},"severity":{"type":"score","instructions":[{"type":"text","text":"Rate severity"}],"criteria":["low","high"]}}`

func TestGeminiDecisionsMultimodalRoundTrip(t *testing.T) {
	body := `{"model":"gemini-decision-1","input":[{"type":"text","text":"hello"},{"type":"image","data":"IMAGE_BASE64","mime_type":"image/png","extra":0},{"type":"audio","uri":"gs://bucket/audio.wav"},{"type":"video","uri":"https://example.test/video.mp4","start_offset":0},{"type":"document","data":"PDF_BASE64","mime_type":"application/pdf"}],"questions":` + geminiDecisionsQuestions + `,"stream":false,"future":{"large":9007199254740993,"zero":0,"flag":false}}`
	var req GeminiDecisionsRequest
	require.NoError(t, common.Unmarshal([]byte(body), &req))
	copy := req
	copy.SetModelName("mapped-model")
	encoded, err := common.Marshal(copy)
	require.NoError(t, err)
	require.Equal(t, "gemini-decision-1", req.Model)
	require.Equal(t, "mapped-model", gjson.GetBytes(encoded, "model").String())
	for _, path := range []string{"input", "questions", "future", "stream"} {
		require.JSONEq(t, gjson.Get(body, path).Raw, gjson.GetBytes(encoded, path).Raw)
	}
	require.Equal(t, "9007199254740993", gjson.GetBytes(encoded, "future.large").Raw)
	require.False(t, req.IsStream(nil))
	estimate := req.GetTokenCountMeta().CombineText
	require.Contains(t, estimate, "hello")
	require.Contains(t, estimate, "Rate severity")
	require.NotContains(t, estimate, "BASE64")
	require.NotContains(t, estimate, "https://")
}

func TestGeminiDecisionsValidation(t *testing.T) {
	base := `{"model":"gemini-decision-1","input":"hi","questions":` + geminiDecisionsQuestions + `}`
	for _, tc := range []struct{ name, field, value string }{
		{"missing model", "model", `null`}, {"missing input", "input", `null`},
		{"empty input blocks", "input", `[]`}, {"untyped input", "input", `{"text":"hello"}`},
		{"media without data", "input", `{"type":"image"}`}, {"bad text", "input", `{"type":"text","text":0}`},
		{"question array", "questions", `[]`}, {"empty questions", "questions", `{}`},
		{"invalid id", "questions", `{" bad ":{"type":"noul","instructions":"hi"}}`},
		{"unknown type", "questions", `{"q":{"type":"predicate","instructions":"hi"}}`},
		{"missing instructions", "questions", `{"q":{"type":"noul"}}`},
		{"media instructions", "questions", `{"q":{"type":"noul","instructions":{"type":"image","data":"abc"}}}`},
		{"bad noul criteria", "questions", `{"q":{"type":"noul","instructions":"hi","criteria":{"true_description":false}}}`},
		{"one choice", "questions", `{"q":{"type":"choice","instructions":"hi","criteria":{"a":""}}}`},
		{"null choice", "questions", `{"q":{"type":"choice","instructions":"hi","criteria":{"a":"","b":null}}}`},
		{"empty score level", "questions", `{"q":{"type":"score","instructions":"hi","criteria":["low",""]}}`},
		{"stream", "stream", `true`}, {"stream options", "stream_options", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fields map[string]json.RawMessage
			require.NoError(t, common.Unmarshal([]byte(base), &fields))
			fields[tc.field] = json.RawMessage(tc.value)
			body, err := common.Marshal(fields)
			require.NoError(t, err)
			var req GeminiDecisionsRequest
			require.Error(t, common.Unmarshal(body, &req))
		})
	}
	for _, input := range []string{`"hello"`, `{"type":"text","text":"hello"}`, `[{"type":"text","text":"hello"}]`} {
		var req GeminiDecisionsRequest
		require.NoError(t, common.Unmarshal([]byte(`{"model":"gemini-decision-1","input":`+input+`,"questions":`+geminiDecisionsQuestions+`}`), &req))
	}
}

func TestGeminiDecisionsLimits(t *testing.T) {
	for _, n := range []int{1, 1000, 1001} {
		questions := make([]string, n)
		for i := range questions {
			questions[i] = fmt.Sprintf(`"q%d":{"type":"noul","instructions":"hi"}`, i)
		}
		var req GeminiDecisionsRequest
		err := common.Unmarshal([]byte(`{"model":"gemini-decision-1","input":"hi","questions":{`+strings.Join(questions, ",")+`}}`), &req)
		require.Equal(t, n > 1000, err != nil, "questions=%d, err=%v", n, err)
	}
	for _, kind := range []string{"choice", "score"} {
		for _, n := range []int{1, 2, 255, 256} {
			options := make([]string, n)
			for i := range options {
				options[i] = fmt.Sprintf(`"level%d"`, i)
				if kind == "choice" {
					options[i] += `:""`
				}
			}
			criteria := "[" + strings.Join(options, ",") + "]"
			if kind == "choice" {
				criteria = "{" + strings.Join(options, ",") + "}"
			}
			var req GeminiDecisionsRequest
			err := common.Unmarshal([]byte(`{"model":"gemini-decision-1","input":"hi","questions":{"q":{"type":"`+kind+`","instructions":"hi","criteria":`+criteria+`}}}`), &req)
			require.Equal(t, n < 2 || n > 255, err != nil, "%s=%d, err=%v", kind, n, err)
		}
	}
}
