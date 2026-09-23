package opencode

// Adapted from piexian/new-api (AGPL-3.0), commit afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f.
// https://github.com/piexian/new-api/tree/afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f/relay/channel/opencode

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestFreeGuardPreservesSplitDeclaredNameWithCorePrefix(t *testing.T) {
	body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read"}}]},"finish_reason":null}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" + "data: [DONE]\n\n"
	for _, legitimate := range []bool{true, false} {
		fixture := body
		if !legitimate {
			fixture = strings.ReplaceAll(body, `"name":"_file",`, "")
		}
		stream := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(fixture)), constant.OpenCodeEndpointChat, map[string]bool{"read": true}, time.Second)
		stream.declared = map[string]bool{"read_file": true}
		result, err := collapseFreeStream(stream)
		require.NoError(t, stream.Close())
		if legitimate {
			require.NoError(t, err)
			require.Equal(t, "read_file", gjson.GetBytes(result, "choices.0.message.tool_calls.0.function.name").String())
		} else {
			require.ErrorContains(t, err, "compatibility-only")
			require.Empty(t, result)
		}
	}
}

func TestFreeStreamFailureDoesNotEmitSuccessTerminator(t *testing.T) {
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = old })
	for _, fixture := range []string{
		strings.ReplaceAll(freeChatFixture, "data: [DONE]\n\n", ""),
		strings.Split(freeChatFixture, "data: [DONE]")[0] + `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"bash","arguments":"{}"}}]}}]}` + "\n\n",
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeUnknown, IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenCode, UpstreamModelName: "mimo-v2.5-free"}}
		stream := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(fixture)), constant.OpenCodeEndpointChat, map[string]bool{"bash": true}, time.Second)
		adaptor := &Adaptor{freeRequestMode: constant.OpenCodeEndpointChat, freeResponseStream: stream}
		_, apiErr := adaptor.DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: stream}, info)
		require.NotNil(t, apiErr)
		require.Equal(t, 502, apiErr.StatusCode)
		require.NotContains(t, recorder.Body.String(), "[DONE]")
		require.NotContains(t, recorder.Body.String(), `"name":"bash"`)
		if strings.Contains(recorder.Body.String(), "event: error") {
			before := recorder.Body.String()
			c.JSON(502, gin.H{"unexpected": "controller JSON"})
			require.Equal(t, before, recorder.Body.String())
		}
	}
}

func TestFreeResponsesPreservesFinalFieldsAndIncompleteStatus(t *testing.T) {
	fixture := strings.ReplaceAll(freeResponsesFixture, "response.completed", "response.incomplete")
	fixture = strings.ReplaceAll(fixture, `"status":"completed","created_at"`, `"status":"incomplete","created_at"`)
	stream := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(fixture)), constant.OpenCodeEndpointResponses, nil, time.Second)
	defer stream.Close()
	body, err := collapseFreeStream(stream)
	require.NoError(t, err)
	require.Equal(t, "incomplete", gjson.GetBytes(body, "status").String())
	require.Equal(t, "false", gjson.GetBytes(body, "extra").Raw)
	require.Equal(t, int64(1), gjson.GetBytes(body, "usage.input_tokens_details.cached_tokens").Int())
}

func TestFreeStreamSizeLimits(t *testing.T) {
	for _, body := range []string{"data: " + strings.Repeat("x", freeEventLimit), strings.Repeat(": comment\n", freeEventLimit/10+1)} {
		stream := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(body)), constant.OpenCodeEndpointChat, nil, time.Second)
		_, err := io.ReadAll(stream)
		require.Error(t, err)
		require.NoError(t, stream.Close())
	}
}

