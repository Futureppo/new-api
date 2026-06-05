package volcengine

import (
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

func normalizeVolcengineImageRequest(request dto.ImageRequest) dto.ImageRequest {
	if request.Size == "" {
		request.Size = "2K"
		return request
	}

	size := strings.ToLower(strings.TrimSpace(request.Size))
	switch size {
	case "256x256", "512x512":
		request.Size = "1K"
	case "1024x1024", "1024x1792", "1792x1024":
		request.Size = "2K"
	}
	return request
}
