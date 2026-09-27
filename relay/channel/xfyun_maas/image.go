package xfyun_maas

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type imageOptions struct {
	Seed           *int64   `json:"seed,omitempty"`
	Steps          *int     `json:"num_inference_steps,omitempty"`
	Guidance       *float64 `json:"guidance_scale,omitempty"`
	Scheduler      *string  `json:"scheduler,omitempty"`
	NegativePrompt *string  `json:"negative_prompt,omitempty"`
}

type imageRequest struct {
	Header struct {
		UID     *string  `json:"uid,omitempty"`
		PatchID []string `json:"patch_id"`
	} `json:"header"`
	Parameter struct {
		Chat struct {
			Domain    string  `json:"domain"`
			Width     int     `json:"width"`
			Height    int     `json:"height"`
			Seed      int64   `json:"seed"`
			Steps     int     `json:"num_inference_steps"`
			Guidance  float64 `json:"guidance_scale"`
			Scheduler string  `json:"scheduler"`
		} `json:"chat"`
	} `json:"parameter"`
	Payload struct {
		Message struct {
			Text []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"text"`
		} `json:"message"`
		NegativePrompts *struct {
			Text string `json:"text"`
		} `json:"negative_prompts,omitempty"`
	} `json:"payload"`
}

func (a *Adaptor) ConvertImageRequest(_ *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	if info.RelayMode != relayconstant.RelayModeImagesGenerations || request.IsStream(nil) {
		return nil, invalid("Xunfei MaaS only supports non-streaming image generation, not image edits")
	}
	if request.N != nil && *request.N != 1 {
		return nil, invalid("Xunfei MaaS image generation only supports n=1")
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "b64_json" {
		return nil, invalid("Xunfei MaaS images only support response_format=b64_json")
	}
	if request.Prompt == "" || utf8.RuneCountInString(request.Prompt) > 1024 {
		return nil, invalid("image prompt must contain 1 to 1024 characters")
	}
	if (request.Quality != "" && request.Quality != "standard") || len(request.Style) > 0 ||
		len(request.Image) > 0 || len(request.Images) > 0 || len(request.Mask) > 0 ||
		len(request.Background) > 0 || len(request.OutputFormat) > 0 || len(request.OutputCompression) > 0 ||
		len(request.PartialImages) > 0 || len(request.InputFidelity) > 0 || request.Watermark != nil {
		return nil, invalid("unsupported image option: Xunfei MaaS supports prompt, size and documented generation parameters")
	}
	size := request.Size
	if size == "" {
		size = "768x768"
	}
	switch size {
	case "768x768", "1024x1024", "576x1024", "768x1024", "1024x576", "1024x768":
	default:
		return nil, invalid("unsupported Xunfei MaaS image size")
	}
	fields := make(map[string]json.RawMessage)
	if len(request.ExtraFields) > 0 {
		if err := common.Unmarshal(request.ExtraFields, &fields); err != nil || fields == nil {
			return nil, invalid("extra_fields must be an object")
		}
	}
	for k, v := range request.Extra {
		// Explicit top-level fields take precedence over the legacy container.
		fields[k] = v
	}
	for k := range fields {
		switch k {
		case "seed", "num_inference_steps", "guidance_scale", "scheduler", "negative_prompt":
		default:
			return nil, invalid("unsupported Xunfei MaaS image parameter: " + k)
		}
	}
	b, err := common.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var opts imageOptions
	if err := common.Unmarshal(b, &opts); err != nil {
		return nil, invalid("invalid Xunfei MaaS image parameters")
	}
	out := &imageRequest{}
	// The live API requires this sentinel even for public, non-fine-tuned models.
	out.Header.PatchID = []string{"0"}
	if len(request.User) > 0 && string(request.User) != "null" {
		var uid string
		if err := common.Unmarshal(request.User, &uid); err != nil || utf8.RuneCountInString(uid) > 32 {
			return nil, invalid("image user must be a string of at most 32 characters")
		}
		out.Header.UID = &uid
	}
	chat := &out.Parameter.Chat
	chat.Domain = request.Model
	dims := strings.Split(size, "x")
	chat.Width, _ = strconv.Atoi(dims[0])
	chat.Height, _ = strconv.Atoi(dims[1])
	if opts.Seed == nil {
		seed, err := rand.Int(rand.Reader, big.NewInt(1<<31))
		if err != nil {
			return nil, err
		}
		chat.Seed = seed.Int64()
	} else {
		if *opts.Seed < 0 || *opts.Seed > 2147483647 {
			return nil, invalid("seed must be between 0 and 2147483647")
		}
		chat.Seed = *opts.Seed
	}
	chat.Steps, chat.Guidance, chat.Scheduler = 20, 5, "DPM++ 2M Karras"
	if opts.Steps != nil {
		if *opts.Steps < 0 || *opts.Steps > 50 {
			return nil, invalid("num_inference_steps must be between 0 and 50")
		}
		chat.Steps = *opts.Steps
	}
	if opts.Guidance != nil {
		if *opts.Guidance < 0 || *opts.Guidance > 20 {
			return nil, invalid("guidance_scale must be between 0 and 20")
		}
		chat.Guidance = *opts.Guidance
	}
	if opts.Scheduler != nil {
		switch *opts.Scheduler {
		case "DPM++ 2M Karras", "DPM++ SDE Karras", "DDIM", "Euler a", "Euler":
			chat.Scheduler = *opts.Scheduler
		default:
			return nil, invalid("unsupported scheduler")
		}
	}
	out.Payload.Message.Text = append(out.Payload.Message.Text, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{"user", request.Prompt})
	if opts.NegativePrompt != nil {
		if utf8.RuneCountInString(*opts.NegativePrompt) > 1024 {
			return nil, invalid("negative_prompt must be at most 1024 characters")
		}
		out.Payload.NegativePrompts = &struct {
			Text string `json:"text"`
		}{*opts.NegativePrompt}
	}
	return out, nil
}

func imageResponse(c *gin.Context, info *relaycommon.RelayInfo, data []byte) (*dto.Usage, *types.NewAPIError) {
	var result struct {
		Payload struct {
			Choices struct {
				Text []struct {
					Content string `json:"content"`
				} `json:"text"`
			} `json:"choices"`
		} `json:"payload"`
	}
	if err := common.Unmarshal(data, &result); err != nil {
		return nil, badResponse(err)
	}
	if len(result.Payload.Choices.Text) != 1 {
		return nil, badResponse(errors.New("expected exactly one generated image"))
	}
	content := result.Payload.Choices.Text[0].Content
	if strings.HasPrefix(content, "data:") {
		parts := strings.SplitN(content, ",", 2)
		if len(parts) != 2 || !strings.Contains(parts[0], ";base64") {
			return nil, badResponse(errors.New("invalid image data URI"))
		}
		content = parts[1]
	}
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil || len(decoded) == 0 {
		return nil, badResponse(errors.New("invalid or empty base64 image"))
	}
	if !strings.HasPrefix(http.DetectContentType(decoded), "image/") {
		return nil, badResponse(errors.New("upstream result is not an image"))
	}
	config, _, _, err := service.DecodeBase64ImageData(content)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, badResponse(errors.New("upstream image cannot be decoded"))
	}
	out := struct {
		Created int64               `json:"created"`
		Data    []map[string]string `json:"data"`
	}{common.GetTimestamp(), []map[string]string{{"b64_json": content}}}
	info.PriceData.AddOtherRatio("n", 1)
	body, err := common.Marshal(out)
	if err != nil {
		return nil, badResponse(err)
	}
	c.Header("Content-Type", "application/json")
	service.IOCopyBytesGracefully(c, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
	return &dto.Usage{}, nil
}
