package gemini

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

const testGifDataURL = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw=="

func newGifFilterRelayInfo(enabled *bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeGemini,
			UpstreamModelName: "gemini-2.5-flash",
			ChannelOtherSettings: dto.ChannelOtherSettings{
				RemoveGifImagesEnabled: enabled,
			},
		},
	}
}

func requireInvalidGifOnlyRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.Equal(t, types.ErrorCodeInvalidRequest, apiErr.GetErrorCode())
	require.Equal(t, emptyAfterGifFilteringMessage, apiErr.Error())
}

func TestConvertGeminiRequestFiltersNativeGifParts(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role: "user",
				Parts: []dto.GeminiPart{
					{Text: "keep me"},
					{InlineData: &dto.GeminiInlineData{MimeType: " Image/GIF ; charset=binary", Data: "gif"}},
					{InlineData: &dto.GeminiInlineData{MimeType: "image/png", Data: "png"}},
					{FileData: &dto.GeminiFileData{MimeType: "image/gif", FileUri: "https://example.com/file"}},
				},
			},
			{
				Role:  "user",
				Parts: []dto.GeminiPart{{InlineData: &dto.GeminiInlineData{MimeType: "image/gif", Data: "gif"}}},
			},
		},
		SystemInstructions: &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{{InlineData: &dto.GeminiInlineData{MimeType: "image/gif", Data: "gif"}}},
		},
	}

	converted, err := (&Adaptor{}).ConvertGeminiRequest(nil, newGifFilterRelayInfo(nil), request)
	require.NoError(t, err)
	got := converted.(*dto.GeminiChatRequest)
	require.Len(t, got.Contents, 1)
	require.Len(t, got.Contents[0].Parts, 2)
	require.Equal(t, "keep me", got.Contents[0].Parts[0].Text)
	require.Equal(t, "image/png", got.Contents[0].Parts[1].InlineData.MimeType)
	require.Nil(t, got.SystemInstructions)
}

func TestConvertGeminiRequestPreservesGifWhenDisabled(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role:  "user",
			Parts: []dto.GeminiPart{{InlineData: &dto.GeminiInlineData{MimeType: "image/gif", Data: "gif"}}},
		}},
	}

	converted, err := (&Adaptor{}).ConvertGeminiRequest(nil, newGifFilterRelayInfo(common.GetPointer(false)), request)
	require.NoError(t, err)
	require.Len(t, converted.(*dto.GeminiChatRequest).Contents[0].Parts, 1)
}

func TestGeminiGifFilterDoesNotApplyToVertex(t *testing.T) {
	info := newGifFilterRelayInfo(nil)
	info.ChannelType = constant.ChannelTypeVertexAi
	require.False(t, isGeminiGifFilterEnabled(info))
}

func TestConvertGeminiRequestRejectsGifOnlyContent(t *testing.T) {
	request := &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{
			Role:  "user",
			Parts: []dto.GeminiPart{{InlineData: &dto.GeminiInlineData{MimeType: "image/gif", Data: "gif"}}},
		}},
	}

	_, err := (&Adaptor{}).ConvertGeminiRequest(nil, newGifFilterRelayInfo(nil), request)
	requireInvalidGifOnlyRequest(t, err)
}

func TestConvertOpenAIRequestFiltersDataURLAndPreservesText(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				dto.MediaContent{Type: dto.ContentTypeText, Text: "before"},
				dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: testGifDataURL}},
				dto.MediaContent{Type: dto.ContentTypeText, Text: "after"},
			},
		}},
	}

	converted, err := CovertOpenAI2Gemini(nil, request, newGifFilterRelayInfo(nil))
	require.NoError(t, err)
	require.Len(t, converted.Contents, 1)
	require.Len(t, converted.Contents[0].Parts, 2)
	require.Equal(t, "before", converted.Contents[0].Parts[0].Text)
	require.Equal(t, "after", converted.Contents[0].Parts[1].Text)
}

func TestConvertOpenAIRequestFiltersMarkdownGifAndPreservesSurroundingText(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role:    "user",
			Content: "before ![gif](" + testGifDataURL + ") after",
		}},
	}

	converted, err := CovertOpenAI2Gemini(nil, request, newGifFilterRelayInfo(nil))
	require.NoError(t, err)
	require.Len(t, converted.Contents, 1)
	var textParts []string
	for _, part := range converted.Contents[0].Parts {
		require.Nil(t, part.InlineData)
		if part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}
	require.Equal(t, "before  after", strings.Join(textParts, ""))
}

func TestConvertOpenAIRequestFiltersRemoteGif(t *testing.T) {
	if service.GetHttpClient() == nil {
		service.InitHttpClient()
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		_, _ = w.Write([]byte("GIF89a"))
	}))
	t.Cleanup(server.Close)

	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = false
	t.Cleanup(func() { *fetchSetting = originalFetchSetting })
	originalMaxFileDownloadMB := constant.MaxFileDownloadMB
	constant.MaxFileDownloadMB = 1
	t.Cleanup(func() { constant.MaxFileDownloadMB = originalMaxFileDownloadMB })

	request := dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				dto.MediaContent{Type: dto.ContentTypeText, Text: "keep"},
				dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: server.URL + "/image"}},
			},
		}},
	}

	converted, err := CovertOpenAI2Gemini(nil, request, newGifFilterRelayInfo(nil))
	require.NoError(t, err)
	require.Len(t, converted.Contents, 1)
	require.Len(t, converted.Contents[0].Parts, 1)
	require.Equal(t, "keep", converted.Contents[0].Parts[0].Text)
}

func TestConvertOpenAIRequestGifDisabledKeepsExistingUnsupportedMimeError(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: testGifDataURL}},
			},
		}},
	}

	_, err := CovertOpenAI2Gemini(nil, request, newGifFilterRelayInfo(common.GetPointer(false)))
	require.ErrorContains(t, err, "mime type is not supported by Gemini: 'image/gif'")
}

func TestConvertOpenAIRequestRejectsGifOnlyContent(t *testing.T) {
	request := dto.GeneralOpenAIRequest{
		Messages: []dto.Message{{
			Role: "user",
			Content: []any{
				dto.MediaContent{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: testGifDataURL}},
			},
		}},
	}

	_, err := CovertOpenAI2Gemini(nil, request, newGifFilterRelayInfo(nil))
	requireInvalidGifOnlyRequest(t, err)
}
