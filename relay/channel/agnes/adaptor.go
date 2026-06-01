package agnes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	openai.Adaptor
}

type imageRequest struct {
	Model     string         `json:"model"`
	Prompt    string         `json:"prompt"`
	Size      string         `json:"size,omitempty"`
	ExtraBody map[string]any `json:"extra_body,omitempty"`
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	relayMode := relayconstant.RelayModeImagesGenerations
	if info != nil {
		relayMode = info.RelayMode
	}
	switch relayMode {
	case relayconstant.RelayModeImagesGenerations:
		return convertImageRequest(info, request, false)
	case relayconstant.RelayModeImagesEdits:
		return convertImageRequest(info, request, true)
	default:
		return a.Adaptor.ConvertImageRequest(c, info, request)
	}
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if info != nil && info.RelayMode == relayconstant.RelayModeImagesEdits {
		baseURL := info.ChannelBaseUrl
		if baseURL == "" {
			baseURL = channelconstant.ChannelBaseURLs[channelconstant.ChannelTypeAgnesAI]
		}
		return relaycommon.GetFullRequestURL(baseURL, "/v1/images/generations", info.ChannelType), nil
	}
	return a.Adaptor.GetRequestURL(info)
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.Adaptor.SetupRequestHeader(c, header, info); err != nil {
		return err
	}
	if info == nil {
		return nil
	}
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		header.Set("Content-Type", gin.MIMEJSON)
	}
	return nil
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

func convertImageRequest(info *relaycommon.RelayInfo, request dto.ImageRequest, requireImage bool) (any, error) {
	if request.N != nil && *request.N > 1 {
		return nil, errors.New("agnes image API does not support n > 1")
	}

	modelName := strings.TrimSpace(request.Model)
	if info != nil && info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		modelName = strings.TrimSpace(info.UpstreamModelName)
	}
	if modelName == "" {
		modelName = ModelImage21Flash
	}

	extraBody, err := buildExtraBody(request)
	if err != nil {
		return nil, err
	}
	if requireImage && !hasImageInput(extraBody) {
		return nil, errors.New("agnes image edits require an image URL in image or extra_body.image; file uploads are not supported")
	}

	converted := imageRequest{
		Model:     modelName,
		Prompt:    request.Prompt,
		Size:      request.Size,
		ExtraBody: extraBody,
	}

	return converted, nil
}

func hasImageInput(extraBody map[string]any) bool {
	if len(extraBody) == 0 {
		return false
	}
	images, ok := extraBody["image"].([]string)
	return ok && len(images) > 0
}

func buildExtraBody(request dto.ImageRequest) (map[string]any, error) {
	extraBody := make(map[string]any)

	if request.Extra != nil {
		if raw, ok := request.Extra["extra_body"]; ok && len(bytes.TrimSpace(raw)) > 0 {
			var parsed map[string]json.RawMessage
			if err := common.Unmarshal(raw, &parsed); err != nil {
				return nil, fmt.Errorf("invalid extra_body: %w", err)
			}
			for key, value := range parsed {
				if len(bytes.TrimSpace(value)) == 0 {
					continue
				}
				if key == "image" {
					images, ok, err := normalizeImageValue(value)
					if err != nil {
						return nil, err
					}
					if ok {
						extraBody[key] = images
					}
				} else {
					extraBody[key] = value
				}
			}
		}
	}

	if _, ok := extraBody["image"]; !ok && len(bytes.TrimSpace(request.Image)) > 0 {
		images, ok, err := normalizeImageValue(request.Image)
		if err != nil {
			return nil, err
		}
		if ok {
			extraBody["image"] = images
		}
	}

	if _, ok := extraBody["response_format"]; !ok && strings.TrimSpace(request.ResponseFormat) != "" {
		extraBody["response_format"] = strings.TrimSpace(request.ResponseFormat)
	}

	if len(extraBody) == 0 {
		return nil, nil
	}
	return extraBody, nil
}

func normalizeImageValue(raw json.RawMessage) ([]string, bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false, nil
	}

	var images []string
	if err := common.Unmarshal(trimmed, &images); err == nil {
		images = compactStrings(images)
		return images, len(images) > 0, nil
	}

	var image string
	if err := common.Unmarshal(trimmed, &image); err == nil {
		image = strings.TrimSpace(image)
		if image == "" {
			return nil, false, nil
		}
		return []string{image}, true, nil
	}

	return nil, false, errors.New("agnes image input must be a URL string or an array of URL strings")
}

func compactStrings(values []string) []string {
	compact := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			compact = append(compact, value)
		}
	}
	return compact
}
