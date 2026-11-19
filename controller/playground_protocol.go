package controller

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

var playgroundFormats = map[constant.EndpointType]types.RelayFormat{
	constant.EndpointTypeOpenAI:                types.RelayFormatOpenAI,
	constant.EndpointTypeCohereChat:            types.RelayFormatOpenAI,
	constant.EndpointTypeOpenAIResponse:        types.RelayFormatOpenAIResponses,
	constant.EndpointTypeOpenAIResponseCompact: types.RelayFormatOpenAIResponsesCompaction,
	constant.EndpointTypeAnthropic:             types.RelayFormatClaude,
	constant.EndpointTypeGemini:                types.RelayFormatGemini,
	constant.EndpointTypeVertexNative:          types.RelayFormatGemini,
	constant.EndpointTypeTypeSafeSystemOne:     types.RelayFormatTypeSafe,
	constant.EndpointTypeOpenAIDecisions:       types.RelayFormatOpenAIDecisions,
	constant.EndpointTypeImageGeneration:       types.RelayFormatOpenAIImage,
	constant.EndpointTypeOpenAIVideo:           types.RelayFormatTask,
	constant.EndpointTypeBatchGeneration:       types.RelayFormatTask,
	constant.EndpointTypeEmbeddings:            types.RelayFormatEmbedding,
	constant.EndpointTypeCohereEmbeddings:      types.RelayFormatEmbedding,
	constant.EndpointTypeJinaRerank:            types.RelayFormatRerank,
	constant.EndpointTypeCohereRerank:          types.RelayFormatRerank,
	constant.EndpointTypeModerations:           types.RelayFormatOpenAI,
	constant.EndpointTypeAudioSpeech:           types.RelayFormatOpenAIAudio,
	constant.EndpointTypeAudioTranscription:    types.RelayFormatOpenAIAudio,
}

type playgroundProtocolRequest struct {
	Model           string          `json:"model"`
	Group           string          `json:"group"`
	Stream          bool            `json:"stream"`
	Native          bool            `json:"native"`
	Body            json.RawMessage `json:"body"`
	ImageEdit       *bool           `json:"image_edit,omitempty"`
	ReferenceURLs   []string        `json:"reference_urls,omitempty"`
	VideoImages     []string        `json:"video_images,omitempty"`
	VideoImageMode  string          `json:"video_image_mode,omitempty"`
	EmbeddingImage  string          `json:"embedding_image,omitempty"`
	EmbeddingImages []string        `json:"embedding_images,omitempty"`
	EmbeddingMode   string          `json:"embedding_mode,omitempty"`
	InputImage      string          `json:"input_image,omitempty"`
	InputImages     []string        `json:"input_images,omitempty"`
	QueryImage      string          `json:"query_image,omitempty"`
	ImageTarget     string          `json:"image_target,omitempty"`
}

type playgroundEndpointOption struct {
	Stream               bool     `json:"stream"`
	Action               string   `json:"action,omitempty"`
	Family               string   `json:"family,omitempty"`
	Variant              string   `json:"variant,omitempty"`
	Upload               bool     `json:"upload,omitempty"`
	UploadLimit          int      `json:"upload_limit,omitempty"`
	ImageEdit            bool     `json:"image_edit,omitempty"`
	ImageURLLimit        int      `json:"image_url_limit,omitempty"`
	EmbeddingImage       bool     `json:"embedding_image,omitempty"`
	EmbeddingSeparate    bool     `json:"embedding_separate,omitempty"`
	EmbeddingJoint       bool     `json:"embedding_joint,omitempty"`
	EmbeddingLimit       int      `json:"embedding_limit,omitempty"`
	EmbeddingJointLimit  int      `json:"embedding_joint_limit,omitempty"`
	InputImage           bool     `json:"input_image,omitempty"`
	InputImageLimit      int      `json:"input_image_limit,omitempty"`
	ImageQuery           bool     `json:"image_query,omitempty"`
	VideoImageURL        bool     `json:"video_image_url,omitempty"`
	VideoImageData       bool     `json:"video_image_data,omitempty"`
	VideoFramesLimit     int      `json:"video_frames_limit,omitempty"`
	VideoReferencesLimit int      `json:"video_references_limit,omitempty"`
	AudioFormats         []string `json:"audio_formats,omitempty"`
	DefaultVoice         string   `json:"default_voice,omitempty"`
}

func playgroundError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"message": message, "type": "invalid_request_error"}})
}

func playgroundScope(c *gin.Context, group string) (string, []string, bool) {
	if c.GetBool("use_access_token") {
		playgroundError(c, http.StatusForbidden, "操练场需要使用登录会话")
		return "", nil, false
	}
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		playgroundError(c, http.StatusInternalServerError, "读取用户分组失败")
		return "", nil, false
	}
	if group == "" {
		group = user.Group
	}
	if group != user.Group && !service.GroupInUserUsableGroups(user.Group, group) {
		playgroundError(c, http.StatusForbidden, "无权访问所选分组")
		return "", nil, false
	}
	user.WriteContext(c)
	return group, service.GetRequestGroupCandidates(c, user.Group, group), true
}

func playgroundMappedModel(ch *model.Channel, name string) string {
	var mapping map[string]string
	if raw := ch.GetModelMapping(); raw != "" {
		_ = common.UnmarshalJsonStr(raw, &mapping)
	}
	seen := map[string]bool{}
	for {
		next := mapping[name]
		if next == name || next == "" || seen[next] {
			return name
		}
		seen[name] = true
		name = next
	}
}

