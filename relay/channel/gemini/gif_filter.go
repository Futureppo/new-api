package gemini

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

const emptyAfterGifFilteringMessage = "request contains no content after removing image/gif inputs"

func normalizeMimeType(mimeType string) string {
	mimeType = strings.TrimSpace(mimeType)
	if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}
	return strings.ToLower(mimeType)
}

func isGifMimeType(mimeType string) bool {
	return normalizeMimeType(mimeType) == "image/gif"
}

func isGeminiGifFilterEnabled(info *relaycommon.RelayInfo) bool {
	return info != nil &&
		info.ChannelType == constant.ChannelTypeGemini &&
		info.ChannelOtherSettings.ShouldRemoveGifImages()
}

func filterGifParts(parts []dto.GeminiPart) ([]dto.GeminiPart, int) {
	filtered := make([]dto.GeminiPart, 0, len(parts))
	removed := 0
	for _, part := range parts {
		if (part.InlineData != nil && isGifMimeType(part.InlineData.MimeType)) ||
			(part.FileData != nil && isGifMimeType(part.FileData.MimeType)) {
			removed++
			continue
		}
		filtered = append(filtered, part)
	}
	return filtered, removed
}

func filterGeminiGifImages(request *dto.GeminiChatRequest, info *relaycommon.RelayInfo) error {
	if request == nil || !isGeminiGifFilterEnabled(info) {
		return nil
	}

	removed := 0
	contents := make([]dto.GeminiChatContent, 0, len(request.Contents))
	for _, content := range request.Contents {
		filteredParts, removedFromContent := filterGifParts(content.Parts)
		removed += removedFromContent
		if removedFromContent > 0 && len(filteredParts) == 0 {
			continue
		}
		content.Parts = filteredParts
		contents = append(contents, content)
	}
	request.Contents = contents

	if request.SystemInstructions != nil {
		filteredParts, removedFromSystem := filterGifParts(request.SystemInstructions.Parts)
		removed += removedFromSystem
		if removedFromSystem > 0 && len(filteredParts) == 0 {
			request.SystemInstructions = nil
		} else {
			request.SystemInstructions.Parts = filteredParts
		}
	}

	if removed > 0 && len(request.Contents) == 0 {
		return types.NewErrorWithStatusCode(
			errors.New(emptyAfterGifFilteringMessage),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	return nil
}
