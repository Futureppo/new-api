package gcp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	rootconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	accountCredentials Credentials
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		if baseURL == "" || baseURL == defaultSpeechBaseURL || baseURL == defaultTextToSpeechBaseURL {
			baseURL = defaultTextToSpeechBaseURL
		}
		return baseURL + "/v1/text:synthesize", nil
	case relayconstant.RelayModeAudioTranscription:
		if baseURL == "" || baseURL == defaultSpeechBaseURL || baseURL == defaultTextToSpeechBaseURL {
			baseURL = defaultSpeechBaseURL
		}
		return baseURL + "/v1/speech:recognize", nil
	case relayconstant.RelayModeAudioTranslation:
		return "", errors.New("GCP speech channel does not support /v1/audio/translations")
	default:
		return "", fmt.Errorf("unsupported relay mode for GCP: %d", info.RelayMode)
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Content-Type", gin.MIMEJSON)

	creds := Credentials{}
	if err := common.Unmarshal([]byte(info.ApiKey), &creds); err != nil {
		return fmt.Errorf("failed to decode Google service account credentials: %w", err)
	}
	a.accountCredentials = creds

	accessToken, err := getAccessToken(info, creds)
	if err != nil {
		return err
	}
	req.Set("Authorization", "Bearer "+accessToken)
	if creds.ProjectID != "" {
		req.Set("x-goog-user-project", creds.ProjectID)
	}
	return nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	gcpMeta, err := parseGCPMetadata(request.Metadata)
	if err != nil {
		return nil, err
	}
	nativeResponse := boolFromMap(gcpMeta, "native_response")
	c.Set(contextKeyGCPNativeResponse, nativeResponse)

	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		return convertSpeechRequest(c, info, request, gcpMeta)
	case relayconstant.RelayModeAudioTranscription:
		return convertTranscriptionRequest(c, request, gcpMeta)
	case relayconstant.RelayModeAudioTranslation:
		return nil, errors.New("GCP speech channel does not support /v1/audio/translations")
	default:
		return nil, fmt.Errorf("unsupported audio relay mode for GCP: %d", info.RelayMode)
	}
}

func convertSpeechRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest, gcpMeta map[string]any) (io.Reader, error) {
	responseFormat := normalizeTTSResponseFormat(request.ResponseFormat)
	audioEncoding := mapTTSAudioEncoding(responseFormat)
	c.Set(contextKeyGCPResponseFormat, responseFormat)
	c.Set(contextKeyGCPAudioEncoding, audioEncoding)

	body := shallowCopyMap(gcpMeta)
	delete(body, "native_response")

	input := mapFromAny(body["input"])
	if len(input) == 0 {
		input = map[string]any{"text": request.Input}
	}
	body["input"] = input

	voice := mapFromAny(body["voice"])
	if len(voice) == 0 {
		voiceName := mapOpenAIVoiceToGCP(request.Voice)
		voice = map[string]any{
			"languageCode": languageCodeFromVoiceName(voiceName, defaultTTSLanguageCode),
			"name":         voiceName,
		}
	} else {
		if _, ok := voice["languageCode"]; !ok {
			if name, ok := voice["name"].(string); ok && name != "" {
				voice["languageCode"] = languageCodeFromVoiceName(name, defaultTTSLanguageCode)
			} else {
				voice["languageCode"] = defaultTTSLanguageCode
			}
		}
	}
	body["voice"] = voice

	audioConfig := mapFromAny(firstNonNil(body["audioConfig"], body["audio_config"]))
	if len(audioConfig) == 0 {
		audioConfig = map[string]any{}
	}
	if _, ok := audioConfig["audioEncoding"]; !ok {
		audioConfig["audioEncoding"] = audioEncoding
	}
	if request.Speed != nil {
		audioConfig["speakingRate"] = *request.Speed
	}
	body["audioConfig"] = audioConfig
	delete(body, "audio_config")

	if _, ok := body["advancedVoiceOptions"]; !ok {
		if advanced := body["advanced_voice_options"]; advanced != nil {
			body["advancedVoiceOptions"] = advanced
			delete(body, "advanced_voice_options")
		}
	}

	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("error marshalling GCP TTS request: %w", err)
	}
	relaycommon.AppendRequestConversionFromRequest(info, body)
	return bytes.NewReader(bodyBytes), nil
}

