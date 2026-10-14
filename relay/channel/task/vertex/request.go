package vertex

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	geminitask "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type videoRequest struct {
	Instances  []map[string]json.RawMessage `json:"instances"`
	Parameters map[string]json.RawMessage   `json:"parameters"`
}

func videoRaw(v any) json.RawMessage { data, _ := common.Marshal(v); return data }
func videoError(err error) *dto.TaskError {
	return service.TaskErrorWrapperLocal(err, "invalid_vertex_video_request", http.StatusBadRequest)
}

func (a *TaskAdaptor) validateVideoRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	info.Action = constant.TaskActionTextGenerate
	if c.GetBool("vertex_native_video") {
		request, err := dto.ParseVertexRequest(c)
		if err != nil {
			return videoError(err)
		}
		info.OriginModelName = request.Model
		data, err := common.Marshal(request.Fields)
		if err != nil {
			return videoError(err)
		}
		var payload videoRequest
		if err := common.Unmarshal(data, &payload); err != nil {
			return videoError(err)
		}
		c.Set("vertex_video_request", payload)
		c.Set("task_request", relaycommon.TaskSubmitReq{Model: request.Model})
		return nil
	}
	var req relaycommon.TaskSubmitReq
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return videoError(err)
	}
	if req.Model == "" {
		return videoError(errors.New("model is required"))
	}
	info.OriginModelName = req.Model
	instance := map[string]json.RawMessage{}
	if req.Prompt != "" {
		instance["prompt"] = videoRaw(req.Prompt)
	}
	parameters := map[string]json.RawMessage{}
	if nested, ok := req.Metadata["parameters"]; ok {
		if err := common.Unmarshal(videoRaw(nested), &parameters); err != nil {
			return videoError(err)
		}
		if parameters == nil {
			parameters = map[string]json.RawMessage{}
		}
	}
	for k, v := range req.Metadata {
		switch k {
		case "image", "video", "lastFrame", "mask", "referenceImages", "cameraControl":
			instance[k] = videoRaw(v)
		case "parameters":
			continue
		default:
			if _, exists := parameters[k]; exists {
				return videoError(fmt.Errorf("duplicate video parameter %s", k))
			}
			parameters[k] = videoRaw(v)
		}
	}
	if parameters == nil {
		parameters = map[string]json.RawMessage{}
	}
	// Preserve explicit scalar values and reject unsupported multi-video requests.
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		var fields map[string]json.RawMessage
		if err := common.UnmarshalBodyReusable(c, &fields); err != nil {
			return videoError(err)
		}
		for _, key := range []string{"seconds", "duration"} {
			if v, ok := fields[key]; ok {
				s := strings.Trim(string(v), "\"")
				n, err := strconv.ParseFloat(s, 64)
				if err != nil || n <= 0 {
					return videoError(errors.New("seconds and duration must be positive when supplied"))
				}
			}
		}
		for _, key := range []string{"n", "sampleCount"} {
			if v, ok := fields[key]; ok {
				parameters["sampleCount"] = v
			}
		}
	} else {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return videoError(err)
		}
		defer form.RemoveAll()
		for _, key := range []string{"n", "sampleCount", "seconds", "duration"} {
			if values := form.Value[key]; len(values) > 0 {
				if key == "n" || key == "sampleCount" {
					v, err := strconv.Atoi(values[0])
					if err != nil {
						return videoError(err)
					}
					parameters["sampleCount"] = videoRaw(v)
				} else if key == "seconds" {
					req.Seconds = values[0]
				} else {
					v, err := strconv.Atoi(values[0])
					if err != nil {
						return videoError(err)
					}
					req.Duration = v
				}
			}
		}
	}
	image := req.InputReference
	if image == "" {
		image = req.ImageURL
	}
	if image == "" {
		image = req.Image
	}
	if len(req.Images) > 1 {
		return videoError(errors.New("use metadata.referenceImages for multiple reference images"))
	}
	if len(req.Images) == 1 {
		image = req.Images[0]
	}
	if image != "" {
		f, err := videoInput(c, image, "image/png")
		if err != nil {
			return videoError(err)
		}
		instance["image"] = videoRaw(f)
	}
	if img := geminitask.ExtractMultipartImage(c, info); img != nil {
		instance["image"] = videoRaw(img)
	}
	var duration float64
	if value, ok := parameters["durationSeconds"]; ok {
		if err := common.Unmarshal(value, &duration); err != nil {
			return videoError(err)
		}
		if duration <= 0 {
			return videoError(errors.New("durationSeconds must be positive"))
		}
	}
	for _, value := range []string{req.Seconds, strconv.Itoa(req.Duration)} {
		if value == "" || value == "0" {
			continue
		}
		v, err := strconv.ParseFloat(value, 64)
		if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return videoError(errors.New("video duration must be a positive number"))
		}
		if duration != 0 && duration != v {
			return videoError(errors.New("seconds, duration and durationSeconds conflict"))
		}
		duration = v
	}
	if duration != 0 {
		parameters["durationSeconds"] = videoRaw(duration)
	}
	if req.Size != "" {
		if _, ok := parameters["resolution"]; !ok {
			parameters["resolution"] = videoRaw(geminitask.SizeToVeoResolution(req.Size))
		}
		if _, ok := parameters["aspectRatio"]; !ok {
			parameters["aspectRatio"] = videoRaw(geminitask.SizeToVeoAspectRatio(req.Size))
		}
	}
	c.Set("task_request", req)
	c.Set("vertex_video_request", videoRequest{Instances: []map[string]json.RawMessage{instance}, Parameters: parameters})
	return nil
}

