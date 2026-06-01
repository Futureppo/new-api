package agnes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

func TestConvertImageRequestBuildsAgnesExtraBody(t *testing.T) {
	var request dto.ImageRequest
	err := common.Unmarshal([]byte(`{
		"model": "agnes-image-2.1-flash",
		"prompt": "turn it into a rainy cyberpunk night",
		"size": "1024x768",
		"response_format": "url",
		"extra_body": {
			"image": "https://example.com/input.png"
		}
	}`), &request)
	if err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	converted, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: ModelImage21Flash,
		},
	}, request)
	if err != nil {
		t.Fatalf("convert image request: %v", err)
	}

	data, err := common.Marshal(converted)
	if err != nil {
		t.Fatalf("marshal converted request: %v", err)
	}

	var payload struct {
		Model          string `json:"model"`
		Prompt         string `json:"prompt"`
		Size           string `json:"size"`
		ResponseFormat string `json:"response_format"`
		Image          any    `json:"image"`
		ExtraBody      struct {
			Image          []string `json:"image"`
			ResponseFormat string   `json:"response_format"`
		} `json:"extra_body"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal converted payload: %v", err)
	}

	if payload.Model != ModelImage21Flash {
		t.Fatalf("model = %q, want %q", payload.Model, ModelImage21Flash)
	}
	if payload.Prompt != "turn it into a rainy cyberpunk night" {
		t.Fatalf("prompt = %q", payload.Prompt)
	}
	if payload.Size != "1024x768" {
		t.Fatalf("size = %q", payload.Size)
	}
	if payload.ResponseFormat != "" {
		t.Fatalf("top-level response_format = %q, want omitted", payload.ResponseFormat)
	}
	if payload.Image != nil {
		t.Fatalf("top-level image = %#v, want omitted", payload.Image)
	}
	if len(payload.ExtraBody.Image) != 1 || payload.ExtraBody.Image[0] != "https://example.com/input.png" {
		t.Fatalf("extra_body.image = %#v", payload.ExtraBody.Image)
	}
	if payload.ExtraBody.ResponseFormat != "url" {
		t.Fatalf("extra_body.response_format = %q", payload.ExtraBody.ResponseFormat)
	}
}

func TestConvertImageRequestRejectsMultipleImagesN(t *testing.T) {
	n := uint(2)
	request := dto.ImageRequest{
		Model:  ModelImage21Flash,
		Prompt: "a cute cat",
		N:      &n,
	}

	_, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
	}, request)
	if err == nil {
		t.Fatal("expected n > 1 to be rejected")
	}
}

func TestConvertImageEditsRequestMapsTopLevelImage(t *testing.T) {
	var request dto.ImageRequest
	err := common.Unmarshal([]byte(`{
		"model": "agnes-image-2.1-flash",
		"prompt": "make the cube blue",
		"image": "https://example.com/edit-source.png"
	}`), &request)
	if err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}

	converted, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
	}, request)
	if err != nil {
		t.Fatalf("convert image edits request: %v", err)
	}

	data, err := common.Marshal(converted)
	if err != nil {
		t.Fatalf("marshal converted request: %v", err)
	}

	var payload struct {
		ExtraBody struct {
			Image []string `json:"image"`
		} `json:"extra_body"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal converted payload: %v", err)
	}

	if len(payload.ExtraBody.Image) != 1 || payload.ExtraBody.Image[0] != "https://example.com/edit-source.png" {
		t.Fatalf("extra_body.image = %#v", payload.ExtraBody.Image)
	}
}

func TestConvertImageEditsRequestRequiresImageURL(t *testing.T) {
	request := dto.ImageRequest{
		Model:  ModelImage21Flash,
		Prompt: "make the cube blue",
	}

	_, err := (&Adaptor{}).ConvertImageRequest(nil, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
	}, request)
	if err == nil {
		t.Fatal("expected image edits without an image URL to be rejected")
	}
}

func TestGetRequestURLEditsUsesGenerationsEndpoint(t *testing.T) {
	got, err := (&Adaptor{}).GetRequestURL(&relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://apihub.agnes-ai.com",
		},
	})
	if err != nil {
		t.Fatalf("get request url: %v", err)
	}

	want := "https://apihub.agnes-ai.com/v1/images/generations"
	if got != want {
		t.Fatalf("request url = %q, want %q", got, want)
	}
}

func TestSetupRequestHeaderForcesJSONForImageEdits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=test")

	header := http.Header{}
	err := (&Adaptor{}).SetupRequestHeader(c, &header, &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "test-key",
		},
	})
	if err != nil {
		t.Fatalf("setup request header: %v", err)
	}

	if got := header.Get("Content-Type"); got != gin.MIMEJSON {
		t.Fatalf("content-type = %q, want %q", got, gin.MIMEJSON)
	}
	if got := header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("authorization = %q", got)
	}
}