func TestFreePassThroughStaysUnmodified(t *testing.T) {
	info := newRelayInfo("mimo-v2.5-free")
	adaptor := &Adaptor{}
	adaptor.Init(info)
	require.True(t, adaptor.needsFreeCompatibility(info))
	info.ChannelSetting.PassThroughBodyEnabled = true
	require.False(t, adaptor.needsFreeCompatibility(info))
	info.ChannelSetting.PassThroughBodyEnabled = false
	info.ChannelType = constant.ChannelTypeOpenCodeGo
	require.False(t, adaptor.needsFreeCompatibility(info))
	info.ChannelType = constant.ChannelTypeOpenCode
	settings := model_setting.GetGlobalSettings()
	before := settings.PassThroughRequestEnabled
	t.Cleanup(func() { settings.PassThroughRequestEnabled = before })
	settings.PassThroughRequestEnabled = true
	require.False(t, adaptor.needsFreeCompatibility(info))
}

func TestFreeResponsesGuardCoversNativeAndConvertedClients(t *testing.T) {
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = old })
	for _, converted := range []bool{false, true} {
		for _, fixture := range []string{
			strings.Split(freeResponsesFixture, "event: response.completed")[0],
			`data: {"type":"response.output_item.added","item":{"type":"function_call","name":"bash","arguments":"{}"}}` + "\n\n",
		} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := newRelayInfo("muse-spark-1.3-contributor-free")
			info.IsStream, info.DisablePing = true, true
			info.RelayMode, info.RelayFormat = relayconstant.RelayModeResponses, types.RelayFormatOpenAIResponses
			stream := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(fixture)), constant.OpenCodeEndpointResponses, map[string]bool{"bash": true}, time.Second)
			a := &Adaptor{freeResponseStream: stream}
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: stream}
			var apiErr *types.NewAPIError
			if converted {
				info.RelayFormat = types.RelayFormatOpenAI
				_, apiErr = a.DoResponsesToChatResponse(c, resp, info)
			} else {
				_, apiErr = a.DoResponse(c, resp, info)
			}
			require.NotNil(t, apiErr)
			require.NotContains(t, recorder.Body.String(), "[DONE]")
			require.NotContains(t, recorder.Body.String(), "response.completed")
			require.NotContains(t, recorder.Body.String(), `"name":"bash"`)
		}
	}
}

func TestFreeClaudeAgentStream(t *testing.T) {
	service.InitHttpClient()
	old := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = old })
	for _, model := range []string{"mimo-v2.5-free", "muse-spark-1.3-contributor-free"} {
		t.Run(model, func(t *testing.T) {
			responses := strings.HasPrefix(model, "muse-")
			sent := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				sent <- body
				w.Header().Set("Content-Type", "text/event-stream")
				fixture := freeChatFixture
				if responses {
					fixture = freeResponsesFixture
				}
				_, _ = io.WriteString(w, fixture)
			}))
			defer server.Close()
			info := newRelayInfo(model)
			info.ChannelBaseUrl = server.URL
			info.RelayFormat = types.RelayFormatClaude
			info.IsStream, info.DisablePing = true, true
			info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			var req dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"`+model+`","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"Say OK"}],"tools":[{"name":"read","description":"caller's tool","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}]}`, &req))
			a := &Adaptor{}
			a.Init(info)
			var converted any
			var err error
			if responses {
				chat, convErr := service.ClaudeToOpenAIRequest(req, info)
				require.NoError(t, convErr)
				resp, convErr := service.ChatCompletionsRequestToResponsesRequest(chat)
				require.NoError(t, convErr)
				info.RelayMode = relayconstant.RelayModeResponses
				converted, err = a.ConvertOpenAIResponsesRequest(c, info, *resp)
			} else {
				converted, err = a.ConvertClaudeRequest(c, info, &req)
			}
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			raw, err := a.DoRequest(c, info, strings.NewReader(string(body)))
			require.NoError(t, err)
			outbound := <-sent
			path := "tools.0.function.parameters.properties.path.type"
			if responses {
				path = "tools.0.parameters.properties.path.type"
			}
			require.Equal(t, "string", gjson.GetBytes(outbound, path).String())
			require.Len(t, gjson.GetBytes(outbound, "tools").Array(), 5)
			var apiErr *types.NewAPIError
			if responses {
				_, apiErr = a.DoResponsesToChatResponse(c, raw.(*http.Response), info)
			} else {
				_, apiErr = a.DoResponse(c, raw.(*http.Response), info)
			}
			require.Nil(t, apiErr)
			require.Contains(t, recorder.Body.String(), "message_stop")
			require.Contains(t, recorder.Body.String(), "text_delta")
			require.NotContains(t, recorder.Body.String(), "event: error")
			var text strings.Builder
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if strings.HasPrefix(line, "data:") {
					text.WriteString(gjson.Get(strings.TrimSpace(line[5:]), "delta.text").String())
				}
			}
			require.Equal(t, "OK", text.String())
		})
	}
}

