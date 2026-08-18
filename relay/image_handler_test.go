package relay

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

func TestShouldPassThroughImageRequestSkipsAgnesAI(t *testing.T) {
	original := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	defer func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = original
	}()

	model_setting.GetGlobalSettings().PassThroughRequestEnabled = true
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeAgnesAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}

	if shouldPassThroughImageRequest(info) {
		t.Fatal("expected Agnes AI image requests to be converted even when pass-through is enabled")
	}
}

func TestShouldPassThroughImageRequestHonorsNonAgnesChannels(t *testing.T) {
	original := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	defer func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = original
	}()

	model_setting.GetGlobalSettings().PassThroughRequestEnabled = false
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}

	if !shouldPassThroughImageRequest(info) {
		t.Fatal("expected non-Agnes image requests to honor channel pass-through")
	}
}

func TestImageEditParamOverrideTakesPriorityOverPassThrough(t *testing.T) {
	original := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	t.Cleanup(func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = original
	})

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ParamOverride: map[string]interface{}{
				"response_format": "b64_json",
			},
			ChannelSetting: dto.ChannelSettings{PassThroughBodyEnabled: true},
		},
	}

	model_setting.GetGlobalSettings().PassThroughRequestEnabled = false
	if !shouldPassThroughImageRequest(info) {
		t.Fatal("expected channel pass-through to be enabled")
	}
	if !shouldApplyImageEditParamOverride(info) {
		t.Fatal("expected parameter override to take priority over channel pass-through")
	}

	info.ChannelSetting.PassThroughBodyEnabled = false
	model_setting.GetGlobalSettings().PassThroughRequestEnabled = true
	if !shouldPassThroughImageRequest(info) {
		t.Fatal("expected global pass-through to be enabled")
	}
	if !shouldApplyImageEditParamOverride(info) {
		t.Fatal("expected parameter override to take priority over global pass-through")
	}

	body, contentType := buildImageEditMultipartBody(t, map[string][]string{
		"response_format": {"url"},
	}, nil)
	overriddenBody, overriddenContentType, err := applyImageEditParamOverride(body, contentType, info)
	if err != nil {
		t.Fatalf("apply parameter override to pass-through body: %v", err)
	}
	form := parseImageEditMultipartBody(t, overriddenBody.Bytes(), overriddenContentType)
	assertImageEditFormValue(t, form, "response_format", []string{"b64_json"})

	info.ParamOverride = nil
	if shouldApplyImageEditParamOverride(info) {
		t.Fatal("expected pass-through body without parameter override to remain unchanged")
	}
}

func TestApplyImageEditMultipartParamOverrideLegacyPreservesFiles(t *testing.T) {
	body, contentType := buildImageEditMultipartBody(t, map[string][]string{
		"model":           {"gpt-image-1"},
		"prompt":          {"make it blue"},
		"quality":         {"standard"},
		"response_format": {"url"},
		"custom_field":    {"keep-me"},
	}, []imageEditMultipartTestFile{
		{field: "image", filename: "source.png", contentType: "image/png", data: []byte("source-image")},
		{field: "mask", filename: "mask.webp", contentType: "image/webp", data: []byte("mask-image")},
	})
	info := newImageEditOverrideInfo(map[string]interface{}{
		"response_format": "b64_json",
		"quality":         "high",
		"n":               0,
		"watermark":       false,
		"custom_field":    "must-not-change",
	})

	overriddenBody, overriddenContentType, err := applyImageEditMultipartParamOverride(body, contentType, info)
	if err != nil {
		t.Fatalf("apply multipart parameter override: %v", err)
	}
	if overriddenContentType == contentType {
		t.Fatal("expected rebuilt multipart body to use a new boundary")
	}

	form := parseImageEditMultipartBody(t, overriddenBody.Bytes(), overriddenContentType)
	assertImageEditFormValue(t, form, "response_format", []string{"b64_json"})
	assertImageEditFormValue(t, form, "quality", []string{"high"})
	assertImageEditFormValue(t, form, "n", []string{"0"})
	assertImageEditFormValue(t, form, "watermark", []string{"false"})
	assertImageEditFormValue(t, form, "custom_field", []string{"keep-me"})
	assertImageEditMultipartFile(t, form, "image", "source.png", "image/png", []byte("source-image"))
	assertImageEditMultipartFile(t, form, "mask", "mask.webp", "image/webp", []byte("mask-image"))
}

