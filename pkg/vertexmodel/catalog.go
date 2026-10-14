package vertexmodel

// Catalog contains Vertex IDs rather than Gemini Developer API aliases.
// Resolve remains prefix based so channel-configured versions keep working.
var Catalog = []string{
	"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.5-flash-lite",
	"gemini-3-pro-preview", "gemini-3-flash-preview", "gemini-3.1-pro-preview",
	"gemini-2.5-flash-image", "gemini-3-pro-image-preview", "gemini-3.1-flash-image-preview",
	"gemini-2.5-flash-tts", "gemini-2.5-pro-tts", "gemini-3.1-flash-tts", "gemini-3.8-flash-tts", "gemini-3.5-transcribe",
	"text-embedding-005", "text-multilingual-embedding-002", "gemini-embedding-001", "gemini-embedding-2", "multimodalembedding@001",
	"imagen-3.0-generate-002", "imagen-3.0-capability-001", "imagen-4.0-generate-001", "imagen-4.0-fast-generate-001", "imagen-4.0-ultra-generate-001",
	"veo-2.0-generate-001", "veo-3.0-generate-001", "veo-3.0-fast-generate-001", "veo-3.1-generate-preview", "veo-3.1-fast-generate-preview", "lyria-002",
	"mistralai/mistral-medium-3", "mistralai/mistral-small-2503", "mistralai/mistral-ocr-2505", "mistralai/codestral-2", "ai21/jamba-1.5-large", "xai/grok-4",
	"meta/llama3-405b-instruct-maas",
}
