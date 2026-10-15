package openai

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func openRouterDecisionsResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	if info.IsStream {
		return nil, decisionsBadResponse("OpenRouter Decisions returned unexpected SSE")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, decisionsBadResponse("failed to read OpenRouter Decisions response")
	}
	var result struct {
		Model   string                     `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Error   json.RawMessage            `json:"error"`
		Usage   *struct {
			Input  *int `json:"input_tokens"`
			Output *int `json:"output_tokens"`
			Cost   any  `json:"cost"`
		} `json:"usage"`
	}
	if common.Unmarshal(body, &result) != nil {
		return nil, decisionsBadResponse("invalid OpenRouter Decisions response")
	}
	if apiErr := openRouterResponseError(result.Error); apiErr != nil {
		return nil, apiErr
	}
	if strings.TrimSpace(result.Model) == "" || len(result.Answers) == 0 || result.Usage == nil || result.Usage.Input == nil || result.Usage.Output == nil {
		return nil, decisionsBadResponse("OpenRouter Decisions requires model, named answers and token usage")
	}
	input, output := *result.Usage.Input, *result.Usage.Output
	if input < 0 || output < 0 || input > math.MaxInt-output {
		return nil, decisionsBadResponse("invalid OpenRouter Decisions usage")
	}
	usage := &dto.Usage{PromptTokens: input, CompletionTokens: output, TotalTokens: input + output, UsageSource: "upstream", Cost: result.Usage.Cost}
	info.SetFirstResponseTime()
	c.Set("openrouter_response_committed", true)
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}