func playgroundChannelOptions(ch *model.Channel, name string) map[constant.EndpointType]playgroundEndpointOption {
	if strings.HasSuffix(name, ratio_setting.CompactModelSuffix) {
		if ch.Type == constant.ChannelTypeOpenAI || ch.Type == constant.ChannelTypeCodex {
			return map[constant.EndpointType]playgroundEndpointOption{constant.EndpointTypeOpenAIResponseCompact: {}}
		}
		return nil
	}
	name = playgroundMappedModel(ch, name)
	embeddingImage := relaycommon.PlaygroundEmbeddingImageFormat(ch.Type, name) != ""
	endpoints := slices.Clone(common.GetEndpointTypesByChannelType(ch.Type, name))
	switch ch.Type {
	case constant.ChannelTypeAnthropic, constant.ChannelTypeGemini, constant.ChannelTypeOpenCode, constant.ChannelTypeOpenCodeGo:
		// These adaptors translate Chat to the model's actual wire protocol.
		endpoints = endpoints[:min(len(endpoints), 1)]
		if ch.Type == constant.ChannelTypeGemini {
			if strings.Contains(name, "embedding") {
				endpoints = []constant.EndpointType{constant.EndpointTypeEmbeddings}
			} else if common.IsImageGenerationModel(name) || strings.HasPrefix(name, "imagen") {
				endpoints = []constant.EndpointType{constant.EndpointTypeImageGeneration}
			}
		}
	case constant.ChannelTypeAws:
		// Bedrock uses signed operations, not the Anthropic Messages path.
		return nil
	case constant.ChannelTypeDeepSeek:
		endpoints = append(endpoints, constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAIResponse)
	case constant.ChannelTypeOpenAI:
		switch {
		case strings.HasPrefix(name, "text-embedding-") || embeddingImage:
			endpoints = []constant.EndpointType{constant.EndpointTypeEmbeddings}
		case strings.Contains(name, "moderation"):
			endpoints = []constant.EndpointType{constant.EndpointTypeModerations}
		case name == "whisper-1" || strings.Contains(name, "transcribe"):
			endpoints = []constant.EndpointType{constant.EndpointTypeAudioTranscription}
		case strings.HasPrefix(name, "tts-") || strings.Contains(name, "-tts"):
			endpoints = []constant.EndpointType{constant.EndpointTypeAudioSpeech}
		case common.IsImageGenerationModel(name) || playgroundSupportsImageUpload(ch, name):
			endpoints = []constant.EndpointType{constant.EndpointTypeImageGeneration}
		case common.IsVideoGenerationModel(name):
			endpoints = []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
		default:
			if !slices.Contains(endpoints, constant.EndpointTypeOpenAIResponse) {
				endpoints = append(endpoints, constant.EndpointTypeOpenAIResponse)
			}
		}
	case constant.ChannelTypeTypeSafe:
		endpoints = []constant.EndpointType{constant.EndpointTypeTypeSafeSystemOne, constant.EndpointTypeOpenAIDecisions}
	case constant.ChannelTypeQianwenAI:
		if constant.IsQianwenDecisionModel(name) {
			endpoints = []constant.EndpointType{constant.EndpointTypeTypeSafeSystemOne, constant.EndpointTypeOpenAIDecisions}
		}
	case constant.ChannelTypeVertexAi:
		capability, err := vertexmodel.Resolve(name)
		if err != nil || (ch.GetOtherSettings().VertexKeyType == dto.VertexKeyTypeAPIKey && capability.Action != "generateContent") {
			return nil
		}
		if capability.Action != "chat/completions" && !slices.Contains(endpoints, constant.EndpointTypeVertexNative) {
			endpoints = append(endpoints, constant.EndpointTypeVertexNative)
		}
	case constant.ChannelTypeBilibiliWeb:
		endpoints = []constant.EndpointType{constant.EndpointTypeOpenAI}
	case constant.ChannelTypeXai:
		if strings.HasPrefix(name, "grok-imagine-image") {
			endpoints = []constant.EndpointType{constant.EndpointTypeImageGeneration}
		}
	case constant.ChannelTypeJina:
		if !strings.Contains(name, "rerank") {
			endpoints = []constant.EndpointType{constant.EndpointTypeEmbeddings}
		}
	case constant.ChannelTypeCodex:
		endpoints = []constant.EndpointType{constant.EndpointTypeOpenAIResponse}
	case constant.ChannelTypeOpenRouter:
		if embeddingImage {
			endpoints = []constant.EndpointType{constant.EndpointTypeEmbeddings}
		}
		settings := ch.GetOtherSettings()
		endpoints = slices.DeleteFunc(endpoints, func(e constant.EndpointType) bool {
			return e == constant.EndpointTypeTypeSafeSystemOne ||
				(settings.IsOpenRouterEnterprise() && (e == constant.EndpointTypeAnthropic || e == constant.EndpointTypeOpenAIDecisions || e == constant.EndpointTypeOpenAIVideo))
		})
	case constant.ChannelTypeOpenAILocal:
		if embeddingImage {
			endpoints = []constant.EndpointType{constant.EndpointTypeEmbeddings}
		}
	}
	out := make(map[constant.EndpointType]playgroundEndpointOption, len(endpoints))
	for _, endpoint := range endpoints {
		if _, ok := playgroundFormats[endpoint]; !ok {
			continue
		}
		option := playgroundEndpointOption{Stream: common.ChannelTestEndpointSupportsStream(endpoint)}
		option.AudioFormats = playgroundAudioFormats(ch.Type, name, endpoint)
		if endpoint == constant.EndpointTypeAudioSpeech && ch.Type == constant.ChannelTypeOpenAI {
			option.DefaultVoice = "alloy"
		}
		option.Upload = endpoint == constant.EndpointTypeAudioTranscription ||
			(endpoint == constant.EndpointTypeOpenAIVideo && slices.Contains([]int{
				constant.ChannelTypeOpenAI, constant.ChannelTypeSora, constant.ChannelTypeGemini, constant.ChannelTypeVertexAi,
			}, ch.Type))
		if endpoint == constant.EndpointTypeOpenAIVideo && !strings.Contains(name, "-t2v") {
			option.VideoImageURL = slices.Contains([]int{
				constant.ChannelTypeQianwenAI, constant.ChannelTypeVolcEngine, constant.ChannelTypeXai, constant.ChannelTypeVertexAi,
			}, ch.Type)
			option.VideoFramesLimit, option.VideoReferencesLimit = playgroundVideoImageLimits(ch.Type, name)
			option.VideoImageURL = option.VideoImageURL || option.VideoFramesLimit > 0 || option.VideoReferencesLimit > 0
			option.VideoImageData = ch.Type != constant.ChannelTypeOpenRouter && (option.VideoFramesLimit > 0 || option.VideoReferencesLimit > 0)
		}
		if endpoint == constant.EndpointTypeImageGeneration {
			option.Upload = playgroundSupportsImageUpload(ch, name)
			option.ImageEdit = option.Upload || ch.Type == constant.ChannelTypeGMICloud
			if option.Upload {
				option.UploadLimit = playgroundImageUploadLimit(ch.Type, name)
			}
			if ch.Type == constant.ChannelTypeGMICloud {
				option.ImageURLLimit = 5
			}
		}
		if endpoint == constant.EndpointTypeOpenAIVideo && option.Upload {
			option.UploadLimit = 1
		}
		option.EmbeddingImage = embeddingImage && (endpoint == constant.EndpointTypeEmbeddings || endpoint == constant.EndpointTypeCohereEmbeddings)
		if option.EmbeddingImage {
			option.EmbeddingSeparate, option.EmbeddingJoint, option.EmbeddingLimit = relaycommon.PlaygroundEmbeddingModes(ch.Type, name)
			if option.EmbeddingJoint {
				switch relaycommon.PlaygroundEmbeddingImageFormat(ch.Type, name) {
				case relaycommon.EmbeddingImageGemini:
					option.EmbeddingJointLimit = 6
				case relaycommon.EmbeddingImageDashScope:
					option.EmbeddingJointLimit = option.EmbeddingLimit
				}
			}
		}
		if endpoint == constant.EndpointTypeModerations {
			option.InputImage = ch.Type == constant.ChannelTypeOpenAI &&
				strings.HasPrefix(name[strings.LastIndexByte(name, '/')+1:], "omni-moderation-")
			if strings.Contains(name, "-csam-") {
				option.InputImageLimit = 1
			}
		} else if endpoint == constant.EndpointTypeJinaRerank {
			qwenImage := ch.Type == constant.ChannelTypeQianwenAI && name == "qwen3-vl-rerank"
			jinaImage := strings.HasPrefix(name[strings.LastIndexByte(name, '/')+1:], "jina-reranker-m0")
			option.ImageQuery = qwenImage || (ch.Type == constant.ChannelTypeJina && jinaImage)
			option.InputImage = option.ImageQuery || (ch.Type == constant.ChannelTypeOpenRouter && jinaImage)
			if qwenImage {
				option.InputImageLimit = 40
			}
		}
		if endpoint == constant.EndpointTypeOpenAIDecisions && (ch.Type == constant.ChannelTypeOpenRouter || constant.SupportsNamedDecisions(ch.Type)) {
			option.Variant = "named"
		}
		if endpoint == constant.EndpointTypeOpenAI && ch.Type == constant.ChannelTypeBilibiliWeb && constant.IsBilibiliWebEchoModel(name) {
			option.Stream = false
		}
		if endpoint == constant.EndpointTypeVertexNative {
			capability, err := vertexmodel.Resolve(name)
			if err != nil {
				continue
			}
			option.Action = capability.Action
			option.Family = string(capability.Family)
			option.InputImage = capability.Action == "embedContent" || capability.Family == vertexmodel.Gemini || capability.Family == vertexmodel.GeminiImage
			if capability.Action == "embedContent" {
				option.InputImageLimit = 6
			}
		}
		out[endpoint] = option
	}
	return out
}

