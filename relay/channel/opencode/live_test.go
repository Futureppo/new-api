package opencode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type liveClientHeaderResult struct {
	Model         string            `json:"model"`
	Mode          string            `json:"mode"`
	Endpoint      string            `json:"endpoint"`
	Status        int               `json:"status"`
	Headers       map[string]string `json:"headers,omitempty"`
	Error         string            `json:"error,omitempty"`
	Text          string            `json:"text,omitempty"`
	Terminal      bool              `json:"terminal"`
	Usage         *dto.Usage        `json:"usage,omitempty"`
	Skipped       string            `json:"skipped,omitempty"`
	ToolName      string            `json:"tool_name,omitempty"`
	ToolArguments string            `json:"tool_arguments,omitempty"`
}

// Explicitly opt-in. A supplied key is read only from the process environment.
// Only free models are requested; authorization headers are never logged.
func TestOpenCodeLiveClientIdentifiers(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE_TEST") != "1" {
		t.Skip("set OPENCODE_LIVE_TEST=1 to test the live OpenCode free models")
	}
	apiKey := os.Getenv("OPENCODE_LIVE_API_KEY")
	if apiKey == "" {
		apiKey = "public"
	}
	models := []string{"mimo-v2.5-free", "muse-spark-1.3-contributor-free"}
	if os.Getenv("OPENCODE_LIVE_ALL_FREE") == "1" {
		var err error
		models, err = liveFreeModels(apiKey)
		require.NoError(t, err)
		require.NotEmpty(t, models)
	}
	if model := os.Getenv("OPENCODE_LIVE_MODEL"); model != "" {
		require.True(t, isLiveFreeModel(model), "live test accepts only free models")
		models = []string{model}
	}
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var results []liveClientHeaderResult
	t.Cleanup(func() {
		path := os.Getenv("OPENCODE_LIVE_REPORT")
		if path == "" {
			return
		}
		data, err := common.Marshal(results)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0600))
	})

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 120
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			result := liveClientHeaderResult{Model: model, Mode: "agent_stream"}
			defer func() { results = append(results, result) }()
			if strings.HasPrefix(model, "jev-") {
				result.Skipped = "native SystemOne model; no streaming Chat/Responses endpoint"
				t.Skip(result.Skipped)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			info := newRelayInfo(model)
			info.ApiKey = apiKey
			info.IsStream = true
			info.DisablePing = true
			info.ShouldIncludeUsage = true
			info.RelayFormat = types.RelayFormatOpenAI
			info.HeadersOverride = map[string]any{"*": ""}
			payload := map[string]any{"model": model, "stream": true}
			toolTest := os.Getenv("OPENCODE_LIVE_TOOL") == "1"
			prompt := "Reply only with OK. Do not call any tools."
			if toolTest {
				prompt = "Call the read tool with path opencode-test.txt. Wait for its result before replying."
				result.Mode = "agent_stream_tool"
			}
			if endpoint(info) == constant.OpenCodeEndpointResponses {
				info.RelayMode = relayconstant.RelayModeResponses
				info.RelayFormat = types.RelayFormatOpenAIResponses
				payload["input"] = prompt
				payload["max_output_tokens"] = 1024
				payload["store"] = false
			} else {
				payload["messages"] = []map[string]string{{"role": "user", "content": prompt}}
				payload["max_tokens"] = 1024
			}
			if toolTest {
				function := map[string]any{"name": "read", "description": "Read the specified test file.", "parameters": map[string]any{
					"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"},
				}}
				tool := map[string]any{"type": "function", "function": function}
				if endpoint(info) == constant.OpenCodeEndpointResponses {
					function["type"] = "function"
					tool = function
				}
				payload["tools"] = []any{tool}
			}
			body, err := common.Marshal(payload)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set("User-Agent", "claude-code/2.0")
			adaptor := &Adaptor{}
			adaptor.Init(info)
			result.Endpoint, err = adaptor.GetRequestURL(info)
			require.NoError(t, err)
			redact := func(err string) string { return strings.ReplaceAll(err, apiKey, "[REDACTED]") }
			raw, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
			if err != nil {
				result.Error = redact(err.Error())
				t.Fatal(result.Error)
			}
			response := raw.(*http.Response)
			defer response.Body.Close()
			result.Status = response.StatusCode
			result.Headers = make(map[string]string)
			for _, name := range []string{"User-Agent", "x-opencode-client", "x-opencode-session", "x-opencode-request", "x-opencode-project"} {
				result.Headers[name] = response.Request.Header.Get(name)
				require.NotEmpty(t, result.Headers[name], "missing outbound %s", name)
			}
			require.Equal(t, defaultUserAgent, result.Headers["User-Agent"])
			if response.StatusCode != http.StatusOK {
				data, _ := io.ReadAll(io.LimitReader(response.Body, 2000))
				result.Error = redact(string(data))
				t.Fatalf("status=%d body=%s", result.Status, result.Error)
			}
			usage, apiErr := adaptor.DoResponse(c, response, info)
			if apiErr != nil {
				result.Error = redact(apiErr.Error())
				t.Error(result.Error)
			}
			if usage != nil {
				result.Usage, _ = usage.(*dto.Usage)
			}
			data := recorder.Body.Bytes()
			if liveResponseHasError(data) && result.Error == "" {
				result.Error = "downstream stream contains an error"
				t.Error(result.Error)
			}
			var content strings.Builder
			for _, line := range bytes.Split(data, []byte("\n")) {
				if !bytes.HasPrefix(line, []byte("data:")) {
					continue
				}
				event := bytes.TrimSpace(line[5:])
				if bytes.Equal(event, []byte("[DONE]")) {
					result.Terminal = true
					continue
				}
				root := gjson.ParseBytes(event)
				switch root.Get("type").String() {
				case "response.completed", "response.incomplete":
					result.Terminal = true
				case "response.output_text.delta":
					content.WriteString(root.Get("delta").String())
				case "response.output_item.added", "response.output_item.done":
					if root.Get("item.type").String() == "function_call" {
						result.ToolName = root.Get("item.name").String()
						if args := root.Get("item.arguments").String(); args != "" {
							result.ToolArguments = args
						}
					}
				case "response.function_call_arguments.delta":
					result.ToolArguments += root.Get("delta").String()
				}
				for _, choice := range root.Get("choices").Array() {
					content.WriteString(choice.Get("delta.content").String())
					for _, tool := range choice.Get("delta.tool_calls").Array() {
						result.ToolName += tool.Get("function.name").String()
						result.ToolArguments += tool.Get("function.arguments").String()
					}
				}
			}
			result.Text = content.String()
			if len(result.Text) > 1000 {
				result.Text = result.Text[:1000]
			}
			if !result.Terminal && result.Error == "" {
				result.Error = "missing terminal event"
			}
			if !toolTest && result.Text == "" && result.Error == "" {
				result.Error = "no output text (possibly exhausted output token budget)"
			}
			if toolTest {
				require.Equal(t, "read", result.ToolName)
				require.Equal(t, "opencode-test.txt", gjson.Get(result.ToolArguments, "path").String())
			}
			if result.Error != "" {
				t.Error(result.Error)
			}
			require.NotNil(t, result.Usage)
			t.Logf("model=%s status=%d terminal=%t text=%q", model, result.Status, result.Terminal, result.Text)
		})
	}
}

func liveResponseHasError(body []byte) bool {
	check := func(data []byte) bool {
		var event struct {
			Type     string `json:"type"`
			Error    any    `json:"error"`
			Response *struct {
				Error any `json:"error"`
			} `json:"response"`
		}
		if common.Unmarshal(data, &event) != nil {
			return false
		}
		return event.Error != nil || event.Type == "error" || event.Type == "response.failed" ||
			(event.Response != nil && event.Response.Error != nil)
	}
	if check(body) {
		return true
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data:")) && check(bytes.TrimSpace(line[5:])) {
			return true
		}
	}
	return false
}

func isLiveFreeModel(model string) bool {
	// big-pickle is listed with zero input/output cost in the OpenCode catalog.
	return strings.HasSuffix(model, "-free") || model == "big-pickle"
}

func liveFreeModels(apiKey string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://opencode.ai/zen/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models endpoint returned %d", res.StatusCode)
	}
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := common.DecodeJson(io.LimitReader(res.Body, 1024*1024), &catalog); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var models []string
	for _, model := range catalog.Data {
		if isLiveFreeModel(model.ID) && !seen[model.ID] {
			models = append(models, model.ID)
			seen[model.ID] = true
		}
	}
	sort.Strings(models)
	return models, nil
}
