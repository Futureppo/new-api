package mistral

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type embeddingRequest struct {
	Model           string  `json:"model"`
	Input           any     `json:"input"`
	OutputDimension *int    `json:"output_dimension,omitempty"`
	EncodingFormat  *string `json:"encoding_format,omitempty"`
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	switch input := request.Input.(type) {
	case string:
		if input == "" {
			return nil, invalidRequest("input must not be empty")
		}
	case []string:
		if len(input) == 0 {
			return nil, invalidRequest("input must not be empty")
		}
		for _, text := range input {
			if text == "" {
				return nil, invalidRequest("input must contain non-empty strings")
			}
		}
	case []any:
		if len(input) == 0 {
			return nil, invalidRequest("input must not be empty")
		}
		for _, value := range input {
			if text, ok := value.(string); !ok || text == "" {
				return nil, invalidRequest("Mistral embeddings require text strings, not token IDs")
			}
		}
	default:
		return nil, invalidRequest("Mistral embeddings require a string or array of strings")
	}
	if request.EncodingFormat != "" && request.EncodingFormat != "float" && request.EncodingFormat != "base64" {
		return nil, invalidRequest("encoding_format must be float or base64")
	}
	if request.Dimensions != nil && *request.Dimensions <= 0 {
		return nil, invalidRequest("dimensions must be positive")
	}
	a.embeddingFormat = request.EncodingFormat
	// Some Mistral models silently return floats even when base64 is requested.
	// Request floats consistently and encode locally to OpenAI's float32 layout.
	return &embeddingRequest{Model: request.Model, Input: request.Input, OutputDimension: request.Dimensions, EncodingFormat: common.GetPointer("float")}, nil
}

func (a *Adaptor) embeddingResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, badResponse(err)
	}
	if apiErr := errorFromBody(data, http.StatusBadGateway); apiErr != nil {
		return nil, apiErr
	}
	var response map[string]any
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, badResponse(err)
	}
	items, ok := response["data"].([]any)
	if !ok || len(items) == 0 {
		return nil, badResponse(errors.New("Mistral embedding response has no data"))
	}
	for _, value := range items {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, badResponse(errors.New("invalid embedding item"))
		}
		vector, ok := item["embedding"].([]any)
		if !ok || len(vector) == 0 {
			return nil, badResponse(errors.New("invalid embedding vector"))
		}
		encoded := make([]byte, 4*len(vector))
		for i, value := range vector {
			number, ok := value.(float64)
			if !ok || math.IsInf(number, 0) || math.IsNaN(number) || math.Abs(number) > math.MaxFloat32 {
				return nil, badResponse(errors.New("invalid embedding number"))
			}
			if a.embeddingFormat == "base64" {
				binary.LittleEndian.PutUint32(encoded[i*4:], math.Float32bits(float32(number)))
			}
		}
		if a.embeddingFormat == "base64" {
			item["embedding"] = base64.StdEncoding.EncodeToString(encoded)
		}
	}
	normalizeUsageMap(response)
	usage, err := readUsage(response)
	if err != nil {
		return nil, badResponse(err)
	}
	data, err = common.Marshal(response)
	if err != nil {
		return nil, badResponse(err)
	}
	// Do not use the chat handler: ForceFormat would replace data with choices.
	service.IOCopyBytesGracefully(c, resp, data)
	return usage, nil
}
