package channel

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

// Some providers expose a compatibility format on a provider-specific prefix.
// Compare the operation suffix so an implicit protocol conversion cannot send
// an explicit playground request to a different conversational API.
func matchesPlaygroundPath(endpoint constant.EndpointType, path string) bool {
	path = strings.TrimRight(path, "/")
	switch endpoint {
	case constant.EndpointTypeOpenAI:
		return strings.HasSuffix(path, "/chat/completions")
	case constant.EndpointTypeOpenAIResponse:
		return strings.HasSuffix(path, "/responses")
	case constant.EndpointTypeOpenAIResponseCompact:
		return strings.HasSuffix(path, "/responses/compact")
	case constant.EndpointTypeAnthropic:
		return strings.HasSuffix(path, "/messages") || strings.HasSuffix(path, ":rawPredict") || strings.HasSuffix(path, ":streamRawPredict")
	case constant.EndpointTypeGemini:
		return strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")
	case constant.EndpointTypeTypeSafeSystemOne:
		return strings.HasSuffix(path, "/systemone")
	case constant.EndpointTypeOpenAIDecisions:
		return strings.HasSuffix(path, "/decisions")
	case constant.EndpointTypeCohereChat:
		return strings.HasSuffix(path, "/v2/chat")
	case constant.EndpointTypeCohereRerank:
		return strings.HasSuffix(path, "/v2/rerank")
	case constant.EndpointTypeCohereEmbeddings:
		return strings.HasSuffix(path, "/v2/embed")
	default:
		// Media and task protocols select provider-specific operations in their
		// adapters, including endpoints with no OpenAI-compatible path suffix.
		return true
	}
}
