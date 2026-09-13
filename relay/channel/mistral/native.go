package mistral

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const RealtimePath = "/v1/audio/transcriptions/realtime"

func IsNativePath(path string) bool {
	switch path {
	case "/v1/ocr", "/v1/fim/completions", "/v1/agents/completions", "/v1/audio/speech", RealtimePath:
		return true
	}
	return false
}

// Select native streaming/URL transcription without changing the existing
// OpenAI multipart response_format conversion path.
func UsesNativeProtocol(c *gin.Context) bool {
	if IsNativePath(c.Request.URL.Path) {
		return true
	}
	if c.Request.URL.Path != "/v1/audio/transcriptions" {
		return false
	}
	if c.ContentType() == "application/json" {
		return true
	}
	request, err := dto.ParseMistralNativeRequest(c)
	return err == nil && request.Stream
}

func nativeURL(info *relaycommon.RelayInfo, path string) (string, error) {
	if !IsNativePath(path) && path != "/v1/audio/transcriptions" {
		return "", unsupported()
	}
	base, err := url.Parse(NormalizeBaseURL(info.ChannelBaseUrl))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return "", errors.New("invalid Mistral base URL")
	}
	base.Path = strings.TrimSuffix(strings.TrimRight(base.Path, "/"), "/v1") + path
	base.RawPath, base.Fragment = "", ""
	requestURL, err := url.Parse(info.RequestURLPath)
	if err != nil {
		return "", err
	}
	query := base.Query()
	for key, values := range requestURL.Query() {
		query[key] = values
	}
	if path == RealtimePath {
		query.Set("model", info.UpstreamModelName)
		if base.Scheme == "https" {
			base.Scheme = "wss"
		} else {
			base.Scheme = "ws"
		}
	}
	base.RawQuery = query.Encode()
	return base.String(), nil
}

func (a *Adaptor) ConvertNativeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.MistralNativeRequest) (io.Reader, error) {
	path := c.Request.URL.Path
	if !IsNativePath(path) && path != "/v1/audio/transcriptions" {
		return nil, unsupported()
	}
	a.nativePath, a.nativeContentType = path, request.ContentType
	if path == RealtimePath {
		return nil, nil
	}
	data := request.Body
	mediaType, params, err := mime.ParseMediaType(request.ContentType)
	if err != nil {
		return nil, err
	}
	if mediaType == "application/json" {
		if request.Model != request.OriginalModel {
			var fields map[string]json.RawMessage
			if err := common.Unmarshal(data, &fields); err != nil {
				return nil, err
			}
			fields[request.ModelField], err = common.Marshal(request.Model)
			if err != nil {
				return nil, err
			}
			data, err = common.Marshal(fields)
			if err != nil {
				return nil, err
			}
		}
		if len(info.ParamOverride) > 0 {
			data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
			if err != nil {
				return nil, err
			}
		}
	} else if mediaType == "multipart/form-data" && request.Model != request.OriginalModel {
		reader := multipart.NewReader(bytes.NewReader(data), params["boundary"])
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			dest, err := writer.CreatePart(part.Header)
			if err != nil {
				part.Close()
				return nil, err
			}
			if part.FormName() == "model" && part.FileName() == "" {
				_, err = io.WriteString(dest, request.Model)
			} else {
				_, err = io.Copy(dest, part)
			}
			part.Close()
			if err != nil {
				return nil, err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		data, a.nativeContentType = buffer.Bytes(), writer.FormDataContentType()
	}
	return bytes.NewReader(data), nil
}

func RequiresCallPrice(path string) bool {
	// These endpoints may return pages or raw audio without token usage. Keep
	// configured per-request pricing; never invent token counts from page counts.
	return path == "/v1/ocr" || path == "/v1/audio/speech"
}

func NativeMethodAllowed(method, path string) bool {
	if path == RealtimePath {
		return method == http.MethodGet
	}
	return method == http.MethodPost && (IsNativePath(path) || path == "/v1/audio/transcriptions")
}
