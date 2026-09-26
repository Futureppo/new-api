package gmicloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	return c.Request.URL.Path == "/v1/images/generations" || c.Request.URL.Path == "/v1/images/edits" || c.Request.URL.Path == "/v1/images/tasks"
}

func parseSyncImageRequest(c *gin.Context) (*TaskRequest, error) {
	var req struct {
		Model          string          `json:"model"`
		Prompt         string          `json:"prompt"`
		Size           *string         `json:"size,omitempty"`
		N              *uint           `json:"n,omitempty"`
		Stream         *bool           `json:"stream,omitempty"`
		ResponseFormat *string         `json:"response_format,omitempty"`
		Seed           *int64          `json:"seed,omitempty"`
		MaxPixels      *int64          `json:"generate_max_pixels,omitempty"`
		Image          json.RawMessage `json:"image,omitempty"`
		Images         json.RawMessage `json:"images,omitempty"`
		Mask           json.RawMessage `json:"mask,omitempty"`
	}
	// HY accepts publicly reachable reference URLs, not multipart uploads.
	// Never silently drop uploaded images and produce text-to-image instead.
	if c.ContentType() != "application/json" {
		return nil, fmt.Errorf("HY requires application/json with image URLs; multipart file uploads are not supported")
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
	if len(req.Mask) != 0 && string(req.Mask) != "null" {
		return nil, fmt.Errorf("HY reference-guided editing does not support mask")
	}
	if len(req.Image) != 0 {
		payload["image"] = req.Image
	}
	if len(req.Images) != 0 {
		payload["images"] = req.Images
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
		// OpenAI clients use "auto"; GMI's Auto value is the empty string.
		// Explicit sizes (including 4096x4096) must not be capped by the
		// generate_max_pixels budget, which only applies in Auto mode.
		if strings.EqualFold(strings.TrimSpace(size), "auto") {
			payload["size"] = json.RawMessage(`""`)
		}
	}
	return nil
}

// Normalize both GMI's image URL(s) and the OpenAI JSON images[].image_url
// shape to the upstream array. The gateway does not download or publish input
// images; the provider fetches them using its documented 20 MB/image limit.
func normalizeImageReferences(payload map[string]json.RawMessage, required bool) (bool, error) {
	if raw, ok := payload["mask"]; ok && string(raw) != "null" {
		return false, fmt.Errorf("HY reference-guided editing does not support mask")
	}
	raw, hasImage := payload["image"]
	images, hasImages := payload["images"]
	if hasImage && hasImages {
		return false, fmt.Errorf("specify only one of image or images")
	}
	var refs []string
	if hasImages {
		var items []struct {
			ImageURL string `json:"image_url"`
			FileID   string `json:"file_id"`
		}
		if common.Unmarshal(images, &items) != nil || items == nil {
			return false, fmt.Errorf("images must be an array of image_url objects")
		}
		for _, item := range items {
			if item.FileID != "" {
				return false, fmt.Errorf("HY does not support file_id; use a public image_url")
			}
			refs = append(refs, item.ImageURL)
		}
	} else if hasImage {
		var single string
		if common.Unmarshal(raw, &single) == nil && string(raw) != "null" {
			refs = []string{single}
		} else if common.Unmarshal(raw, &refs) != nil || refs == nil {
			return false, fmt.Errorf("image must be a public URL or an array of public URLs")
		}
	}
	if len(refs) == 0 {
		if required || hasImage || hasImages {
			return false, fmt.Errorf("at least one reference image URL is required")
		}
		return false, nil
	}
	if len(refs) > 5 {
		return false, fmt.Errorf("HY supports at most 5 reference images")
	}
	for i, ref := range refs {
		refs[i] = strings.TrimSpace(ref)
		u, err := url.Parse(refs[i])
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return false, fmt.Errorf("reference image %d must be a public HTTP(S) URL; data URLs, files and authenticated URLs are not supported", i+1)
		}
	}
	encoded, err := common.Marshal(refs)
	if err != nil {
		return false, err
	}
	payload["image"] = encoded
	delete(payload, "images")
	return true, nil
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
	if constant.IsImageTaskAction(info.Action) && !channelgmicloud.IsImageModel(info.UpstreamModelName) {
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