func convertTranscriptionRequest(c *gin.Context, request dto.AudioRequest, gcpMeta map[string]any) (io.Reader, error) {
	responseFormat := normalizeSTTResponseFormat(request.ResponseFormat)
	c.Set(contextKeyGCPResponseFormat, responseFormat)

	body := shallowCopyMap(gcpMeta)
	delete(body, "native_response")

	config := mapFromAny(body["config"])
	if len(config) == 0 {
		config = map[string]any{}
	}
	if _, ok := config["languageCode"]; !ok {
		config["languageCode"] = resolveSTTLanguageCode(request)
	}

	audio := mapFromAny(body["audio"])
	if hasRecognitionAudio(audio) {
		body["audio"] = audio
		body["config"] = config
		bodyBytes, err := common.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("error marshalling GCP STT request: %w", err)
		}
		return bytes.NewReader(bodyBytes), nil
	}

	content, filename, err := readMultipartAudioFile(c)
	if err != nil {
		return nil, err
	}
	audio["content"] = base64.StdEncoding.EncodeToString(content)
	body["audio"] = audio

	if _, ok := config["encoding"]; !ok {
		if encoding := inferRecognitionEncoding(filename); encoding != "" {
			config["encoding"] = encoding
		}
	}
	body["config"] = config

	bodyBytes, err := common.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("error marshalling GCP STT request: %w", err)
	}
	return bytes.NewReader(bodyBytes), nil
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		return handleTTSResponse(c, resp, info)
	case relayconstant.RelayModeAudioTranscription:
		return handleSTTResponse(c, resp, info)
	case relayconstant.RelayModeAudioTranslation:
		return nil, types.NewErrorWithStatusCode(
			errors.New("GCP speech channel does not support /v1/audio/translations"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	default:
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported relay mode for GCP: %d", info.RelayMode),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
}

func handleTTSResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	usage := newAudioOutputUsage(info)
	if c.GetBool(contextKeyGCPNativeResponse) {
		c.Data(http.StatusOK, gin.MIMEJSON, body)
		return usage, nil
	}

	if apiErr := gcpErrorFromBody(body); apiErr != nil {
		return nil, apiErr
	}

	var gcpResp struct {
		AudioContent string `json:"audioContent"`
	}
	if err = common.Unmarshal(body, &gcpResp); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if gcpResp.AudioContent == "" {
		return nil, types.NewErrorWithStatusCode(errors.New("missing audioContent in GCP TTS response"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	audioBytes, err := base64.StdEncoding.DecodeString(gcpResp.AudioContent)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to decode GCP audioContent: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	responseFormat := c.GetString(contextKeyGCPResponseFormat)
	if responseFormat == "" {
		responseFormat = defaultTTSFormat
	}
	c.Data(http.StatusOK, contentTypeForAudioFormat(responseFormat), audioBytes)
	fillAudioCompletionUsage(c, usage, audioBytes, responseFormat)
	return usage, nil
}

func handleSTTResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	usage := newAudioInputUsage(info)
	if c.GetBool(contextKeyGCPNativeResponse) {
		c.Data(http.StatusOK, gin.MIMEJSON, body)
		return usage, nil
	}

	if apiErr := gcpErrorFromBody(body); apiErr != nil {
		return nil, apiErr
	}

	var gcpResp struct {
		Results []struct {
			Alternatives []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
			} `json:"alternatives"`
			LanguageCode string `json:"languageCode"`
		} `json:"results"`
	}
	if err = common.Unmarshal(body, &gcpResp); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	transcriptParts := make([]string, 0, len(gcpResp.Results))
	language := defaultSTTLanguageCode
	for _, result := range gcpResp.Results {
		if result.LanguageCode != "" {
			language = result.LanguageCode
		}
		if len(result.Alternatives) == 0 {
			continue
		}
		text := strings.TrimSpace(result.Alternatives[0].Transcript)
		if text != "" {
			transcriptParts = append(transcriptParts, text)
		}
	}
	text := strings.Join(transcriptParts, "\n")

	switch c.GetString(contextKeyGCPResponseFormat) {
	case "text", "srt", "vtt":
		c.String(http.StatusOK, text)
	case "verbose_json":
		c.JSON(http.StatusOK, dto.WhisperVerboseJSONResponse{
			Task:     "transcribe",
			Language: language,
			Text:     text,
		})
	default:
		c.JSON(http.StatusOK, dto.AudioResponse{Text: text})
	}
	return usage, nil
}

func gcpErrorFromBody(body []byte) *types.NewAPIError {
	var errorResp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &errorResp); err != nil || errorResp.Error == nil {
		return nil
	}
	message := errorResp.Error.Message
	if message == "" {
		message = errorResp.Error.Status
	}
	if message == "" {
		message = "GCP upstream error"
	}
	statusCode := http.StatusBadGateway
	if errorResp.Error.Code >= 400 && errorResp.Error.Code <= 599 {
		statusCode = errorResp.Error.Code
	}
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponse, statusCode)
}

