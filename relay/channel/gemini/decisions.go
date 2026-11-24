package gemini

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func invalidDecisionsRequest() *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New("Gemini decision models require synchronous POST /v1beta/decisions or /v1/decisions with input and named questions"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func decisionsBadResponse(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func GeminiDecisionsHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, decisionsBadResponse("empty Gemini Decisions response")
	}
	defer service.CloseResponseBodyGracefully(resp)
	if strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return nil, decisionsBadResponse("Gemini Decisions returned unexpected SSE")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, decisionsBadResponse("failed to read Gemini Decisions response")
	}
	var result struct {
		Answers        map[string]json.RawMessage `json:"answers"`
		Error          json.RawMessage            `json:"error"`
		PromptFeedback *struct {
			BlockReason string `json:"block_reason"`
		} `json:"prompt_feedback"`
		Usage *struct {
			Input  *int `json:"total_input_tokens"`
			Output *int `json:"total_output_tokens"`
			Total  *int `json:"total_tokens"`
		} `json:"usage"`
	}
	if common.Unmarshal(body, &result) != nil {
		return nil, decisionsBadResponse("invalid Gemini Decisions response")
	}
	if raw := bytes.TrimSpace(result.Error); len(raw) > 0 && string(raw) != "null" {
		return nil, decisionsBadResponse("Gemini Decisions returned an error payload")
	}
	blocked := result.PromptFeedback != nil && result.PromptFeedback.BlockReason != ""
	if len(result.Answers) == 0 && !blocked {
		return nil, decisionsBadResponse("Gemini Decisions response requires named answers or prompt feedback")
	}
	usage := &dto.Usage{UsageSource: "upstream"}
	if result.Usage == nil {
		if !blocked {
			return nil, decisionsBadResponse("Gemini Decisions response requires token usage")
		}
	} else {
		u := result.Usage
		if u.Input == nil || u.Output == nil || *u.Input < 0 || *u.Output != 0 || (u.Total != nil && *u.Total != *u.Input) {
			return nil, decisionsBadResponse("invalid Gemini Decisions token usage")
		}
		usage.PromptTokens, usage.TotalTokens = *u.Input, *u.Input
	}
	if blocked {
		common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_block_reason="+result.PromptFeedback.BlockReason)
	}
	info.SetFirstResponseTime()
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}