func playgroundAudioFormats(channelType int, name string, endpoint constant.EndpointType) []string {
	switch endpoint {
	case constant.EndpointTypeAudioSpeech:
		switch channelType {
		case constant.ChannelTypeOpenAI:
			return []string{"mp3", "wav", "opus", "aac", "flac", "pcm"}
		case constant.ChannelTypeVertexAi:
			return []string{"wav", "pcm"}
		case constant.ChannelTypeGCP:
			return []string{"mp3", "wav", "ogg", "pcm"}
		}
	case constant.EndpointTypeAudioTranscription:
		switch channelType {
		case constant.ChannelTypeOpenAI:
			if name == "whisper-1" {
				return []string{"json", "text", "srt", "vtt", "verbose_json"}
			}
		case constant.ChannelTypeVertexAi:
			return []string{"json", "text"}
		case constant.ChannelTypeGCP:
			return []string{"json", "text", "srt", "vtt", "verbose_json"}
		case constant.ChannelTypeOpenRouter:
			return []string{"json", "verbose_json"}
		}
	}
	return nil
}

func playgroundSupportsImageUpload(ch *model.Channel, name string) bool {
	name = strings.ToLower(name)
	switch ch.Type {
	case constant.ChannelTypeXai:
		return strings.HasPrefix(name, "grok-imagine-image")
	case constant.ChannelTypeVolcEngine:
		return seedreamReferenceLimit(name) > 0
	case constant.ChannelTypeOpenAI, constant.ChannelTypeOpenAILocal:
		return strings.HasPrefix(name, "gpt-image-") || name == "chatgpt-image-latest" || name == "dall-e-2" || name == "dall-e"
	case constant.ChannelTypeOpenRouter:
		settings := ch.GetOtherSettings()
		return !settings.IsOpenRouterEnterprise()
	case constant.ChannelTypeQianwenAI:
		return strings.HasPrefix(name, "qwen-image") || strings.HasPrefix(name, "wan2.")
	case constant.ChannelTypeAli:
		return strings.HasPrefix(name, "qwen-image-edit") || strings.HasPrefix(name, "wan2.7-image") || strings.Contains(name, "edit")
	case constant.ChannelTypeVertexAi:
		capability, err := vertexmodel.Resolve(name)
		return err == nil && (capability.Family == vertexmodel.GeminiImage ||
			(capability.Family == vertexmodel.Imagen && strings.Contains(name, "capability")))
	default:
		return false
	}
}

