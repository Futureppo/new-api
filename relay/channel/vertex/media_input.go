package vertex

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// Read multipart uploads from the reusable body, not Request.MultipartForm:
// validation may already have removed that form's temporary files.
func mediaUploads(c *gin.Context, keys ...string) ([]*types.LocalFileData, error) {
	if !strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data") {
		return nil, nil
	}
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, err
	}
	defer form.RemoveAll()
	var result []*types.LocalFileData
	for _, key := range keys {
		for _, header := range form.File[key] {
			f, err := header.Open()
			if err != nil {
				return nil, err
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				return nil, err
			}
			if len(data) == 0 {
				return nil, errors.New("empty media upload")
			}
			contentType := header.Header.Get("Content-Type")
			if contentType == "" || contentType == "application/octet-stream" {
				contentType = mime.TypeByExtension(filepath.Ext(header.Filename))
				if contentType == "" {
					contentType = http.DetectContentType(data)
				}
			}
			result = append(result, &types.LocalFileData{MimeType: contentType, Base64Data: base64.StdEncoding.EncodeToString(data), Size: int64(len(data))})
		}
	}
	return result, nil
}

func mediaSources(c *gin.Context, data json.RawMessage) ([]*types.LocalFileData, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var source any
	if err := common.Unmarshal(data, &source); err != nil {
		return nil, err
	}
	var values []any
	if list, ok := source.([]any); ok {
		values = list
	} else {
		values = []any{source}
	}
	result := make([]*types.LocalFileData, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("media input must be a URL, data URL, or object")
			}
			s, _ = m["url"].(string)
			if s == "" {
				s, _ = m["image_url"].(string)
			}
			if s == "" {
				b, _ := m["b64_json"].(string)
				mt, _ := m["mime_type"].(string)
				if mt == "" {
					mt = "image/png"
				}
				if b != "" {
					s = "data:" + mt + ";base64," + b
				}
			}
		}
		if s == "" {
			return nil, errors.New("empty media source")
		}
		if strings.HasPrefix(s, "data:") {
			parts := strings.SplitN(s, ",", 2)
			if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
				return nil, errors.New("media data URL must use base64")
			}
			decoded, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil || len(decoded) == 0 {
				return nil, errors.New("invalid media base64")
			}
			result = append(result, &types.LocalFileData{MimeType: strings.TrimSuffix(strings.TrimPrefix(parts[0], "data:"), ";base64"), Base64Data: parts[1], Size: int64(len(decoded))})
			continue
		}
		if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
			return nil, fmt.Errorf("media source must be an HTTP or data URL")
		}
		file, err := service.GetFileBase64FromUrl(c, s, "Vertex media input")
		if err != nil {
			return nil, err
		}
		result = append(result, file)
	}
	return result, nil
}

func inlinePart(f *types.LocalFileData) map[string]any {
	return map[string]any{"inlineData": map[string]any{"mimeType": f.MimeType, "data": f.Base64Data}}
}
