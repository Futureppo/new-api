package typesafe

import (
	"encoding/json"
	"errors"
	"fmt"
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

// UpstreamError is carried through the existing retry/refund pipeline. It must
// not write a response until the controller has finished trying channels.
type UpstreamError struct {
	Status     int
	Body       []byte
	RetryAfter string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("TypeSafe upstream returned HTTP %d", e.Status)
}

func WriteUpstreamError(c *gin.Context, apiErr *types.NewAPIError) bool {
	var upstream *UpstreamError
	if !errors.As(apiErr, &upstream) {
		return false
	}
	if upstream.RetryAfter != "" {
		c.Header("Retry-After", upstream.RetryAfter)
	}
	// c.Writer applies the existing error privacy and full model mapping policy.
	c.Data(apiErr.StatusCode, gin.MIMEJSON, upstream.Body)
	return true
}

func badResponse(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, badResponse("empty TypeSafe response")
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, badResponse("failed to read TypeSafe response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var raw json.RawMessage
		if err := common.Unmarshal(body, &raw); err != nil || len(raw) == 0 {
			return nil, badResponse("invalid TypeSafe error response")
		}
		upstream := &UpstreamError{Status: resp.StatusCode, Body: body, RetryAfter: resp.Header.Get("Retry-After")}
		apiErr := types.NewErrorWithStatusCode(upstream, types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return nil, apiErr
	}
	var result struct {
		Model   string                     `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   *struct {
			InputTokens  *int `json:"input_tokens"`
			OutputTokens *int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := common.Unmarshal(body, &result); err != nil || strings.TrimSpace(result.Model) == "" || result.Answers == nil || result.Usage == nil || result.Usage.InputTokens == nil || result.Usage.OutputTokens == nil {
		return nil, badResponse("invalid TypeSafe response: model, answers and token usage are required")
	}
	input, output := *result.Usage.InputTokens, *result.Usage.OutputTokens
	if input < 0 || output < 0 || input > math.MaxInt-output {
		return nil, badResponse("invalid TypeSafe token usage")
	}
	usage := &dto.Usage{PromptTokens: input, CompletionTokens: output, TotalTokens: input + output}
	info.SetFirstResponseTime()
	c.Data(resp.StatusCode, gin.MIMEJSON, body)
	return usage, nil
}