func videoInput(c *gin.Context, value, mime string) (map[string]any, error) {
	if strings.HasPrefix(value, "gs://") {
		if _, err := vertexcore.StorageObjectURL(value); err != nil {
			return nil, err
		}
		return map[string]any{"gcsUri": value, "mimeType": mime}, nil
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		f, err := service.GetFileBase64FromUrl(c, value, "Vertex Veo input")
		if err != nil {
			return nil, err
		}
		return map[string]any{"bytesBase64Encoded": f.Base64Data, "mimeType": f.MimeType}, nil
	}
	if strings.HasPrefix(value, "data:") {
		parts := strings.SplitN(value, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return nil, errors.New("invalid video input data URL")
		}
		mime = strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64")
		value = parts[1]
	}
	if b, err := base64.StdEncoding.DecodeString(value); err != nil || len(b) == 0 {
		return nil, errors.New("video input requires valid base64 or GCS URI")
	}
	return map[string]any{"bytesBase64Encoded": value, "mimeType": mime}, nil
}

func (a *TaskAdaptor) ValidateMappedRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	cap, err := vertexmodel.Resolve(info.UpstreamModelName)
	if err != nil {
		return videoError(err)
	}
	if cap.Family != vertexmodel.Video {
		return videoError(errors.New("Vertex video requests require a Veo model"))
	}
	if info.ChannelOtherSettings.VertexKeyType == dto.VertexKeyTypeAPIKey {
		return videoError(errors.New("Veo requires service-account JSON credentials"))
	}
	value, ok := c.Get("vertex_video_request")
	if !ok {
		return videoError(errors.New("missing video request"))
	}
	payload := value.(videoRequest)
	data, err := common.Marshal(payload)
	if err != nil {
		return videoError(err)
	}
	if len(info.ParamOverride) > 0 {
		data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
		if err != nil {
			return videoError(err)
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			return videoError(err)
		}
	}
	if len(payload.Instances) != 1 {
		return videoError(errors.New("Veo requires exactly one instance"))
	}
	if payload.Parameters == nil {
		payload.Parameters = map[string]json.RawMessage{}
	}
	p := payload.Parameters
	i := payload.Instances[0]
	count := 1
	if v, ok := p["sampleCount"]; ok {
		if err := common.Unmarshal(v, &count); err != nil {
			return videoError(err)
		}
	}
	if count < 1 || count > 4 {
		return videoError(errors.New("Veo sampleCount must be between 1 and 4"))
	}
	if !c.GetBool("vertex_native_video") && count != 1 {
		return videoError(errors.New("compatible video requests require sampleCount=1; native requests support multiple results"))
	}
	duration := float64(8)
	if v, ok := p["durationSeconds"]; ok {
		if err := common.Unmarshal(v, &duration); err != nil {
			return videoError(err)
		}
	}
	if duration <= 0 {
		return videoError(errors.New("durationSeconds must be positive"))
	}
	p["sampleCount"] = videoRaw(count)
	p["durationSeconds"] = videoRaw(duration)
	present := func(key string) bool { v, ok := i[key]; return ok && string(v) != "null" }
	kinds := 0
	for _, key := range []string{"image", "video", "referenceImages"} {
		if present(key) {
			kinds++
		}
	}
	if kinds > 1 {
		return videoError(errors.New("image, video and referenceImages are mutually exclusive"))
	}
	if present("lastFrame") && !present("image") {
		return videoError(errors.New("lastFrame requires image"))
	}
	if present("mask") && !present("video") {
		return videoError(errors.New("video mask requires an input video"))
	}
	var prompt string
	_ = common.Unmarshal(i["prompt"], &prompt)
	if strings.TrimSpace(prompt) == "" && !present("video") {
		return videoError(errors.New("prompt or an input video is required"))
	}
	for _, key := range []string{"image", "video", "lastFrame", "mask"} {
		if present(key) {
			if err := validateVideoMedia(i[key]); err != nil {
				return videoError(fmt.Errorf("%s: %w", key, err))
			}
		}
	}
	if present("referenceImages") {
		var refs []struct {
			Image json.RawMessage `json:"image"`
			Type  string          `json:"referenceType"`
		}
		if err := common.Unmarshal(i["referenceImages"], &refs); err != nil {
			return videoError(err)
		}
		assets, styles := 0, 0
		for _, ref := range refs {
			if err := validateVideoMedia(ref.Image); err != nil {
				return videoError(err)
			}
			switch ref.Type {
			case "asset":
				assets++
			case "style":
				styles++
			default:
				return videoError(errors.New("referenceType must be asset or style"))
			}
		}
		if len(refs) == 0 || assets > 3 || styles > 1 || (assets > 0 && styles > 0) {
			return videoError(errors.New("use up to three asset references or one style reference"))
		}
	}
	if kinds > 0 {
		info.Action = constant.TaskActionGenerate
	}
	data, err = common.Marshal(payload)
	if err != nil {
		return videoError(err)
	}
	c.Set("vertex_video_body", data)
	c.Set("vertex_video_request", payload)
	return nil
}

