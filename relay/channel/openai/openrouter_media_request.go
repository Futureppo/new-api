package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

func readOpenRouterUpload(file *multipart.FileHeader, limit int64) ([]byte, error) {
	if file.Size > limit {
		return nil, openRouterInvalid("uploaded file exceeds the size limit")
	}
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, openRouterInvalid("uploaded file exceeds the size limit")
	}
	return data, nil
}

func openRouterImageReference(value json.RawMessage) ([]json.RawMessage, error) {
	if !hasOpenRouterValue(value) {
		return nil, nil
	}
	if common.GetJsonType(value) == "array" {
		var values []json.RawMessage
		if err := common.Unmarshal(value, &values); err != nil {
			return nil, err
		}
		var refs []json.RawMessage
		for _, item := range values {
			parts, err := openRouterImageReference(item)
			if err != nil {
				return nil, err
			}
			refs = append(refs, parts...)
		}
		return refs, nil
	}
	var url, detail string
	if common.GetJsonType(value) == "string" {
		if err := common.Unmarshal(value, &url); err != nil {
			return nil, err
		}
	} else {
		var ref struct {
			URL      string `json:"url"`
			ImageURL struct {
				URL    string `json:"url"`
				Detail string `json:"detail,omitempty"`
			} `json:"image_url"`
		}
		if err := common.Unmarshal(value, &ref); err != nil {
			return nil, err
		}
		url = ref.URL
		if url == "" {
			url = ref.ImageURL.URL
			detail = ref.ImageURL.Detail
		}
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "data:image/") {
		return nil, openRouterInvalid("image references must be HTTP(S) URLs or image data URLs")
	}
	imageURL := map[string]string{"url": url}
	if detail != "" {
		imageURL["detail"] = detail
	}
	ref, err := common.Marshal(map[string]any{"type": "image_url", "image_url": imageURL})
	return []json.RawMessage{ref}, err
}

func convertOpenRouterImage(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	fields, err := openRouterObject(request)
	if err != nil {
		return nil, err
	}
	refs, err := openRouterImageReference(request.InputReferences)
	if err != nil {
		return nil, err
	}
	for _, value := range []json.RawMessage{request.Image, request.Images} {
		more, e := openRouterImageReference(value)
		if e != nil {
			return nil, e
		}
		refs = append(refs, more...)
	}
	if strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return nil, err
		}
		defer form.RemoveAll()
		if len(form.File["mask"]) > 0 || len(form.Value["mask"]) > 0 {
			return nil, openRouterInvalid("mask editing is not supported; use reference images")
		}
		for _, name := range []string{"image", "image[]", "images", "images[]"} {
			for _, file := range form.File[name] {
				data, err := readOpenRouterUpload(file, 25<<20)
				if err != nil {
					return nil, err
				}
				mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(file.Filename)))
				if mediaType == "" {
					mediaType = http.DetectContentType(data)
				}
				if !strings.HasPrefix(mediaType, "image/") {
					return nil, openRouterInvalid("image upload must contain an image")
				}
				url, _ := common.Marshal("data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data))
				parts, err := openRouterImageReference(url)
				if err != nil {
					return nil, err
				}
				refs = append(refs, parts...)
			}
		}
	}
	if info.RelayMode == relayconstant.RelayModeImagesEdits && len(refs) == 0 {
		return nil, openRouterInvalid("image editing requires at least one reference image")
	}
	if len(refs) > 0 {
		fields["input_references"], err = common.Marshal(refs)
		if err != nil {
			return nil, err
		}
	}
	delete(fields, "image")
	delete(fields, "images")
	return fields, nil
}

func convertOpenRouterAudio(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	fields, err := openRouterObject(request)
	if err != nil {
		return nil, err
	}
	if request.Voice.ID != "" {
		return nil, openRouterInvalid("voice must be a string")
	}
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		if strings.TrimSpace(request.Input) == "" {
			return nil, openRouterInvalid("input is required")
		}
		if request.Voice.Name == "" {
			delete(fields, "voice")
		}
		if request.StreamFormat != "" && request.StreamFormat != "audio" {
			return nil, openRouterInvalid("speech returns binary audio; stream_format=sse is not supported")
		}
		delete(fields, "stream")
		delete(fields, "stream_format")
	case relayconstant.RelayModeAudioTranscription:
		delete(fields, "input")
		delete(fields, "voice")
		if request.Stream != nil && *request.Stream {
			return nil, openRouterInvalid("transcriptions do not support SSE")
		}
		delete(fields, "stream")
		if request.SourceURL != nil {
			if hasOpenRouterValue(request.InputAudio) {
				return nil, openRouterInvalid("provide only one audio source")
			}
			fields["input_audio"], _ = common.Marshal(map[string]string{"url": *request.SourceURL})
		}
		delete(fields, "source_url")
		if strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
			form, err := common.ParseMultipartFormReusable(c)
			if err != nil {
				return nil, err
			}
			defer form.RemoveAll()
			files := form.File["file"]
			if len(files) > 1 || (len(files) > 0 && hasOpenRouterValue(fields["input_audio"])) {
				return nil, openRouterInvalid("provide exactly one file or audio URL")
			}
			if len(files) == 1 {
				data, err := readOpenRouterUpload(files[0], 25<<20)
				if err != nil {
					return nil, err
				}
				format := strings.TrimPrefix(strings.ToLower(filepath.Ext(files[0].Filename)), ".")
				if format == "" {
					mediaType, _, _ := mime.ParseMediaType(files[0].Header.Get("Content-Type"))
					format = strings.TrimPrefix(mediaType, "audio/")
					if format == "mpeg" {
						format = "mp3"
					}
				}
				fields["input_audio"], err = common.Marshal(map[string]string{"data": base64.StdEncoding.EncodeToString(data), "format": format})
				if err != nil {
					return nil, err
				}
			}
		}
		if !hasOpenRouterValue(fields["input_audio"]) {
			return nil, openRouterInvalid("input_audio, file or source_url is required")
		}
	default:
		return nil, openRouterInvalid("audio translations are not supported")
	}
	body, err := common.Marshal(fields)
	if err != nil {
		return nil, err
	}
	body, err = relaycommon.RemoveDisabledFields(body, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		body, err = relaycommon.ApplyParamOverrideWithRelayInfo(body, info)
		if err != nil {
			return nil, fmt.Errorf("apply audio parameter override: %w", err)
		}
	}
	return bytes.NewReader(body), nil
}
