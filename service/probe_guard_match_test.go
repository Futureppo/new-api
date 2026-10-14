package service

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func parseProbeRequest(t testing.TB, protocol, body string) dto.Request {
	t.Helper()
	var request dto.Request
	switch protocol {
	case "responses":
		request = &dto.OpenAIResponsesRequest{}
	case "messages":
		request = &dto.ClaudeRequest{}
	default:
		request = &dto.GeneralOpenAIRequest{}
	}
	require.NoError(t, common.UnmarshalJsonStr(body, request))
	return request
}

func matcherConfig() setting.ProbeGuardSetting {
	return setting.ProbeGuardSetting{TextRules: setting.DefaultProbeGuardTextRules, ProbePingEnabled: true}
}

func TestProbeGuardSingleTurnMatching(t *testing.T) {
	cases := []struct {
		name, protocol, body string
		match                bool
	}{
		{"chat plain", "chat", `{"messages":[{"role":"user","content":"Reply with exactly OK"}]}`, true},
		{"chat tools allowed", "chat", `{"messages":[{"role":"system","content":"helper"},{"role":"user","content":"say OK"}],"tools":[{"type":"function","function":{"name":"read"}}]}`, true},
		{"chat text blocks", "chat", `{"messages":[{"role":"user","content":[{"type":"text","text":"reply"},{"type":"text","text":"exactly: OK"}]}]}`, true},
		{"normalization", "chat", `{"messages":[{"role":"user","content":"  ＲＥＰＬＹ\n  ＷＩＴＨ　ＥＸＡＣＴＬＹ　ＯＫ  "}]}`, true},
		{"responses string", "responses", `{"input":"say OK","max_output_tokens":4000,"stream":false}`, true},
		{"responses blocks", "responses", `{"input":[{"role":"system","content":"helper"},{"role":"user","content":[{"type":"input_text","text":"回复两个字：可用"}]}]}`, true},
		{"responses tool definitions", "responses", `{"input":"say OK","tools":[{"type":"web_search"}],"previous_response_id":null}`, true},
		{"claude plain", "messages", `{"system":"helper","messages":[{"role":"user","content":"REPLY ` + "`PONG`" + ` ONLY"}],"tools":[{"name":"read","input_schema":{"type":"object"}}]}`, true},
		{"claude text blocks", "messages", `{"messages":[{"role":"user","content":[{"type":"text","text":"Reply with exactly: OK"}]}]}`, true},
		{"not substring", "chat", `{"messages":[{"role":"user","content":"Explain Reply with exactly OK"}]}`, false},
		{"punctuation retained", "chat", `{"messages":[{"role":"user","content":"say OK!"}]}`, false},
		{"greeting", "chat", `{"messages":[{"role":"user","content":"hi"}]}`, false},
		{"zcode excluded", "responses", `{"instructions":"You are ZCode connectivity probe.","input":"hi"}`, false},
		{"assistant history", "chat", `{"messages":[{"role":"assistant","content":"title?"},{"role":"user","content":"say OK"}]}`, false},
		{"two users", "chat", `{"messages":[{"role":"user","content":"hello"},{"role":"user","content":"say OK"}]}`, false},
		{"tool history", "chat", `{"messages":[{"role":"user","content":"say OK"},{"role":"tool","content":"result","tool_call_id":"call1"}]}`, false},
		{"legacy function history", "chat", `{"messages":[{"role":"function","content":"result"},{"role":"user","content":"say OK"}]}`, false},
		{"tool calls on user", "chat", `{"messages":[{"role":"user","content":"say OK","tool_calls":[{}]}]}`, false},
		{"image with text", "chat", `{"messages":[{"role":"user","content":[{"type":"text","text":"say OK"},{"type":"image_url","image_url":{"url":"x"}}]}]}`, false},
		{"audio with text", "chat", `{"messages":[{"role":"user","content":[{"type":"text","text":"say OK"},{"type":"input_audio","input_audio":{"data":"abc","format":"wav"}}]}]}`, false},
		{"responses continuation", "responses", `{"input":"say OK","previous_response_id":"resp1"}`, false},
		{"responses conversation", "responses", `{"input":"say OK","conversation":"conv1"}`, false},
		{"responses item reference", "responses", `{"input":[{"type":"item_reference","id":"msg1"},{"role":"user","content":"say OK"}]}`, false},
		{"responses function output", "responses", `{"input":[{"role":"user","content":"say OK"},{"type":"function_call_output","call_id":"x","output":"ok"}]}`, false},
		{"responses reasoning", "responses", `{"input":[{"type":"reasoning","summary":[]},{"role":"user","content":"say OK"}]}`, false},
		{"responses file", "responses", `{"input":[{"role":"user","content":[{"type":"input_text","text":"say OK"},{"type":"input_file","file_id":"file1"}]}]}`, false},
		{"responses prompt reference", "responses", `{"input":"say OK","prompt":{"id":"pmpt1"}}`, false},
		{"claude history", "messages", `{"messages":[{"role":"assistant","content":"hello"},{"role":"user","content":"say OK"}]}`, false},
		{"claude tool result", "messages", `{"messages":[{"role":"user","content":[{"type":"text","text":"say OK"},{"type":"tool_result","tool_use_id":"x","content":"done"}]}]}`, false},
		{"claude container", "messages", `{"messages":[{"role":"user","content":"say OK"}],"container":"container1"}`, false},
		{"system only", "chat", `{"messages":[{"role":"system","content":"say OK"}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := parseProbeRequest(t, tc.protocol, tc.body)
			require.Equal(t, tc.match, MatchProbeGuardRequest(request, matcherConfig()) != nil)
		})
	}
}

func TestProbeGuardCustomRules(t *testing.T) {
	cfg := matcherConfig()
	cfg.TextRules = "CUSTOM CHECK\n自定义确认"
	req := parseProbeRequest(t, "chat", `{"messages":[{"role":"user","content":"custom check"}]}`)
	require.NotNil(t, MatchProbeGuardRequest(req, cfg))
	require.Nil(t, MatchProbeGuardText("say OK", cfg.TextRules))
	cfg.TextRules = ""
	require.Nil(t, MatchProbeGuardRequest(req, cfg))
	require.Nil(t, MatchProbeGuardText("say OK", cfg.TextRules))
	require.Nil(t, MatchProbeGuardText(strings.Repeat(" ", maxProbeTextBytes)+"say OK", setting.DefaultProbeGuardTextRules))
}

func TestProbePingStructureAcrossProtocols(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []any{"ok"}}
	for _, protocol := range []string{"chat", "responses", "messages"} {
		t.Run(protocol, func(t *testing.T) {
			body := map[string]any{}
			switch protocol {
			case "chat":
				body["messages"] = []any{map[string]any{"role": "user", "content": probePingPrompt}}
				body["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "probe_ping", "parameters": schema}}}
				body["tool_choice"] = "required"
			case "responses":
				body["input"] = probePingPrompt
				body["tools"] = []any{map[string]any{"type": "function", "name": "probe_ping", "parameters": schema}}
				body["tool_choice"] = "required"
			case "messages":
				body["messages"] = []any{map[string]any{"role": "user", "content": probePingPrompt}}
				body["tools"] = []any{map[string]any{"name": "probe_ping", "input_schema": schema}}
				body["tool_choice"] = map[string]any{"type": "any"}
			}
			encode := func() dto.Request {
				data, err := common.Marshal(body)
				require.NoError(t, err)
				return parseProbeRequest(t, protocol, string(data))
			}
			require.Equal(t, "tool:probe_ping", MatchProbeGuardRequest(encode(), matcherConfig()).RuleID)
			cfg := matcherConfig()
			cfg.ProbePingEnabled = false
			require.Nil(t, MatchProbeGuardRequest(encode(), cfg))
			originalChoice := body["tool_choice"]
			body["tool_choice"] = "auto"
			require.Nil(t, MatchProbeGuardRequest(encode(), matcherConfig()))
			body["tool_choice"] = originalChoice
			originalTools := body["tools"]
			body["tools"] = append(body["tools"].([]any), map[string]any{"name": "read"})
			require.Nil(t, MatchProbeGuardRequest(encode(), matcherConfig()))
			body["tools"] = originalTools
			if protocol == "responses" {
				body["input"] = "Use probe_ping to test my application"
			} else {
				body["messages"] = []any{map[string]any{"role": "user", "content": "Use probe_ping to test my application"}}
			}
			require.Nil(t, MatchProbeGuardRequest(encode(), matcherConfig()))
		})
	}
	for _, invalid := range []any{nil, map[string]any{"type": "object"}, map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "string"}}, "required": []any{"ok"}}, map[string]any{"type": "object", "properties": schema["properties"], "required": []any{}}} {
		require.False(t, isProbePingSchema(invalid))
	}
}

// Optional local replay: captures stay outside the repository and are never changed.
func TestProbeGuardCaptureReplay(t *testing.T) {
	root := os.Getenv("PROBE_GUARD_REPLAY_DIR")
	if root == "" {
		t.Skip("set PROBE_GUARD_REPLAY_DIR to replay local captures")
	}
	var matched []int
	count := 0
	for _, batch := range []string{"1", "2"} {
		paths, err := filepath.Glob(filepath.Join(root, batch, "raw", "*.json"))
		require.NoError(t, err)
		require.NotEmpty(t, paths)
		for _, path := range paths {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var capture struct {
				ID   int    `json:"id"`
				Body string `json:"client_request_body"`
				Path string `json:"request_path"`
			}
			require.NoError(t, common.Unmarshal(data, &capture))
			protocol := "chat"
			if strings.HasPrefix(capture.Path, "/v1/responses") {
				protocol = "responses"
			} else if strings.HasPrefix(capture.Path, "/v1/messages") {
				protocol = "messages"
			}
			request := parseProbeRequest(t, protocol, capture.Body)
			if MatchProbeGuardRequest(request, matcherConfig()) != nil {
				matched = append(matched, capture.ID)
			}
			count++
		}
	}
	sort.Ints(matched)
	t.Logf("replayed=%d matched=%d ids=%v", count, len(matched), matched)
	require.ElementsMatch(t, []int{35709, 36083, 36085, 36087, 36089, 36090, 36092, 36098, 36118, 36119, 36207, 37127, 37128, 37679, 37681, 37684, 37685, 35285, 35286, 35365, 35370, 35375, 35376, 36035}, matched)
}

func BenchmarkProbeGuardMatch(b *testing.B) {
	cfg := matcherConfig()
	for _, tc := range []struct{ name, body string }{
		{"single", `{"messages":[{"role":"user","content":"Reply with exactly OK"}]}`},
		{"normal", `{"messages":[{"role":"user","content":"Please review this function for bugs."}]}`},
		{"tool_history", `{"messages":[{"role":"user","content":"say OK"},{"role":"assistant","content":"` + strings.Repeat("history ", 10000) + `"},{"role":"tool","content":"result"}]}`},
	} {
		b.Run(tc.name, func(b *testing.B) {
			request := parseProbeRequest(b, "chat", tc.body)
			MatchProbeGuardRequest(request, cfg)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				MatchProbeGuardRequest(request, cfg)
			}
		})
	}
}
