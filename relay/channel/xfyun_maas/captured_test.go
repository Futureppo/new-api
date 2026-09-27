package xfyun_maas

import (
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestCapturedTextProtocols(t *testing.T) {
	for _, protocol := range []struct {
		name   string
		format types.RelayFormat
		mode   int
	}{
		{"chat", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions},
		{"responses", types.RelayFormatOpenAIResponses, relayconstant.RelayModeResponses},
		{"messages", types.RelayFormatClaude, relayconstant.RelayModeUnknown},
		{"vision", types.RelayFormatOpenAI, relayconstant.RelayModeChatCompletions},
	} {
		for _, suffix := range []string{".json", ".sse", "-tools.json", "-tools.sse", "-thinking.sse"} {
			if protocol.name == "vision" && strings.HasPrefix(suffix, "-") {
				continue
			}
			t.Run(protocol.name+suffix, func(t *testing.T) {
				data, err := os.ReadFile("testdata/" + protocol.name + suffix)
				require.NoError(t, err)
				stream := strings.HasSuffix(suffix, ".sse")
				c, w := testContext()
				info := testInfo(t, protocol.format, protocol.mode, stream)
				u, apiErr := (&Adaptor{}).DoResponse(c, response(string(data), stream), info)
				require.Nil(t, apiErr)
				require.NotContains(t, w.Body.String(), `"error"`)
				usage := u.(*dto.Usage)
				require.Positive(t, usage.PromptTokens)
				require.Positive(t, usage.CompletionTokens)
				switch {
				case strings.Contains(suffix, "tools"):
					require.Contains(t, w.Body.String(), "get_weather")
					require.Contains(t, w.Body.String(), "Paris")
					require.Equal(t, 12, usage.CompletionTokens)
					if protocol.name != "chat" || stream {
						require.Equal(t, 64, usage.PromptTokensDetails.CachedTokens)
					}
					if protocol.name == "messages" {
						require.Equal(t, 41, usage.PromptTokens)
					} else {
						require.Equal(t, 105, usage.PromptTokens)
					}
				case strings.Contains(suffix, "thinking"):
					marker := map[string]string{"chat": "reasoning_content", "responses": "response.reasoning_summary_text.delta", "messages": "thinking_delta"}[protocol.name]
					require.Contains(t, w.Body.String(), marker)
					if protocol.name != "messages" {
						require.Positive(t, usage.CompletionTokenDetails.ReasoningTokens)
					}
				case protocol.name == "vision":
					require.Contains(t, w.Body.String(), "123")
					require.Equal(t, 293, usage.PromptTokens)
					require.Equal(t, 5, usage.CompletionTokens)
				default:
					require.Contains(t, w.Body.String(), "OK")
					require.Equal(t, 21, usage.PromptTokens)
					require.Equal(t, 2, usage.CompletionTokens)
				}
				if stream {
					require.True(t, info.StreamStatus.IsNormalEnd())
					require.False(t, info.StreamStatus.HasErrors())
				}
			})
		}
	}
}

func TestCapturedEmbeddingsRerankAndImages(t *testing.T) {
	for _, name := range []string{"embedding", "rerank", "image"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + name + ".json")
			require.NoError(t, err)
			a := &Adaptor{}
			mode := relayconstant.RelayModeImagesGenerations
			if name == "embedding" {
				mode = relayconstant.RelayModeEmbeddings
				_, err = a.ConvertEmbeddingRequest(nil, nil, dto.EmbeddingRequest{Input: []string{"hello world", "hello world"}, EncodingFormat: "base64"})
			} else if name == "rerank" {
				mode = relayconstant.RelayModeRerank
				_, err = a.ConvertRerankRequest(nil, 0, dto.RerankRequest{Query: "What is the capital of France?", Documents: []any{"Berlin is in Germany.", "Paris is the capital of France.", "The sky is blue."}, TopN: common.GetPointer(1), ReturnDocuments: common.GetPointer(true)})
			}
			require.NoError(t, err)
			c, w := testContext()
			u, apiErr := a.DoResponse(c, response(string(data), false), testInfo(t, types.RelayFormatOpenAI, mode, false))
			require.Nil(t, apiErr)
			if name == "rerank" {
				require.Equal(t, 254, u.(*dto.Usage).PromptTokens)
				require.Contains(t, w.Body.String(), "Paris is the capital of France.")
			} else if name == "embedding" {
				require.Equal(t, 6, u.(*dto.Usage).PromptTokens)
			} else {
				require.Contains(t, w.Body.String(), `"b64_json"`)
			}
		})
	}
	for _, name := range []string{"model-field-error", "image-schema-error"} {
		data, err := os.ReadFile("testdata/" + name + ".json")
		require.NoError(t, err)
		status := 200
		if name == "model-field-error" {
			status = 400
		}
		apiErr := businessError(data, status)
		require.NotNil(t, apiErr)
		require.Equal(t, 400, apiErr.StatusCode)
	}
}