func PlaygroundEndpoints(c *gin.Context) {
	_, groups, ok := playgroundScope(c, c.Query("group"))
	if !ok {
		return
	}
	name := strings.TrimSpace(c.Query("model"))
	options := map[constant.EndpointType]playgroundEndpointOption{}
	if name != "" {
		channels, err := model.GetPlaygroundChannels(groups, name)
		if err != nil {
			playgroundError(c, http.StatusInternalServerError, "读取可用上游路径失败")
			return
		}
		for i := range channels {
			for endpoint, option := range playgroundChannelOptions(&channels[i], name) {
				if previous, exists := options[endpoint]; exists {
					option.UploadLimit = mergePlaygroundLimit(option.UploadLimit, option.Upload, previous.UploadLimit, previous.Upload)
					option.ImageURLLimit = max(option.ImageURLLimit, previous.ImageURLLimit)
					option.EmbeddingLimit = mergePlaygroundLimit(option.EmbeddingLimit, option.EmbeddingSeparate, previous.EmbeddingLimit, previous.EmbeddingSeparate)
					option.EmbeddingJointLimit = mergePlaygroundLimit(option.EmbeddingJointLimit, option.EmbeddingJoint, previous.EmbeddingJointLimit, previous.EmbeddingJoint)
					option.InputImageLimit = mergePlaygroundLimit(option.InputImageLimit, option.InputImage, previous.InputImageLimit, previous.InputImage)
					option.Stream = option.Stream || previous.Stream
					option.Upload = option.Upload || previous.Upload
					option.ImageEdit = option.ImageEdit || previous.ImageEdit
					option.EmbeddingImage = option.EmbeddingImage || previous.EmbeddingImage
					option.EmbeddingSeparate = option.EmbeddingSeparate || previous.EmbeddingSeparate
					option.EmbeddingJoint = option.EmbeddingJoint || previous.EmbeddingJoint
					option.InputImage = option.InputImage || previous.InputImage
					option.ImageQuery = option.ImageQuery || previous.ImageQuery
					option.VideoImageURL = option.VideoImageURL || previous.VideoImageURL
					option.VideoImageData = option.VideoImageData || previous.VideoImageData
					option.VideoFramesLimit = max(option.VideoFramesLimit, previous.VideoFramesLimit)
					option.VideoReferencesLimit = max(option.VideoReferencesLimit, previous.VideoReferencesLimit)
					option.AudioFormats = slices.DeleteFunc(option.AudioFormats, func(format string) bool {
						return !slices.Contains(previous.AudioFormats, format)
					})
					if previous.DefaultVoice != option.DefaultVoice {
						option.DefaultVoice = ""
					}
					if previous.Action != option.Action {
						option.Action = ""
					}
					if previous.Family != option.Family {
						option.Family = ""
					}
					if previous.Variant != option.Variant {
						option.Variant = ""
					}
				}
				options[endpoint] = option
			}
		}
	}
	common.ApiSuccess(c, options)
}

