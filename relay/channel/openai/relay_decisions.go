package openai

import (
	"encoding/json"
	"errors"
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

func decisionsBadResponse(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func OpenAIDecisionsHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, decisionsBadResponse("empty Decisions response")
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, decisionsBadResponse("failed to read Decisions response")
	}
	var result struct {
		Model   string            `json:"model"`
		Answers []json.RawMessage `json:"answers"`
		Error   json.RawMessage   `json:"error"`
		Usage   *struct {
			InputTokens  *int `json:"input_tokens"`
			OutputTokens *int `json:"output_tokens"`
			TotalTokens  *int `json:"total_tokens"`
			InputDetails *struct {
				CachedTokens     *int `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails *struct {
				ReasoningTokens *int `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := common.Unmarshal(body, &result); err != nil || strings.TrimSpace(result.Model) == "" || len(result.Answers) == 0 || result.Usage == nil || result.Usage.InputTokens == nil || result.Usage.OutputTokens == nil || result.Usage.TotalTokens == nil {
		return nil, decisionsBadResponse("invalid Decisions response: model, answers and token usage are required")
	}
	if len(result.Error) > 0 && string(result.Error) != "null" {
		return nil, decisionsBadResponse("Decisions returned an error payload")
	}
	for _, raw := range result.Answers {
		if !validDecisionAnswer(raw) {
			return nil, decisionsBadResponse("invalid Decisions answer")
		}
	}
	input, output, total := *result.Usage.InputTokens, *result.Usage.OutputTokens, *result.Usage.TotalTokens
	if input < 0 || output < 0 || input > math.MaxInt-output || total != input+output {
		return nil, decisionsBadResponse("invalid Decisions token usage")
	}
	usage := &dto.Usage{PromptTokens: input, CompletionTokens: output, TotalTokens: total}
	if details := result.Usage.InputDetails; details != nil {
		if details.CachedTokens != nil {
			if *details.CachedTokens < 0 || *details.CachedTokens > input {
				return nil, decisionsBadResponse("invalid Decisions cached token usage")
			}
			usage.PromptTokensDetails.CachedTokens = *details.CachedTokens
		}
		if details.CacheWriteTokens != nil {
			if *details.CacheWriteTokens < 0 || *details.CacheWriteTokens > input-usage.PromptTokensDetails.CachedTokens {
				return nil, decisionsBadResponse("invalid Decisions cache write token usage")
			}
			usage.PromptTokensDetails.CacheWriteTokens = details.CacheWriteTokens
		}
	}
	if details := result.Usage.OutputDetails; details != nil && details.ReasoningTokens != nil {
		if *details.ReasoningTokens < 0 || *details.ReasoningTokens > output {
			return nil, decisionsBadResponse("invalid Decisions reasoning token usage")
		}
		usage.CompletionTokenDetails.ReasoningTokens = *details.ReasoningTokens
	}
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}

func validDecisionValue(raw json.RawMessage) bool {
	var value any
	if common.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value.(type) {
	case string, bool:
		return true
	}
	return false
}

func validDecisionProbability(p *float64) bool {
	return p != nil && *p >= 0 && *p <= 1
}

func validDecisionAnswer(raw json.RawMessage) bool {
	var answer struct {
		Type          string          `json:"type"`
		Name          *string         `json:"name"`
		Probability   *float64        `json:"probability"`
		Confidence    *float64        `json:"confidence"`
		Score         *float64        `json:"score"`
		Choice        json.RawMessage `json:"choice"`
		Probabilities []struct {
			Value       json.RawMessage `json:"value"`
			Probability *float64        `json:"probability"`
			Label       *string         `json:"label"`
		} `json:"probabilities"`
	}
	if common.Unmarshal(raw, &answer) != nil {
		return false
	}
	switch answer.Type {
	case "refusal":
		return true
	case "predicate":
		return validDecisionProbability(answer.Probability)
	case "choice", "score":
		if !validDecisionProbability(answer.Confidence) || len(answer.Probabilities) == 0 {
			return false
		}
		if answer.Type == "choice" && !validDecisionValue(answer.Choice) {
			return false
		}
		if answer.Type == "score" && (answer.Score == nil || *answer.Score < 0) {
			return false
		}
		for _, option := range answer.Probabilities {
			if !validDecisionProbability(option.Probability) {
				return false
			}
			if answer.Type == "choice" {
				if !validDecisionValue(option.Value) {
					return false
				}
			} else {
				var index *int
				if common.Unmarshal(option.Value, &index) != nil || index == nil || *index < 0 || option.Label == nil {
					return false
				}
			}
		}
		return true
	default:
		return false
	}
}
