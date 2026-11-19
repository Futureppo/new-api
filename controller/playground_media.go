package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

func playgroundImageUploadLimit(channelType int, model string) int {
	model = strings.ToLower(model)
	switch channelType {
	case constant.ChannelTypeXai:
		return 5
	case constant.ChannelTypeVolcEngine:
		return seedreamReferenceLimit(model)
	case constant.ChannelTypeVertexAi:
		if strings.Contains(model, "gemini-3-pro-image") {
			return 14
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeOpenAILocal:
		if strings.HasPrefix(model, "dall-e") {
			return 1
		}
		return 16
	case constant.ChannelTypeQianwenAI, constant.ChannelTypeAli:
		switch {
		case strings.HasPrefix(model, "qwen-image-3.0"), strings.HasPrefix(model, "qwen-image-2.0"), strings.HasPrefix(model, "qwen-image-edit"):
			return 3
		case strings.HasPrefix(model, "qwen-image-2.1"):
			return 10
		case strings.HasPrefix(model, "wan2.7-image"):
			return 9
		case strings.HasPrefix(model, "wan2.6-image"):
			return 4
		}
	}
	return 0
}

func seedreamReferenceLimit(model string) int {
	model = strings.ReplaceAll(model, ".", "-")
	switch {
	case strings.Contains(model, "seedream-5-5"), strings.Contains(model, "seedream-5-0-pro"), strings.Contains(model, "seedream-5-0-flash"):
		return 10
	case strings.Contains(model, "seedream-5-0-lite"), strings.Contains(model, "seedream-4-5"), strings.Contains(model, "seedream-4-0"):
		return 14
	}
	return 0
}

func playgroundVideoImageLimits(channelType int, model string) (frames, references int) {
	model = strings.ToLower(model)
	switch channelType {
	case constant.ChannelTypeXai, constant.ChannelTypeOpenRouter:
		if base := model[strings.LastIndexByte(model, '/')+1:]; strings.HasPrefix(base, "grok-imagine-video") {
			switch {
			case strings.HasPrefix(base, "grok-imagine-video-1.5-lite"):
				return 0, 0
			case strings.HasPrefix(base, "grok-imagine-video-1.5"):
				return 2, 14
			default:
				return 0, 7
			}
		}
		if channelType == constant.ChannelTypeOpenRouter {
			if strings.HasPrefix(model, "google/veo-3.1-lite") {
				return 2, 0
			}
			if strings.HasPrefix(model, "google/veo-3.1") {
				return 2, 3
			}
			if strings.HasPrefix(model, "bytedance/seedance-") {
				return playgroundVideoImageLimits(constant.ChannelTypeVolcEngine, model)
			}
		}
	case constant.ChannelTypeQianwenAI:
		switch {
		case strings.HasPrefix(model, "wan3."):
			return 2, 10
		case strings.HasPrefix(model, "wan2.7-i2v"):
			return 2, 0
		case strings.HasPrefix(model, "wan2.7-r2v"):
			return 0, 5
		}
	case constant.ChannelTypeVolcEngine:
		model = strings.ReplaceAll(model, ".", "-")
		switch {
		case strings.Contains(model, "seedance-2-5-flash"):
			return 2, 10
		case strings.Contains(model, "seedance-2-5"):
			return 2, 30
		case strings.Contains(model, "seedance-2-0"):
			return 2, 9
		case strings.Contains(model, "seedance-1-0-pro") && !strings.Contains(model, "fast"):
			return 2, 0
		}
	case constant.ChannelTypeVertexAi:
		if strings.Contains(model, "veo-3.1-generate-001") || strings.Contains(model, "veo-3.1-fast-generate-001") {
			return 2, 3
		}
	}
	return 0, 0
}

func mergePlaygroundLimit(a int, hasA bool, b int, hasB bool) int {
	if !hasA {
		return b
	}
	if !hasB {
		return a
	}
	if a == 0 || b == 0 {
		return 0
	}
	return max(a, b)
}

func playgroundLimitExceeded(count, limit int) bool {
	return limit > 0 && count > limit
}

func playgroundPublicImageURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Hostname() != "" && parsed.User == nil && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func parsePlaygroundInputImages(values []string) ([]*dto.GeminiInlineData, error) {
	images := make([]*dto.GeminiInlineData, 0, len(values))
	for i, value := range values {
		image, err := parsePlaygroundInputImage(value)
		if err != nil {
			return nil, fmt.Errorf("第 %d 张图片：%w", i+1, err)
		}
		images = append(images, image)
	}
	return images, nil
}

func playgroundImageFiles(form *multipart.Form) []*multipart.FileHeader {
	if files := form.File["image[]"]; len(files) > 0 {
		return files
	}
	return form.File["image"]
}

func validatePlaygroundImageFile(header *multipart.FileHeader) error {
	file, err := header.Open()
	if err != nil {
		return err
	}
	defer file.Close()
	var prefix [512]byte
	n, err := io.ReadFull(file, prefix[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return err
	}
	if !strings.HasPrefix(http.DetectContentType(prefix[:n]), "image/") {
		return errors.New("参考文件必须是图片")
	}
	return nil
}

func appendPlaygroundVertexImages(fields map[string]json.RawMessage, action string, images []*dto.GeminiInlineData) error {
	var contents []map[string]json.RawMessage
	if action == "embedContent" {
		var content map[string]json.RawMessage
		if common.Unmarshal(fields["content"], &content) != nil || content == nil {
			return errors.New("添加图片时，content 必须是 JSON 对象")
		}
		contents = []map[string]json.RawMessage{content}
	} else if common.Unmarshal(fields["contents"], &contents) != nil || len(contents) == 0 {
		return errors.New("添加图片时，contents 必须是非空数组")
	}
	last := contents[len(contents)-1]
	if last == nil {
		return errors.New("图片需要添加到有效的用户内容")
	}
	if action == "generateContent" {
		var role string
		_ = common.Unmarshal(last["role"], &role)
		if role != "" && role != "user" {
			last = map[string]json.RawMessage{"role": json.RawMessage(`"user"`)}
			contents = append(contents, last)
		}
	}
	var parts []json.RawMessage
	if raw := last["parts"]; raw != nil && common.Unmarshal(raw, &parts) != nil {
		return errors.New("添加图片时，parts 必须是数组")
	}
	for _, image := range images {
		part, _ := common.Marshal(dto.GeminiPart{InlineData: image})
		parts = append(parts, part)
	}
	last["parts"], _ = common.Marshal(parts)
	if action == "embedContent" {
		fields["content"], _ = common.Marshal(last)
	} else {
		fields["contents"], _ = common.Marshal(contents)
	}
	return nil
}