func PreparePlaygroundProtocol(c *gin.Context) {
	endpoint := constant.EndpointType(c.Param("endpoint"))
	if _, ok := playgroundFormats[endpoint]; !ok {
		playgroundError(c, http.StatusBadRequest, "不支持所选上游路径")
		return
	}
	var req playgroundProtocolRequest
	isMultipart := c.ContentType() == gin.MIMEMultipartPOSTForm
	var err error
	if isMultipart {
		req.Group, req.Model = c.Query("group"), c.Query("model")
		if endpoint != constant.EndpointTypeAudioTranscription && endpoint != constant.EndpointTypeOpenAIVideo &&
			endpoint != constant.EndpointTypeImageGeneration {
			playgroundError(c, http.StatusBadRequest, "所选端点不接受文件上传")
			return
		}
		err = validatePlaygroundUpload(c, req.Model, endpoint)
	} else {
		var storage common.BodyStorage
		storage, err = common.GetBodyStorage(c)
		if err == nil {
			err = common.DecodeJson(storage, &req)
		}
	}
	if err != nil {
		status := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) {
			status = http.StatusRequestEntityTooLarge
		}
		playgroundError(c, status, err.Error())
		return
	}
	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		playgroundError(c, http.StatusBadRequest, "请选择模型")
		return
	}
	if req.EmbeddingImage != "" {
		req.EmbeddingImages = append([]string{req.EmbeddingImage}, req.EmbeddingImages...)
	}
	if req.InputImage != "" {
		if req.ImageTarget == "query" && req.QueryImage == "" {
			req.QueryImage = req.InputImage
		} else if req.ImageTarget == "" {
			req.InputImages = append([]string{req.InputImage}, req.InputImages...)
		} else {
			playgroundError(c, http.StatusBadRequest, "重排序只能添加一张查询图片")
			return
		}
	}
	imageEdit := endpoint == constant.EndpointTypeImageGeneration && (isMultipart || (req.ImageEdit != nil && *req.ImageEdit))
	uploadCount := 0
	if isMultipart && imageEdit {
		uploadCount = len(playgroundImageFiles(c.Request.MultipartForm))
	}
	imageURLCount := len(req.ReferenceURLs)
	if imageURLCount > 0 {
		if !imageEdit || isMultipart || req.Native {
			playgroundError(c, http.StatusBadRequest, "使用参考图片时请选择图生图")
			return
		}
		for _, value := range req.ReferenceURLs {
			if !playgroundPublicImageURL(value) {
				playgroundError(c, http.StatusBadRequest, "请输入公开的参考图链接")
				return
			}
		}
	}
	videoImageURL := false
	videoImageData := false
	if len(req.VideoImages) > 0 {
		if endpoint != constant.EndpointTypeOpenAIVideo || isMultipart || req.Native ||
			(req.VideoImageMode != "frames" && req.VideoImageMode != "reference") {
			playgroundError(c, http.StatusBadRequest, "请选择视频图片的用途")
			return
		}
		for _, value := range req.VideoImages {
			if playgroundPublicImageURL(value) {
				continue
			}
			if _, err := parsePlaygroundInputImage(value); err != nil {
				playgroundError(c, http.StatusBadRequest, err.Error())
				return
			}
			videoImageData = true
		}
		common.SetContextKey(c, constant.ContextKeyPlaygroundVideoImageMode, req.VideoImageMode)
	}
	if endpoint == constant.EndpointTypeOpenAIVideo && !req.Native && !isMultipart {
		if value := gjson.GetBytes(req.Body, "image_url").String(); value != "" {
			parsed, parseErr := url.Parse(value)
			if parseErr != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				playgroundError(c, http.StatusBadRequest, "请输入公开的参考图链接")
				return
			}
			videoImageURL = true
		}
	}
	vertexImageAction := ""
	var vertexImages []*dto.GeminiInlineData
	if len(req.InputImages) > 0 || req.QueryImage != "" {
		if req.Native && endpoint == constant.EndpointTypeVertexNative && req.QueryImage == "" {
			if gjson.GetBytes(req.Body, "content").Exists() {
				vertexImageAction = "embedContent"
			} else {
				vertexImageAction = "generateContent"
			}
		} else if req.Native || (endpoint != constant.EndpointTypeModerations && endpoint != constant.EndpointTypeJinaRerank) ||
			(req.QueryImage != "" && endpoint != constant.EndpointTypeJinaRerank) {
			playgroundError(c, http.StatusBadRequest, "所选请求模式不接受图片附件")
			return
		}
		images := slices.Clone(req.InputImages)
		if req.QueryImage != "" {
			images = append(images, req.QueryImage)
		}
		parsed, err := parsePlaygroundInputImages(images)
		if err != nil {
			playgroundError(c, http.StatusBadRequest, err.Error())
			return
		}
		if vertexImageAction != "" {
			vertexImages = parsed
		}
	}
	if len(req.EmbeddingImages) > 0 {
		if req.Native || (endpoint != constant.EndpointTypeEmbeddings && endpoint != constant.EndpointTypeCohereEmbeddings) {
			playgroundError(c, http.StatusBadRequest, "所选请求模式不接受嵌入图片附件")
			return
		}
		if req.EmbeddingMode != "" && req.EmbeddingMode != "separate" && req.EmbeddingMode != "joint" {
			playgroundError(c, http.StatusBadRequest, "嵌入图片模式无效")
			return
		}
		images, err := parsePlaygroundInputImages(req.EmbeddingImages)
		if err != nil {
			playgroundError(c, http.StatusBadRequest, err.Error())
			return
		}
		common.SetContextKey(c, constant.ContextKeyPlaygroundEmbeddingImage, relaycommon.PlaygroundEmbeddingAttachment{Images: images, Mode: req.EmbeddingMode})
	}
	if endpoint == constant.EndpointTypeImageGeneration && !isMultipart && req.ImageEdit != nil {
		var fields map[string]json.RawMessage
		if err := common.Unmarshal(req.Body, &fields); err != nil || fields == nil {
			playgroundError(c, http.StatusBadRequest, "请求体必须是有效的 JSON 对象")
			return
		}
		hasReference := imageURLCount > 0 || hasPlaygroundImageReference(fields)
		if imageEdit && !hasReference {
			playgroundError(c, http.StatusBadRequest, "图生图需要参考图片")
			return
		}
		if !imageEdit && hasReference {
			playgroundError(c, http.StatusBadRequest, "使用参考图片时请选择图生图")
			return
		}
	}
	group, groups, ok := playgroundScope(c, req.Group)
	if !ok {
		return
	}
	channels, err := model.GetPlaygroundChannels(groups, req.Model)
	if err != nil {
		playgroundError(c, http.StatusInternalServerError, "读取可用上游路径失败")
		return
	}
	excluded := make(map[int]bool)
	allowed := make(map[int]bool)
	action := ""
	var decisionFields map[string]json.RawMessage
	if endpoint == constant.EndpointTypeOpenAIDecisions {
		_ = common.Unmarshal(req.Body, &decisionFields)
	}
	for i := range channels {
		ch := &channels[i]
		option, available := playgroundChannelOptions(ch, req.Model)[endpoint]
		if !available || (req.Stream && !option.Stream) || (isMultipart && !option.Upload) ||
			(imageEdit && !option.ImageEdit) ||
			playgroundLimitExceeded(uploadCount, option.UploadLimit) ||
			(imageURLCount > option.ImageURLLimit) ||
			(videoImageURL && !option.VideoImageURL) ||
			(videoImageData && !option.VideoImageData) ||
			(req.VideoImageMode == "frames" && len(req.VideoImages) > option.VideoFramesLimit) ||
			(req.VideoImageMode == "reference" && len(req.VideoImages) > option.VideoReferencesLimit) ||
			(len(req.InputImages) > 0 && (!option.InputImage || playgroundLimitExceeded(len(req.InputImages), option.InputImageLimit))) ||
			(req.QueryImage != "" && !option.ImageQuery) ||
			(vertexImageAction != "" && option.Action != vertexImageAction) ||
			(len(req.EmbeddingImages) > 0 && (!option.EmbeddingImage ||
				((req.EmbeddingMode == "separate" || !option.EmbeddingJoint) && playgroundLimitExceeded(len(req.EmbeddingImages), option.EmbeddingLimit)) ||
				((req.EmbeddingMode == "joint" || !option.EmbeddingSeparate) && playgroundLimitExceeded(len(req.EmbeddingImages), option.EmbeddingJointLimit)) ||
				(req.EmbeddingMode == "joint" && !option.EmbeddingJoint) || (req.EmbeddingMode == "separate" && !option.EmbeddingSeparate))) ||
			(endpoint == constant.EndpointTypeOpenAIDecisions && (option.Variant == "named") != (decisionFields["state"] != nil)) {
			excluded[ch.Id] = true
			continue
		}
		allowed[ch.Id] = true
	}
	if len(allowed) == 0 {
		if len(req.InputImages) > 0 || req.QueryImage != "" || videoImageURL || len(req.VideoImages) > 0 {
			playgroundError(c, http.StatusBadRequest, "当前模型没有支持这种图片输入的可用渠道")
			return
		}
		if len(req.EmbeddingImages) > 0 {
			playgroundError(c, http.StatusBadRequest, "当前模型没有支持图片嵌入的可用渠道")
			return
		}
		if imageEdit {
			playgroundError(c, http.StatusBadRequest, "当前模型没有可用的图生图渠道或不支持这种参考图输入")
			return
		}
		playgroundError(c, http.StatusBadRequest, "当前模型不支持所选上游路径或流式模式")
		return
	}
	if endpoint == constant.EndpointTypeGemini {
		action = "generateContent"
		if req.Stream {
			action = "streamGenerateContent"
		}
	}
	info, _ := common.GetDefaultEndpointInfo(endpoint)
	path := strings.ReplaceAll(info.Path, "{model}", req.Model)
	if imageEdit {
		path = "/v1/images/edits"
	}
	if endpoint == constant.EndpointTypeGemini {
		path = strings.TrimSuffix(path, "generateContent") + action
	}
	if endpoint == constant.EndpointTypeCohereEmbeddings {
		path = "/v1/embeddings"
	} else if endpoint == constant.EndpointTypeCohereRerank {
		path = "/v1/rerank"
	}
	common.SetContextKey(c, constant.ContextKeyPlaygroundEndpoint, string(endpoint))
	common.SetContextKey(c, constant.ContextKeyPlaygroundModel, req.Model)
	common.SetContextKey(c, constant.ContextKeyPlaygroundGroup, group)
	common.SetContextKey(c, constant.ContextKeyPlaygroundExcludedChannels, excluded)
	common.SetContextKey(c, constant.ContextKeyPlaygroundAllowedChannels, allowed)
	if !isMultipart {
		body, err := playgroundProtocolBody(c, endpoint, req, vertexImageAction, vertexImages)
		if err != nil {
			playgroundError(c, http.StatusBadRequest, err.Error())
			return
		}
		replacePlaygroundBody(c, body)
	}
	c.Request.URL = &url.URL{Path: path}
	c.Request.RequestURI = c.Request.URL.RequestURI()
	c.Next()
}

