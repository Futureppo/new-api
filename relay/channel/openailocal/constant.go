package openailocal

const (
	ModelSearch = "gpt-search"
	ModelPPT    = "gpt-image-2-ppt"
	ModelPSD    = "gpt-image-2-psd"

	LegacyModelSearch = "openai-local-search"
	LegacyModelPPT    = "openai-local-ppt"
	LegacyModelPSD    = "openai-local-psd"
)

var ModelList = []string{
	"gpt-image-2",
	ModelPPT,
	ModelPSD,
	"codex-gpt-image-2",
	"auto",
	"gpt-5",
	"gpt-5-1",
	"gpt-5-2",
	"gpt-5-3",
	"gpt-5-3-mini",
	"gpt-5-mini",
	ModelSearch,
	LegacyModelSearch,
	LegacyModelPPT,
	LegacyModelPSD,
}

var ChannelName = "OpenAI-local"
