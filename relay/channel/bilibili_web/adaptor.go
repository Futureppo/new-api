package bilibili_web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/bilibili"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const DefaultModel = "index-mt-9b-syllable"

// This is a maintained catalog, not a live availability check. The public
// /v1/models endpoint does not advertise the website's models.
var ModelList = []string{
	"index-mt-2b-syllable", DefaultModel, "index-nailong-9b",
	"index-echo-2b-s2st", "index-echo-9b-s2st",
}

type Adaptor struct {
	bilibili.Adaptor
	clientStream   bool
	upstreamSSE    bool
	responses      bool
	responseModel  string
	responseMeta   any
	responseID     string
	echoCompleted  bool
	pollInterval   time.Duration
	requestTimeout time.Duration
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	a.Adaptor.Init(info)
	a.clientStream = info.IsStream
	a.responses = info.RelayMode == relayconstant.RelayModeResponses
	a.responseModel = info.OriginModelName
	a.responseID = "resp_" + common.GetUUID()
	if a.pollInterval == 0 {
		a.pollInterval = 2 * time.Second
	}
	if a.requestTimeout == 0 {
		a.requestTimeout = 150 * time.Second
	}
}

func (a *Adaptor) GetModelList() []string { return append([]string(nil), ModelList...) }
func (a *Adaptor) GetChannelName() string { return "bilibili_web" }

func isTextModel(name string) bool {
	return name == "index-mt-2b-syllable" || name == DefaultModel || name == "index-nailong-9b"
}

func webURL(base, path string) (string, error) {
	if err := bilibili.ValidateBaseURL(base); err != nil {
		return "", invalid(err.Error())
	}
	u, _ := url.Parse(bilibili.NormalizeBaseURL(base))
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = ""
	u.RawQuery = url.Values{"p": []string{path}}.Encode()
	return u.String(), nil
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info.RelayMode != relayconstant.RelayModeChatCompletions && info.RelayMode != relayconstant.RelayModeResponses {
		return "", invalid("Bilibili Web supports Chat Completions and text Responses creation only")
	}
	return webURL(info.ChannelBaseUrl, "/v1/chat/completions")
}

func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, req *dto.GeneralOpenAIRequest) (any, error) {
	if req == nil {
		return nil, invalid("request is required")
	}
	// Conversion is performed on the final body in DoRequest, after administrator
	// parameter overrides. Keep the DTO here so system prompt handling still works.
	return req, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, req dto.OpenAIResponsesRequest) (any, error) {
	return &req, nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (any, error) {
	if _, err := a.GetRequestURL(info); err != nil {
		return nil, err
	}
	raw, err := readLimited(body, 16<<20)
	if err != nil {
		return nil, invalid(err.Error())
	}
	var req *dto.GeneralOpenAIRequest
	if a.responses {
		req, a.responseMeta, err = responsesToChat(raw)
	} else {
		req = &dto.GeneralOpenAIRequest{}
		err = common.Unmarshal(raw, req)
	}
	if err != nil {
		return nil, invalid(err.Error())
	}
	if a.responseModel == "" {
		a.responseModel = req.Model
	}
	if req.Stream != nil && *req.Stream != a.clientStream {
		return nil, invalid("parameter overrides cannot change the client's stream mode")
	}
	if constant.IsBilibiliWebEchoModel(req.Model) {
		if a.responses {
			return nil, invalid("Echo audio is supported through Chat Completions only")
		}
		return a.echoRequest(c, info, req)
	}
	if !isTextModel(req.Model) {
		return nil, invalid("unsupported Bilibili Web model: " + req.Model)
	}
	if err = validateTextRequest(req); err != nil {
		return nil, err
	}
	if _, err = a.Adaptor.ConvertOpenAIRequest(c, info, req); err != nil {
		return nil, err
	}
	if req.MaxCompletionTokens != nil {
		req.MaxTokens = req.MaxCompletionTokens
		req.MaxCompletionTokens = nil
	}
	// Request real usage from the website even when the caller wants a single JSON
	// result. DoResponse aggregates it without changing the downstream protocol.
	req.Stream = common.GetPointer(true)
	req.StreamOptions = &dto.StreamOptions{IncludeUsage: common.GetPointer(true)}
	data, err := common.Marshal(req)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	resp, err := a.send(ctx, c, info, http.MethodPost, "/v1/chat/completions", "application/json", bytes.NewReader(data))
	if err != nil {
		cancel()
		return nil, upstreamError(err.Error())
	}
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	a.upstreamSSE = strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	// Relay handlers infer streaming from Content-Type. They must see the client's
	// protocol, not the website's internal transport.
	if !a.clientStream {
		resp.Header.Set("Content-Type", "application/json")
	}
	return resp, nil
}

func (a *Adaptor) send(ctx context.Context, c *gin.Context, info *relaycommon.RelayInfo, method, path, contentType string, body io.Reader) (*http.Response, error) {
	target, err := webURL(info.ChannelBaseUrl, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if err = a.SetupRequestHeader(c, &req.Header, info); err != nil {
		return nil, err
	}
	headers, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
		if strings.EqualFold(name, "Host") {
			req.Host = value
		}
	}
	// Framing is owned by this converter, particularly multipart boundaries.
	req.Header.Set("Content-Type", contentType)
	client, err := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, errors.New("HTTP client is not initialized")
	}
	return client.Do(req)
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelBody) Close() error {
	b.cancel()
	return b.ReadCloser.Close()
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("payload exceeds %d bytes", limit)
	}
	return data, err
}

func invalid(message string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func upstreamError(message string) *types.NewAPIError {
	// Echo submissions may already have succeeded when the response is lost.
	// Never retry the whole request after a website task has been submitted.
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponse, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

var _ channel.Adaptor = (*Adaptor)(nil)