func parsePlaygroundInputImage(dataURL string) (*dto.GeminiInlineData, error) {
	header, data, _ := strings.Cut(dataURL, ",")
	mimeType := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if header != "data:"+mimeType+";base64" || !strings.HasPrefix(mimeType, "image/") || data == "" {
		return nil, errors.New("附件必须是 Base64 图片")
	}
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))
	var prefix [512]byte
	n, err := io.ReadFull(reader, prefix[:])
	if (err != nil && err != io.ErrUnexpectedEOF) || http.DetectContentType(prefix[:n]) != mimeType {
		return nil, errors.New("附件的图片格式无效")
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return nil, errors.New("附件的 Base64 数据无效")
	}
	return &dto.GeminiInlineData{MimeType: mimeType, Data: data}, nil
}

func validatePlaygroundUpload(c *gin.Context, name string, endpoint constant.EndpointType) error {
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return err
	}
	// Task adaptors using Gin's multipart API must reuse the in-memory form.
	c.Request.MultipartForm = form
	c.Request.PostForm = form.Value
	c.Request.Form = c.Request.URL.Query()
	for key, values := range form.Value {
		c.Request.Form[key] = append(c.Request.Form[key], values...)
	}
	if values := form.Value["model"]; len(values) != 1 || values[0] != name {
		return errors.New("上传表单的模型必须与所选模型一致")
	}
	if endpoint == constant.EndpointTypeAudioTranscription && len(form.File["file"]) != 1 {
		return errors.New("请选择要转写的音频文件")
	}
	if endpoint == constant.EndpointTypeImageGeneration || endpoint == constant.EndpointTypeOpenAIVideo {
		files := playgroundImageFiles(form)
		if endpoint == constant.EndpointTypeOpenAIVideo {
			files = form.File["input_reference"]
			if len(files) != 1 {
				return errors.New("请选择一张参考图片")
			}
		} else if len(files) == 0 {
			return errors.New("图生图需要参考图片")
		} else if len(form.File["image"]) > 0 && len(form.File["image[]"]) > 0 {
			return errors.New("参考图片必须使用同一个上传字段")
		}
		for _, file := range files {
			if err := validatePlaygroundImageFile(file); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasPlaygroundImageReference(fields map[string]json.RawMessage) bool {
	for _, key := range []string{"image", "images", "input_references"} {
		switch common.GetJsonType(fields[key]) {
		case "string":
			var value string
			if common.Unmarshal(fields[key], &value) == nil && strings.TrimSpace(value) != "" {
				return true
			}
		case "array":
			var values []json.RawMessage
			if common.Unmarshal(fields[key], &values) == nil && len(values) > 0 {
				return true
			}
		case "object":
			var value map[string]json.RawMessage
			if common.Unmarshal(fields[key], &value) == nil && len(value) > 0 {
				return true
			}
		}
	}
	return false
}

func replacePlaygroundBody(c *gin.Context, body []byte) {
	common.CleanupBodyStorage(c)
	c.Set(common.KeyRequestBody, nil)
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Length", strconv.Itoa(len(body)))
	c.Request.Header.Set("Content-Type", gin.MIMEJSON)
}

func playgroundProtocolBody(c *gin.Context, endpoint constant.EndpointType, req playgroundProtocolRequest, vertexImageAction string, vertexImages []*dto.GeminiInlineData) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(req.Body, &fields); err != nil || fields == nil {
		return nil, errors.New("请求体必须是有效的 JSON 对象")
	}
	if modelField := fields["model"]; modelField != nil {
		var name string
		if err := common.Unmarshal(modelField, &name); err != nil || name != req.Model {
			return nil, errors.New("请求体中的模型必须与所选模型一致")
		}
	}
	delete(fields, "group")
	fields["model"], _ = common.Marshal(req.Model)
	if len(req.VideoImages) > 0 {
		for _, key := range []string{"image", "image_url", "images", "input_reference", "input", "metadata"} {
			if fields[key] != nil {
				return nil, errors.New("请选择一种视频图片输入方式")
			}
		}
		fields["images"], _ = common.Marshal(req.VideoImages)
	}
	if len(req.ReferenceURLs) > 0 {
		fields["image"], _ = common.Marshal(req.ReferenceURLs)
	}
	if len(req.InputImages) > 0 || req.QueryImage != "" {
		switch endpoint {
		case constant.EndpointTypeVertexNative:
			if err := appendPlaygroundVertexImages(fields, vertexImageAction, vertexImages); err != nil {
				return nil, err
			}
		case constant.EndpointTypeModerations:
			var input string
			if raw := fields["input"]; raw != nil && common.Unmarshal(raw, &input) != nil {
				return nil, errors.New("附带图片时，审核文本必须是字符串")
			}
			parts := make([]dto.MediaContent, 0, len(req.InputImages)+1)
			for _, image := range req.InputImages {
				parts = append(parts, dto.MediaContent{Type: "image_url", ImageUrl: &dto.MessageImageUrl{Url: image}})
			}
			if strings.TrimSpace(input) != "" {
				parts = append(parts, dto.MediaContent{Type: "text", Text: input})
			}
			fields["input"], _ = common.Marshal(parts)
		case constant.EndpointTypeJinaRerank:
			if req.QueryImage != "" {
				fields["query"], _ = common.Marshal(map[string]string{"image": req.QueryImage})
			}
			if len(req.InputImages) > 0 {
				var docs []json.RawMessage
				if raw := fields["documents"]; raw != nil && common.Unmarshal(raw, &docs) != nil {
					return nil, errors.New("候选文档必须是数组")
				}
				for _, value := range req.InputImages {
					image, _ := common.Marshal(map[string]string{"image": value})
					docs = append(docs, image)
				}
				fields["documents"], _ = common.Marshal(docs)
			}
			fields["return_documents"] = json.RawMessage(`false`)
		}
	}
	if len(req.EmbeddingImages) > 0 {
		input := fields["input"]
		if len(input) == 0 || common.GetJsonType(input) == "null" {
			fields["input"] = json.RawMessage(`[]`)
		} else if common.GetJsonType(input) != "string" {
			var texts []string
			if err := common.Unmarshal(input, &texts); err != nil {
				return nil, errors.New("附带图片时，嵌入文本必须是字符串或字符串数组")
			}
		}
	}
	if endpoint == constant.EndpointTypeCohereEmbeddings && !req.Native && fields["input_type"] == nil {
		fields["input_type"] = json.RawMessage(`"search_document"`)
	}
	switch endpoint {
	case constant.EndpointTypeOpenAI, constant.EndpointTypeCohereChat, constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic:
		fields["stream"], _ = common.Marshal(req.Stream)
	default:
		delete(fields, "stream")
	}
	body, err := common.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if req.Native {
		switch endpoint {
		case constant.EndpointTypeCohereChat, constant.EndpointTypeCohereRerank, constant.EndpointTypeCohereEmbeddings:
			common.SetContextKey(c, constant.ContextKeyPlaygroundNativeBody, body)
			if endpoint == constant.EndpointTypeCohereEmbeddings {
				fields["input"] = fields["texts"]
				if fields["input"] == nil {
					fields["input"] = fields["inputs"]
				}
				if fields["input"] == nil {
					fields["input"] = fields["images"]
				}
			}
		case constant.EndpointTypeGemini, constant.EndpointTypeVertexNative:
			delete(fields, "model")
		}
		return common.Marshal(fields)
	}
	switch endpoint {
	case constant.EndpointTypeOpenAIResponse, constant.EndpointTypeAnthropic, constant.EndpointTypeGemini:
		var chat dto.GeneralOpenAIRequest
		if err := common.Unmarshal(body, &chat); err != nil {
			return nil, err
		}
		chat.Stream = common.GetPointer(req.Stream)
		switch endpoint {
		case constant.EndpointTypeOpenAIResponse:
			request, err := service.ChatCompletionsRequestToResponsesRequest(&chat)
			if err != nil {
				return nil, err
			}
			return common.Marshal(request)
		case constant.EndpointTypeAnthropic:
			request, err := claude.RequestOpenAI2ClaudeMessage(c, chat)
			if err != nil {
				return nil, err
			}
			return common.Marshal(request)
		default:
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: req.Model}}
			request, err := gemini.CovertOpenAI2Gemini(c, chat, info)
			if err != nil {
				return nil, err
			}
			request.GenerationConfig.TopP = chat.TopP
			if chat.Seed != nil {
				request.GenerationConfig.Seed = common.GetPointer(int64(*chat.Seed))
			}
			return common.Marshal(request)
		}
	default:
		return body, nil
	}
}

