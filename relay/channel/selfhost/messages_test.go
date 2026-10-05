package selfhost

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestQwenInstructionMessages(t *testing.T) {
	cases := []struct {
		name      string
		responses bool
		input     string
		want      string
	}{
		{
			name:  "late system",
			input: `"messages":[{"role":"user","content":"hello"},{"role":"system","content":"first","name":"policy"}]`,
			want:  `"messages":[{"role":"system","content":"first","name":"policy"},{"role":"user","content":"hello"}]`,
		},
		{
			name:  "multiple leading systems",
			input: `"messages":[{"role":"system","content":"first"},{"role":"system","content":"second"},{"role":"user","content":"hello"}]`,
			want:  `"messages":[{"role":"system","content":"first\n\nsecond"},{"role":"user","content":"hello"}]`,
		},
		{
			name:  "developer and split systems preserve tools and reasoning",
			input: `"messages":[{"role":"developer","content":"first"},{"role":"user","content":"hello"},{"role":"system","content":"second"},{"role":"assistant","content":null,"reasoning_content":"thinking","tool_calls":[{"id":"call1","type":"function","function":{"name":"f","arguments":"{\"n\":0}"}}]},{"role":"developer","content":"third"},{"role":"tool","tool_call_id":"call1","content":"false"}]`,
			want:  `"messages":[{"role":"system","content":"first\n\nsecond\n\nthird"},{"role":"user","content":"hello"},{"role":"assistant","content":null,"reasoning_content":"thinking","tool_calls":[{"id":"call1","type":"function","function":{"name":"f","arguments":"{\"n\":0}"}}]},{"role":"tool","tool_call_id":"call1","content":"false"}]`,
		},
		{
			name:  "late developer",
			input: `"messages":[{"role":"user","content":"hello"},{"role":"developer","content":"policy"}]`,
			want:  `"messages":[{"role":"system","content":"policy"},{"role":"user","content":"hello"}]`,
		},
		{
			name:  "content parts and metadata survive",
			input: `"messages":[{"role":"system","content":"first","name":"policy"},{"role":"system","content":[{"type":"text","text":"second","cache_control":{"type":"ephemeral"}},{"type":"text","text":"third","extension":false}],"name":"policy"},{"role":"user","content":"hello"}]`,
			want:  `"messages":[{"role":"system","name":"policy","content":[{"type":"text","text":"first"},{"type":"text","text":"\n\n"},{"type":"text","text":"second","cache_control":{"type":"ephemeral"}},{"type":"text","text":"third","extension":false}]},{"role":"user","content":"hello"}]`,
		},
		{
			name:      "responses instructions and split systems",
			responses: true,
			input:     `"instructions":"first","input":[{"role":"user","content":"hello"},{"type":"message","role":"system","content":"second"},{"type":"function_call","call_id":"call1","name":"f","arguments":"{}"},{"role":"system","content":"third"},{"type":"function_call_output","call_id":"call1","output":"ok"},{"role":"developer","content":"keep this role"}]`,
			want:      `"input":[{"type":"message","role":"system","content":"first\n\nsecond\n\nthird"},{"role":"user","content":"hello"},{"type":"function_call","call_id":"call1","name":"f","arguments":"{}"},{"type":"function_call_output","call_id":"call1","output":"ok"},{"role":"developer","content":"keep this role"}]`,
		},
		{
			name:      "responses content array",
			responses: true,
			input:     `"instructions":"first","input":[{"role":"system","content":[{"type":"input_text","text":"second"}]},{"role":"user","content":"hello"}]`,
			want:      `"input":[{"role":"system","content":[{"type":"input_text","text":"first"},{"type":"input_text","text":"\n\n"},{"type":"input_text","text":"second"}]},{"role":"user","content":"hello"}]`,
		},
		{
			name:      "responses instructions only",
			responses: true,
			input:     `"instructions":"policy","input":"hello"`,
			want:      `"instructions":"policy","input":"hello"`,
		},
		{
			name:      "responses references are opaque",
			responses: true,
			input:     `"input":[{"type":"item_reference","id":"item1"},{"type":"function_call_output","call_id":"call1","output":"ok","role":"system"},{"role":"system","content":"policy"}]`,
			want:      `"input":[{"role":"system","content":"policy"},{"type":"item_reference","id":"item1"},{"type":"function_call_output","call_id":"call1","output":"ok","role":"system"}]`,
		},
		{
			name:  "empty instruction retained",
			input: `"messages":[{"role":"system","content":""},{"role":"system","content":"policy"},{"role":"user","content":"hello"}]`,
			want:  `"messages":[{"role":"system","content":"\n\npolicy"},{"role":"user","content":"hello"}]`,
		},
		{
			name:  "empty content array retained",
			input: `"messages":[{"role":"user","content":"hello"},{"role":"system","content":[]}]`,
			want:  `"messages":[{"role":"system","content":[]},{"role":"user","content":"hello"}]`,
		},
		{
			name:  "valid leading system unchanged",
			input: `"messages":[{"role":"system","content":[{"type":"text","text":"policy"}],"extension":false},{"role":"user","content":"hello"}]`,
			want:  `"messages":[{"role":"system","content":[{"type":"text","text":"policy"}],"extension":false},{"role":"user","content":"hello"}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, channelType := range []int{constant.ChannelTypeVLLM, constant.ChannelTypeLiteLLM} {
				for _, model := range []string{"org/Qwen3.8-Flash-Next-FP8", "qwen3.8-27b-fp8", "other-model"} {
					prefix := `{"model":"` + model + `",`
					input, want := prefix+tc.input+`}`, prefix+tc.want+`}`
					if model == "other-model" {
						want = input
					}
					out, _, _, err := NormalizeRequest([]byte(input), channelType, tc.responses)
					require.NoError(t, err)
					require.JSONEq(t, want, string(out))
					again, _, _, err := NormalizeRequest(out, channelType, tc.responses)
					require.NoError(t, err)
					require.JSONEq(t, string(out), string(again), "normalization must be idempotent")
				}
			}
		})
	}
}

func TestQwenInstructionMergeRejectsDataLoss(t *testing.T) {
	for _, tc := range []struct {
		fields    string
		responses bool
		message   string
	}{
		{`"messages":[{"role":"system","content":"a","name":"a"},{"role":"system","content":"b","name":"b"}]`, false, "conflicting instruction field"},
		{`"messages":[{"role":"user","content":"hello"},{"role":"system","content":null}]`, false, "content must be"},
		{`"instructions":{},"input":[{"role":"system","content":"a"}]`, true, "instructions must be a string"},
	} {
		_, _, _, err := NormalizeRequest([]byte(`{"model":"qwen3.8-27b",`+tc.fields+`}`), constant.ChannelTypeLiteLLM, tc.responses)
		require.ErrorContains(t, err, tc.message)
	}
}