func TestFreeResponsesBufferPreservesOutputSnapshots(t *testing.T) {
	fixture := `data: {"type":"response.output_item.added","output_index":0,"item":{"id":"msg_a","type":"message","role":"assistant","content":[]}}

data: {"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}

data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"OK"}

data: {"type":"response.output_item.done","output_index":1,"item":{"id":"call_a","type":"function_call","name":"read","arguments":"{\"n\":0}","extra":false}}

data: {"type":"response.completed","response":{"id":"resp_a","status":"completed","output":[],"extra":9007199254740993,"usage":{"input_tokens":0,"output_tokens":2}}}

`
	stream := newFreeStream(context.Background(), io.NopCloser(strings.NewReader(fixture)), constant.OpenCodeEndpointResponses, map[string]bool{"bash": true}, time.Second)
	defer stream.Close()
	body, err := collapseFreeStream(stream)
	require.NoError(t, err)
	require.Equal(t, "OK", gjson.GetBytes(body, "output.0.content.0.text").String())
	require.Equal(t, "msg_a", gjson.GetBytes(body, "output.0.id").String())
	require.Equal(t, "read", gjson.GetBytes(body, "output.1.name").String())
	require.Equal(t, `{"n":0}`, gjson.GetBytes(body, "output.1.arguments").String())
	require.Equal(t, "false", gjson.GetBytes(body, "output.1.extra").Raw)
	require.Equal(t, "9007199254740993", gjson.GetBytes(body, "extra").Raw)
	require.Equal(t, "0", gjson.GetBytes(body, "usage.input_tokens").Raw)
	_, err = collapseFreeResponses([]byte(strings.Replace(fixture, `"content_index":0`, `"content_index":9999999999`, 1)))
	require.ErrorContains(t, err, "index exceeds buffer limit")
}

func TestFreeUpstreamRejectionPreserved(t *testing.T) {
	service.InitHttpClient()
	body := `{"type":"error","error":{"type":"FreeTierError","message":"upstream rejection"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	info := newRelayInfo("mimo-v2.5-free")
	info.ChannelBaseUrl = server.URL
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	a := &Adaptor{}
	a.Init(info)
	raw, err := a.DoRequest(c, info, strings.NewReader(`{"messages":[]}`))
	require.NoError(t, err)
	resp := raw.(*http.Response)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, body, string(got))
}

func TestFreeRequestCancellationBeforeUpstreamHeaders(t *testing.T) {
	service.InitHttpClient()
	entered := make(chan struct{})
	exited := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
		close(exited)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeUnknown, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenCode, ChannelBaseUrl: server.URL, UpstreamModelName: "mimo-v2.5-free", ApiKey: "test"}}
	a := &Adaptor{}
	a.Init(info)
	result := make(chan error, 1)
	go func() {
		_, err := a.DoRequest(c, info, strings.NewReader(`{"model":"mimo-v2.5-free","messages":[]}`))
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream was not reached")
	}
	cancel()
	select {
	case err := <-result:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("request was not cancelled")
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream context was not cancelled")
	}
}