func PlaygroundProtocol(c *gin.Context) {
	endpoint := constant.EndpointType(common.GetContextKeyString(c, constant.ContextKeyPlaygroundEndpoint))
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	_ = middleware.SetupContextForToken(c, &model.Token{UserId: c.GetInt("id"), Name: "playground-" + group, Group: group})
	if endpoint == constant.EndpointTypeVertexNative {
		name := common.GetContextKeyString(c, constant.ContextKeyPlaygroundModel)
		ch, err := model.CacheGetChannel(common.GetContextKeyInt(c, constant.ContextKeyChannelId))
		if err != nil || ch == nil {
			playgroundError(c, http.StatusServiceUnavailable, "所选上游渠道已不可用")
			return
		}
		capability, err := vertexmodel.Resolve(playgroundMappedModel(ch, name))
		if err != nil {
			playgroundError(c, http.StatusBadRequest, "模型没有可用的 Vertex 原生操作")
			return
		}
		c.Request.URL.Path = fmt.Sprintf("/v1/models/%s:%s", name, capability.Action)
		c.Request.RequestURI = c.Request.URL.RequestURI()
	}
	if endpoint == constant.EndpointTypeImageGeneration {
		RelayImageGeneration(c)
		return
	}
	if format := playgroundFormats[endpoint]; format == types.RelayFormatTask {
		RelayTask(c)
	} else {
		Relay(c, format)
	}
}

func PlaygroundTask(c *gin.Context) {
	if c.GetBool("use_access_token") {
		playgroundError(c, http.StatusForbidden, "操练场需要使用登录会话")
		return
	}
	task, exists, err := model.GetByTaskId(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		playgroundError(c, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if !exists {
		playgroundError(c, http.StatusNotFound, "任务不存在")
		return
	}
	path := "/v1/video/generations/"
	if constant.IsImageTaskAction(task.Action) {
		path = "/v1/images/tasks/"
	} else if task.Action == constant.TaskActionBatchInference {
		path = "/v1/batch/generations/"
	}
	c.Request.URL = &url.URL{Path: path + task.TaskID}
	c.Request.RequestURI = c.Request.URL.RequestURI()
	RelayTaskFetch(c)
}
