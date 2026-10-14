package vertex

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func (a *Adaptor) convertImage(c *gin.Context, info *relaycommon.RelayInfo, r dto.ImageRequest) (any, error) {
	if a.initErr != nil {
		return nil, a.initErr
	}
	if a.capability.Family != vertexmodel.Imagen && a.capability.Family != vertexmodel.GeminiImage {
		return nil, errors.New("this Vertex model does not support the images API")
	}
	if r.IsStream(c) {
		return nil, errors.New("use native streamGenerateContent for streaming Gemini images")
	}
	if a.capability.Family == vertexmodel.Imagen && !info.PriceData.UsePrice {
		return nil, errors.New("Imagen requires per-image pricing")
	}
	n := uint(1)
	if r.N != nil {
		n = *r.N
	}
	if n == 0 {
		return nil, errors.New("n must be positive")
	}
	if r.ResponseFormat != "" && r.ResponseFormat != "b64_json" && r.ResponseFormat != "url" {
		return nil, errors.New("response_format must be b64_json or url")
	}
	a.imageFormat = r.ResponseFormat
	a.responseKind = "image"
	a.action = a.capability.Action
	images, err := mediaUploads(c, "image", "image[]", "images", "images[]")
	if err != nil {
		return nil, err
	}
	for _, source := range [][]byte{r.Image, r.Images} {
		loaded, err := mediaSources(c, source)
		if err != nil {
			return nil, err
		}
		images = append(images, loaded...)
	}
	masks, err := mediaUploads(c, "mask")
	if err != nil {
		return nil, err
	}
	loaded, err := mediaSources(c, r.Mask)
	if err != nil {
		return nil, err
	}
	masks = append(masks, loaded...)
	if len(masks) > 1 {
		return nil, errors.New("only one mask is supported")
	}
	for _, f := range append(append([]*types.LocalFileData{}, images...), masks...) {
		if !strings.HasPrefix(f.MimeType, "image/") {
			return nil, errors.New("image input must have an image MIME type")
		}
	}
	if info.RelayMode == relayconstant.RelayModeImagesEdits && len(images) == 0 {
		return nil, errors.New("image edits require an image")
	}
	extra := map[string]any{}
	if len(r.ExtraFields) > 0 {
		if err := common.Unmarshal(r.ExtraFields, &extra); err != nil {
			return nil, fmt.Errorf("extra_fields must be an object: %w", err)
		}
	}
	if a.capability.Family == vertexmodel.GeminiImage {
		if n != 1 {
			return nil, errors.New("Gemini images compatibility requests require n=1")
		}
		if len(masks) > 0 {
			return nil, errors.New("Gemini image models do not support Imagen masks; use an instruction and reference images")
		}
		parts := []any{map[string]any{"text": r.Prompt}}
		for _, f := range images {
			parts = append(parts, inlinePart(f))
		}
		config := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
		if v, ok := extra["generationConfig"].(map[string]any); ok {
			for k, v := range v {
				config[k] = v
			}
		}
		imageConfig := map[string]any{}
		if v, ok := config["imageConfig"].(map[string]any); ok {
			imageConfig = v
		}
		if r.AspectRatio != nil && *r.AspectRatio != "" {
			imageConfig["aspectRatio"] = *r.AspectRatio
		}
		if r.Resolution != nil && *r.Resolution != "" {
			imageConfig["imageSize"] = *r.Resolution
		}
		if len(imageConfig) > 0 {
			config["imageConfig"] = imageConfig
		}
		extra["contents"] = []any{map[string]any{"role": "user", "parts": parts}}
		extra["generationConfig"] = config
		return extra, nil
	}
	parameters := map[string]any{}
	if v, ok := extra["parameters"].(map[string]any); ok {
		parameters = v
	} else {
		for k, v := range extra {
			parameters[k] = v
		}
	}
	if existing, ok := parameters["sampleCount"]; ok && fmt.Sprint(existing) != fmt.Sprint(n) {
		return nil, errors.New("n conflicts with parameters.sampleCount")
	}
	parameters["sampleCount"] = n
	if r.Seed != nil {
		parameters["seed"] = *r.Seed
	}
	if r.Watermark != nil {
		parameters["addWatermark"] = *r.Watermark
	}
	if r.NegativePrompt != "" {
		parameters["negativePrompt"] = r.NegativePrompt
	}
	if r.AspectRatio != nil && *r.AspectRatio != "" {
		parameters["aspectRatio"] = *r.AspectRatio
	} else if r.Size != "" && r.Size != "auto" {
		ar := r.Size
		switch r.Size {
		case "1024x1024":
			ar = "1:1"
		case "1024x1792":
			ar = "9:16"
		case "1792x1024":
			ar = "16:9"
		}
		if !strings.Contains(ar, ":") {
			return nil, errors.New("Imagen size must be an aspect ratio or supported dimensions")
		}
		parameters["aspectRatio"] = ar
	}
	instance := map[string]any{"prompt": r.Prompt}
	if len(images) > 0 {
		if parameters["mode"] == "upscale" {
			if len(images) != 1 || len(masks) > 0 {
				return nil, errors.New("upscale requires one image and no mask")
			}
			instance["image"] = map[string]any{"bytesBase64Encoded": images[0].Base64Data}
		}
		if parameters["mode"] != "upscale" {
			refs := []any{}
			for i, f := range images {
				refs = append(refs, map[string]any{"referenceType": "REFERENCE_TYPE_RAW", "referenceId": i + 1, "referenceImage": map[string]any{"bytesBase64Encoded": f.Base64Data}})
			}
			if len(masks) > 0 {
				refs = append(refs, map[string]any{"referenceType": "REFERENCE_TYPE_MASK", "referenceId": len(images) + 1, "referenceImage": map[string]any{"bytesBase64Encoded": masks[0].Base64Data}, "maskImageConfig": map[string]any{"maskMode": "MASK_MODE_USER_PROVIDED"}})
				if _, ok := parameters["editMode"]; !ok {
					parameters["editMode"] = "EDIT_MODE_INPAINT_INSERTION"
				}
			}
			instance["referenceImages"] = refs
		}
	} else if len(masks) > 0 {
		return nil, errors.New("mask requires an input image")
	}
	return map[string]any{"instances": []any{instance}, "parameters": parameters}, nil
}