func TestApplyImageEditMultipartParamOverrideOperations(t *testing.T) {
	originalDebug := common.DebugEnabled
	common.DebugEnabled = true
	t.Cleanup(func() {
		common.DebugEnabled = originalDebug
	})

	body, contentType := buildImageEditMultipartBody(t, map[string][]string{
		"prompt":          {"make it blue"},
		"quality":         {"standard"},
		"response_format": {"url"},
		"custom_field":    {"keep-me"},
	}, nil)
	info := newImageEditOverrideInfo(map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"path": "response_format", "mode": "set", "value": "b64_json"},
			map[string]interface{}{"path": "quality", "mode": "delete"},
			map[string]interface{}{"path": "partial_images", "mode": "set", "value": 0},
			map[string]interface{}{"path": "watermark", "mode": "set", "value": false},
			map[string]interface{}{"path": "style", "mode": "set", "value": []interface{}{"vivid", "natural"}},
			map[string]interface{}{"path": "custom_field", "mode": "set", "value": "must-not-change"},
		},
	})

	overriddenBody, overriddenContentType, err := applyImageEditMultipartParamOverride(body, contentType, info)
	if err != nil {
		t.Fatalf("apply multipart operations override: %v", err)
	}
	form := parseImageEditMultipartBody(t, overriddenBody.Bytes(), overriddenContentType)
	assertImageEditFormValue(t, form, "response_format", []string{"b64_json"})
	assertImageEditFormValue(t, form, "quality", nil)
	assertImageEditFormValue(t, form, "partial_images", []string{"0"})
	assertImageEditFormValue(t, form, "watermark", []string{"false"})
	assertImageEditFormValue(t, form, "style", []string{"vivid", "natural"})
	assertImageEditFormValue(t, form, "custom_field", []string{"keep-me"})

	if len(info.ParamOverrideAudit) == 0 {
		t.Fatal("expected parameter override audit entries")
	}
	foundResponseFormatAudit := false
	for _, entry := range info.ParamOverrideAudit {
		if strings.Contains(entry, "response_format") {
			foundResponseFormatAudit = true
			break
		}
	}
	if !foundResponseFormatAudit {
		t.Fatalf("expected response_format audit entry, got %#v", info.ParamOverrideAudit)
	}
}

func TestApplyImageEditMultipartParamOverrideRejectsObjectField(t *testing.T) {
	body, contentType := buildImageEditMultipartBody(t, map[string][]string{
		"prompt": {"make it blue"},
	}, nil)
	info := newImageEditOverrideInfo(map[string]interface{}{
		"background": map[string]interface{}{"mode": "transparent"},
	})

	_, _, err := applyImageEditMultipartParamOverride(body, contentType, info)
	if err == nil {
		t.Fatal("expected object-valued multipart override to fail")
	}
	if !strings.Contains(err.Error(), "background") {
		t.Fatalf("expected field name in error, got %v", err)
	}
}

func TestApplyImageEditMultipartParamOverridePreservesReturnError(t *testing.T) {
	body, contentType := buildImageEditMultipartBody(t, map[string][]string{
		"prompt": {"make it blue"},
	}, nil)
	info := newImageEditOverrideInfo(map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{
				"mode": "return_error",
				"value": map[string]interface{}{
					"message":     "blocked image edit",
					"status_code": 422,
					"code":        "blocked_image_edit",
					"skip_retry":  true,
				},
			},
		},
	})

	_, _, err := applyImageEditMultipartParamOverride(body, contentType, info)
	if err == nil {
		t.Fatal("expected return_error operation to fail")
	}
	returnErr, ok := relaycommon.AsParamOverrideReturnError(err)
	if !ok {
		t.Fatalf("expected ParamOverrideReturnError, got %T: %v", err, err)
	}
	if returnErr.StatusCode != 422 || returnErr.Code != "blocked_image_edit" || !returnErr.SkipRetry {
		t.Fatalf("unexpected return error: %#v", returnErr)
	}
}

type imageEditMultipartTestFile struct {
	field       string
	filename    string
	contentType string
	data        []byte
}

func newImageEditOverrideInfo(paramOverride map[string]interface{}) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:   constant.ChannelTypeOpenAI,
			ParamOverride: paramOverride,
		},
	}
}

func buildImageEditMultipartBody(
	t *testing.T,
	fields map[string][]string,
	files []imageEditMultipartTestFile,
) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, values := range fields {
		for _, value := range values {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatalf("write multipart field %q: %v", key, err)
			}
		}
	}
	for _, file := range files {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, file.field, file.filename))
		header.Set("Content-Type", file.contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create multipart file %q: %v", file.field, err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatalf("write multipart file %q: %v", file.field, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func parseImageEditMultipartBody(t *testing.T, body []byte, contentType string) *multipart.Form {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse multipart content type: %v", err)
	}
	if mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("unexpected multipart content type %q", contentType)
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(imageEditMultipartMemoryLimit)
	if err != nil {
		t.Fatalf("parse multipart body: %v", err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form
}

func assertImageEditFormValue(t *testing.T, form *multipart.Form, key string, want []string) {
	t.Helper()
	got, ok := form.Value[key]
	if want == nil {
		if ok {
			t.Fatalf("field %q = %#v, want absent", key, got)
		}
		return
	}
	if !ok || len(got) != len(want) {
		t.Fatalf("field %q = %#v, want %#v", key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("field %q = %#v, want %#v", key, got, want)
		}
	}
}

func assertImageEditMultipartFile(
	t *testing.T,
	form *multipart.Form,
	field string,
	filename string,
	contentType string,
	want []byte,
) {
	t.Helper()
	files := form.File[field]
	if len(files) != 1 {
		t.Fatalf("file field %q has %d files, want 1", field, len(files))
	}
	if files[0].Filename != filename {
		t.Fatalf("file field %q filename = %q, want %q", field, files[0].Filename, filename)
	}
	if got := files[0].Header.Get("Content-Type"); got != contentType {
		t.Fatalf("file field %q content type = %q, want %q", field, got, contentType)
	}
	file, err := files[0].Open()
	if err != nil {
		t.Fatalf("open file field %q: %v", field, err)
	}
	got, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		t.Fatalf("read file field %q: %v", field, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file field %q content = %q, want %q", field, got, want)
	}
}