func newAudioOutputUsage(info *relaycommon.RelayInfo) *dto.Usage {
	usage := &dto.Usage{
		PromptTokens: info.GetEstimatePromptTokens(),
		TotalTokens:  info.GetEstimatePromptTokens(),
	}
	usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	return usage
}

func newAudioInputUsage(info *relaycommon.RelayInfo) *dto.Usage {
	usage := &dto.Usage{
		PromptTokens: info.GetEstimatePromptTokens(),
		TotalTokens:  info.GetEstimatePromptTokens(),
	}
	usage.PromptTokensDetails.AudioTokens = usage.PromptTokens
	return usage
}

func fillAudioCompletionUsage(c *gin.Context, usage *dto.Usage, audioBytes []byte, responseFormat string) {
	common.SetContextKey(c, rootconstant.ContextKeyLocalCountTokens, true)

	var duration float64
	var durationErr error
	if responseFormat == "pcm" {
		const sampleRate = 24000
		const bytesPerSample = 2
		const channels = 1
		duration = float64(len(audioBytes)) / float64(sampleRate*bytesPerSample*channels)
	} else {
		reader := bytes.NewReader(audioBytes)
		duration, durationErr = common.GetAudioDuration(c.Request.Context(), reader, "."+responseFormat)
	}

	completionTokens := 0
	if durationErr != nil {
		logger.LogWarn(c, fmt.Sprintf("failed to get GCP audio duration: %v", durationErr))
		completionTokens = int(math.Ceil(float64(len(audioBytes)) / 1000.0))
	} else if duration > 0 {
		completionTokens = int(math.Round(math.Ceil(duration) / 60.0 * 1000))
	}
	if completionTokens > 0 {
		usage.CompletionTokens = completionTokens
		usage.CompletionTokenDetails.AudioTokens = completionTokens
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
}

func parseGCPMetadata(metadata []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(metadata)) == 0 {
		return map[string]any{}, nil
	}
	var envelope map[string]any
	if err := common.Unmarshal(metadata, &envelope); err != nil {
		return nil, fmt.Errorf("error unmarshalling metadata: %w", err)
	}
	if gcp, ok := envelope["gcp"]; ok {
		return shallowCopyMap(mapFromAny(gcp)), nil
	}
	return map[string]any{}, nil
}

func readMultipartAudioFile(c *gin.Context) ([]byte, string, error) {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, "", fmt.Errorf("error parsing multipart form: %w", err)
	}
	fileHeaders := form.File["file"]
	if len(fileHeaders) == 0 {
		return nil, "", errors.New("file is required")
	}
	fileHeader := fileHeaders[0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("error opening audio file: %w", err)
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return nil, "", fmt.Errorf("error reading audio file: %w", err)
	}
	return content, fileHeader.Filename, nil
}

