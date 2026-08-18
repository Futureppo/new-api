package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

const imageEditMultipartMemoryLimit = 32 << 20

var imageEditParamOverrideFields = map[string]struct{}{
	"model":              {},
	"prompt":             {},
	"n":                  {},
	"size":               {},
	"quality":            {},
	"response_format":    {},
	"style":              {},
	"user":               {},
	"background":         {},
	"moderation":         {},
	"output_format":      {},
	"output_compression": {},
	"partial_images":     {},
	"watermark":          {},
	"watermark_enabled":  {},
	"user_id":            {},
}

func shouldApplyImageEditParamOverride(info *relaycommon.RelayInfo) bool {
	return info != nil &&
		info.RelayMode == relayconstant.RelayModeImagesEdits &&
		info.ChannelMeta != nil &&
		len(info.ParamOverride) > 0
}

func applyImageEditParamOverride(
	body []byte,
	contentType string,
	info *relaycommon.RelayInfo,
) (*bytes.Buffer, string, error) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		return applyImageEditMultipartParamOverride(body, contentType, info)
	}

	jsonData, err := relaycommon.ApplyParamOverrideWithRelayInfo(body, info)
	if err != nil {
		return nil, contentType, err
	}
	return bytes.NewBuffer(jsonData), contentType, nil
}

func applyImageEditMultipartParamOverride(
	body []byte,
	contentType string,
	info *relaycommon.RelayInfo,
) (*bytes.Buffer, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, contentType, fmt.Errorf("parse image edit content type: %w", err)
	}
	if !strings.EqualFold(mediaType, "multipart/form-data") {
		return nil, contentType, fmt.Errorf("image edit parameter override requires multipart/form-data, got %s", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, contentType, fmt.Errorf("image edit multipart boundary is missing")
	}

	form, err := multipart.NewReader(bytes.NewReader(body), boundary).ReadForm(imageEditMultipartMemoryLimit)
	if err != nil {
		return nil, contentType, fmt.Errorf("parse image edit multipart body: %w", err)
	}
	defer form.RemoveAll()

	overrideInput := make(map[string]any, len(imageEditParamOverrideFields))
	for key := range imageEditParamOverrideFields {
		values, ok := form.Value[key]
		if !ok || len(values) == 0 {
			continue
		}
		if len(values) == 1 {
			overrideInput[key] = values[0]
		} else {
			overrideInput[key] = append([]string(nil), values...)
		}
	}

	jsonData, err := common.Marshal(overrideInput)
	if err != nil {
		return nil, contentType, fmt.Errorf("marshal image edit multipart parameters: %w", err)
	}
	jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
	if err != nil {
		return nil, contentType, err
	}

	var overridden map[string]json.RawMessage
	if err := common.Unmarshal(jsonData, &overridden); err != nil {
		return nil, contentType, fmt.Errorf("decode overridden image edit multipart parameters: %w", err)
	}
	for key := range imageEditParamOverrideFields {
		raw, ok := overridden[key]
		if !ok || common.GetJsonType(raw) == "null" {
			delete(form.Value, key)
			continue
		}

		values, err := imageEditMultipartFieldValues(raw)
		if err != nil {
			return nil, contentType, fmt.Errorf("invalid image edit multipart override for %q: %w", key, err)
		}
		if len(values) == 0 {
			delete(form.Value, key)
		} else {
			form.Value[key] = values
		}
	}

	var rebuilt bytes.Buffer
	writer := multipart.NewWriter(&rebuilt)
	if err := writeImageEditMultipartForm(writer, form); err != nil {
		return nil, contentType, err
	}
	if err := writer.Close(); err != nil {
		return nil, contentType, fmt.Errorf("close rebuilt image edit multipart body: %w", err)
	}

	return &rebuilt, writer.FormDataContentType(), nil
}

func imageEditMultipartFieldValues(raw json.RawMessage) ([]string, error) {
	switch common.GetJsonType(raw) {
	case "string", "number", "boolean":
		value, err := imageEditMultipartScalarValue(raw)
		if err != nil {
			return nil, err
		}
		return []string{value}, nil
	case "array":
		var items []json.RawMessage
		if err := common.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		values := make([]string, 0, len(items))
		for _, item := range items {
			value, err := imageEditMultipartScalarValue(item)
			if err != nil {
				return nil, fmt.Errorf("array item must be a string, number, or boolean: %w", err)
			}
			values = append(values, value)
		}
		return values, nil
	default:
		return nil, fmt.Errorf("value must be a string, number, boolean, array of scalars, or null")
	}
}

func imageEditMultipartScalarValue(raw json.RawMessage) (string, error) {
	switch common.GetJsonType(raw) {
	case "string":
		var value string
		if err := common.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		return value, nil
	case "number", "boolean":
		return string(bytes.TrimSpace(raw)), nil
	default:
		return "", fmt.Errorf("got %s", common.GetJsonType(raw))
	}
}

func writeImageEditMultipartForm(writer *multipart.Writer, form *multipart.Form) error {
	valueKeys := make([]string, 0, len(form.Value))
	for key := range form.Value {
		valueKeys = append(valueKeys, key)
	}
	sort.Strings(valueKeys)
	for _, key := range valueKeys {
		for _, value := range form.Value[key] {
			if err := writer.WriteField(key, value); err != nil {
				return fmt.Errorf("write image edit multipart field %q: %w", key, err)
			}
		}
	}

	fileKeys := make([]string, 0, len(form.File))
	for key := range form.File {
		fileKeys = append(fileKeys, key)
	}
	sort.Strings(fileKeys)
	for _, key := range fileKeys {
		for _, fileHeader := range form.File[key] {
			file, err := fileHeader.Open()
			if err != nil {
				return fmt.Errorf("open image edit multipart file %q: %w", key, err)
			}

			part, createErr := writer.CreatePart(fileHeader.Header)
			if createErr != nil {
				_ = file.Close()
				return fmt.Errorf("create image edit multipart file part %q: %w", key, createErr)
			}
			_, copyErr := io.Copy(part, file)
			closeErr := file.Close()
			if copyErr != nil {
				return fmt.Errorf("copy image edit multipart file %q: %w", key, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close image edit multipart file %q: %w", key, closeErr)
			}
		}
	}
	return nil
}
