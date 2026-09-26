package gmicloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	channelgmicloud "github.com/QuantumNous/new-api/relay/channel/gmicloud"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const ImageResponseFormatKey = "gmicloud_image_response_format"

func IsImageTaskRequest(c *gin.Context) bool {
	return c.Request.URL.Path == "/v1/images/generations" || c.Request.URL.Path == "/v1/images/tasks"
}

func parseSyncImageRequest(c *gin.Context) (*TaskRequest, error) {
	var req struct {
		Model          string  `json:"model"`
		Prompt         string  `json:"prompt"`
		Size           *string `json:"size,omitempty"`
		N              *uint   `json:"n,omitempty"`
		Stream         *bool   `json:"stream,omitempty"`
		ResponseFormat *string `json:"response_format,omitempty"`
		Seed           *int64  `json:"seed,omitempty"`
		MaxPixels      *int64  `json:"generate_max_pixels,omitempty"`
	}
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return nil, err
	}
	if req.N != nil && *req.N != 1 {
		return nil, fmt.Errorf("HY image generation only supports n=1")
	}
	if req.Stream != nil && *req.Stream {
		return nil, fmt.Errorf("HY image generation does not support streaming")
	}
	format := "url"
	if req.ResponseFormat != nil {
		format = *req.ResponseFormat
	}
	if format != "url" && format != "b64_json" {
		return nil, fmt.Errorf("response_format must be url or b64_json")
	}
	c.Set(ImageResponseFormatKey, format)
	payload := map[string]any{"prompt": req.Prompt}
	if req.Size != nil {
		payload["size"] = *req.Size
	}
	if req.Seed != nil {
		payload["seed"] = *req.Seed
	}
	if req.MaxPixels != nil {
		payload["generate_max_pixels"] = *req.MaxPixels
	}
	body, err := common.Marshal(payload)
	return &TaskRequest{Model: req.Model, Payload: body}, err
}

func validateImagePayload(payload map[string]json.RawMessage) error {
	if err := requirePayloadString(payload, "prompt"); err != nil {
		return err
	}
	if _, ok := payload["size"]; ok {
		var size string
		if string(payload["size"]) == "null" || common.Unmarshal(payload["size"], &size) != nil {
			return fmt.Errorf("payload.size must be a string (empty means Auto)")
		}
	}
	return nil
}

func (a *TaskAdaptor) SubmitImageTask(ctx context.Context, baseURL, key, body, proxy string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, channelgmicloud.ResolveTaskBaseURL(baseURL)+channelgmicloud.TaskRequestsPath, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	// Shared clients can have a shorter relay timeout. HY documents a minimum
	// two-minute read timeout; the worker's context supplies its own bound.
	clientCopy := *client
	clientCopy.Timeout = 0
	return clientCopy.Do(req)
}

// Model mapping happens after request validation, so validate the upstream ID
// here rather than rejecting administrator-defined aliases before mapping.
func (a *TaskAdaptor) ValidateMappedRequest(_ *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if info.Action == constant.TaskActionImageGeneration && !channelgmicloud.IsImageModel(info.UpstreamModelName) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("unsupported GMICLOUD image model: %s", info.UpstreamModelName), "unsupported_model", http.StatusBadRequest)
	}
	return nil
}

type ImageResult struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func imageResults(outcome taskOutcome) []ImageResult {
	results := make([]ImageResult, 0)
	seen := make(map[string]bool)
	add := func(item taskMedia) {
		if item.Type != "" && item.Type != "image" {
			return
		}
		url := strings.TrimSpace(item.URL)
		if url == "" || seen[url] {
			return
		}
		seen[url] = true
		results = append(results, ImageResult{URL: url, Width: item.Width, Height: item.Height})
	}
	for _, items := range [][]taskMedia{outcome.MediaURLs, outcome.Medias} {
		for _, item := range items {
			add(item)
		}
	}
	if len(results) == 0 {
		add(taskMedia{URL: outcome.ThumbnailImageURL, Type: "image"})
	}
	return results
}

func ParseImageResults(body []byte) ([]ImageResult, error) {
	var response taskResponse
	if err := common.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	results := imageResults(response.Outcome)
	if len(results) == 0 {
		return nil, fmt.Errorf("GMICLOUD task succeeded without an image URL")
	}
	return results, nil
}