func validateVideoMedia(data []byte) error {
	var m map[string]json.RawMessage
	if err := common.Unmarshal(data, &m); err != nil {
		return err
	}
	if nested, ok := m["image"]; ok {
		return validateVideoMedia(nested)
	}
	var b64, gcs string
	_ = common.Unmarshal(m["bytesBase64Encoded"], &b64)
	_ = common.Unmarshal(m["gcsUri"], &gcs)
	if (b64 == "") == (gcs == "") {
		return errors.New("provide exactly one of bytesBase64Encoded and gcsUri")
	}
	if gcs != "" {
		_, err := vertexcore.StorageObjectURL(gcs)
		return err
	}
	if data, err := base64.StdEncoding.DecodeString(b64); err != nil || len(data) == 0 {
		return errors.New("invalid base64 media")
	}
	return nil
}

func (a *TaskAdaptor) videoBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	v, ok := c.Get("vertex_video_request")
	if !ok {
		return nil
	}
	p := v.(videoRequest).Parameters
	var seconds, count float64
	var resolution string
	_ = common.Unmarshal(p["durationSeconds"], &seconds)
	_ = common.Unmarshal(p["sampleCount"], &count)
	_ = common.Unmarshal(p["resolution"], &resolution)
	return map[string]float64{"seconds": seconds, "n": count, "resolution": geminitask.VeoResolutionRatio(info.UpstreamModelName, resolution)}
}
func (a *TaskAdaptor) videoBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	if _, ok := c.Get("vertex_video_body"); !ok {
		if err := a.ValidateMappedRequest(c, info); err != nil {
			return nil, errors.New(err.Message)
		}
	}
	data, _ := c.Get("vertex_video_body")
	return bytes.NewReader(data.([]byte)), nil
}