func (a *Adaptor) imageResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, badResponse(err)
	}
	usage, _, err := responseUsage(data)
	if err != nil {
		return nil, badResponse(err)
	}
	var envelope struct {
		Predictions []struct {
			Bytes    string `json:"bytesBase64Encoded"`
			GCSURI   string `json:"gcsUri"`
			MIME     string `json:"mimeType"`
			Prompt   string `json:"prompt"`
			Filtered string `json:"raiFilteredReason"`
		} `json:"predictions"`
		Candidates []struct {
			Content struct {
				Parts []struct {
					Inline *struct {
						Data string `json:"data"`
						MIME string `json:"mimeType"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil, badResponse(err)
	}
	items := []gin.H{}
	add := func(b64, mt, prompt string) error {
		if b64 == "" {
			return nil
		}
		if _, err := base64.StdEncoding.DecodeString(b64); err != nil {
			return err
		}
		if mt == "" {
			mt = "image/png"
		}
		item := gin.H{}
		if a.imageFormat == "url" {
			item["url"] = "data:" + mt + ";base64," + b64
		} else {
			item["b64_json"] = b64
		}
		if prompt != "" {
			item["revised_prompt"] = prompt
		}
		items = append(items, item)
		return nil
	}
	for _, p := range envelope.Predictions {
		if p.Filtered != "" {
			continue
		}
		if p.Bytes == "" && p.GCSURI != "" {
			object, err := FetchStorageObject(c.Request.Context(), p.GCSURI, info.ApiKey, info.ChannelSetting.Proxy)
			if err != nil {
				return nil, badResponse(err)
			}
			data, readErr := io.ReadAll(io.LimitReader(object.Body, 100<<20))
			object.Body.Close()
			if object.StatusCode != http.StatusOK {
				return nil, badResponse(errors.New("unable to download Imagen GCS result"))
			}
			if readErr != nil {
				return nil, badResponse(readErr)
			}
			if len(data) == 0 || len(data) >= 100<<20 {
				return nil, badResponse(errors.New("Imagen GCS result is empty or exceeds 100 MiB"))
			}
			p.Bytes = base64.StdEncoding.EncodeToString(data)
		}
		if err := add(p.Bytes, p.MIME, p.Prompt); err != nil {
			return nil, badResponse(err)
		}
	}
	for _, candidate := range envelope.Candidates {
		for _, p := range candidate.Content.Parts {
			if p.Inline != nil && strings.HasPrefix(p.Inline.MIME, "image/") {
				if err := add(p.Inline.Data, p.Inline.MIME, ""); err != nil {
					return nil, badResponse(err)
				}
			}
		}
	}
	if len(items) == 0 {
		return nil, badResponse(errors.New("Vertex returned no images (empty or safety-filtered result)"))
	}
	c.Set("vertex_completed_images", len(items))
	if info.PriceData.UsePrice {
		info.PriceData.AddOtherRatio("n", float64(len(items)))
		info.NonTokenUsage = true
	}
	c.JSON(http.StatusOK, gin.H{"created": time.Now().Unix(), "data": items, "usage": usage})
	return &usage, nil
}