func hasRecognitionAudio(audio map[string]any) bool {
	if audio == nil {
		return false
	}
	if content, ok := audio["content"].(string); ok && strings.TrimSpace(content) != "" {
		return true
	}
	if uri, ok := audio["uri"].(string); ok && strings.TrimSpace(uri) != "" {
		return true
	}
	return false
}

func inferRecognitionEncoding(filename string) string {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".flac":
		return "FLAC"
	case ".mp3":
		return "MP3"
	case ".wav", ".wave":
		return "LINEAR16"
	case ".ogg", ".oga", ".opus":
		return "OGG_OPUS"
	case ".webm":
		return "WEBM_OPUS"
	case ".mulaw", ".ulaw":
		return "MULAW"
	case ".amr":
		return "AMR"
	case ".awb":
		return "AMR_WB"
	default:
		return ""
	}
}

func resolveSTTLanguageCode(request dto.AudioRequest) string {
	if len(request.Language) == 0 {
		return defaultSTTLanguageCode
	}
	var language string
	if err := common.Unmarshal(request.Language, &language); err != nil || strings.TrimSpace(language) == "" {
		return defaultSTTLanguageCode
	}
	return normalizeLanguageCode(language)
}

func normalizeLanguageCode(language string) string {
	language = strings.TrimSpace(language)
	switch strings.ToLower(language) {
	case "", "zh", "zh-cn", "zh_hans", "zh-hans", "cmn":
		return defaultSTTLanguageCode
	case "en":
		return "en-US"
	case "ja":
		return "ja-JP"
	case "ko":
		return "ko-KR"
	default:
		return language
	}
}

func normalizeTTSResponseFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "mp3":
		return defaultTTSFormat
	case "wav":
		return "wav"
	case "pcm":
		return "pcm"
	case "ogg", "opus", "ogg_opus":
		return "ogg"
	case "mulaw", "ulaw":
		return "mulaw"
	case "alaw":
		return "alaw"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

func normalizeSTTResponseFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return "json"
	case "text":
		return "text"
	case "verbose_json":
		return "verbose_json"
	case "srt":
		return "srt"
	case "vtt":
		return "vtt"
	default:
		return "json"
	}
}

func mapTTSAudioEncoding(format string) string {
	switch normalizeTTSResponseFormat(format) {
	case "wav", "pcm":
		return "LINEAR16"
	case "ogg":
		return "OGG_OPUS"
	case "mulaw":
		return "MULAW"
	case "alaw":
		return "ALAW"
	default:
		return "MP3"
	}
}

func contentTypeForAudioFormat(format string) string {
	switch normalizeTTSResponseFormat(format) {
	case "wav":
		return "audio/wav"
	case "pcm":
		return "audio/pcm"
	case "ogg":
		return "audio/ogg"
	case "mulaw":
		return "audio/basic"
	case "alaw":
		return "audio/basic"
	default:
		return "audio/mpeg"
	}
}

func mapOpenAIVoiceToGCP(voice string) string {
	switch strings.ToLower(strings.TrimSpace(voice)) {
	case "", "alloy", "echo", "fable", "onyx", "nova", "shimmer":
		return defaultTTSVoiceName
	default:
		return strings.TrimSpace(voice)
	}
}

func languageCodeFromVoiceName(voiceName, fallback string) string {
	parts := strings.Split(voiceName, "-")
	if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
		return parts[0] + "-" + parts[1]
	}
	return fallback
}

func shallowCopyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if m, ok := value.(map[string]any); ok {
		return shallowCopyMap(m)
	}
	return map[string]any{}
}

func boolFromMap(m map[string]any, key string) bool {
	if value, ok := m[key]; ok {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return false
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errors.New("GCP channel only supports audio speech and transcription")
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, errors.New("GCP channel does not support rerank requests")
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("GCP channel does not support embedding requests")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("GCP channel does not support image requests")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("GCP channel does not support responses requests")
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, errors.New("GCP channel does not support Claude requests")
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("GCP channel does not support Gemini requests")
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
