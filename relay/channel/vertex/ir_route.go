package vertex

import (
	"strings"

	"github.com/QuantumNous/new-api/pkg/vertexmodel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/ir"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

// IRTarget describes the wire format, declarative shim, and upstream model override
// required when routing a cross-protocol IR request to Vertex AI.
type IRTarget struct {
	Format       ir.Format
	Shim         *ir.Shim
	ModelRewrite string
	RelayMode    int
	ClearStream  bool // PartnerChat does not accept stream_options
}

// ResolveIRTarget resolves the target wire protocol for a Vertex AI chat/generation request.
// It returns ok=false for native pass-through, Imagen/Embedding/TTS/Transcribe/Video/OCR/FIM models,
// or when initialization failed.
func (a *Adaptor) ResolveIRTarget(info *relaycommon.RelayInfo) (IRTarget, bool) {
	if a == nil || info == nil || info.ChannelMeta == nil || a.initErr != nil || a.native || a.responseKind != "" {
		return IRTarget{}, false
	}

	switch a.RequestMode {
	case RequestModeClaude:
		if a.capability.Family != vertexmodel.Claude {
			return IRTarget{}, false
		}
		return IRTarget{
			Format:    ir.FormatAnthropic,
			Shim:      ir.VertexClaudeShim,
			RelayMode: constant.RelayModeChatCompletions,
		}, true

	case RequestModeGemini:
		if a.capability.Action != "generateContent" ||
			a.capability.Family == vertexmodel.Speech ||
			a.capability.Family == vertexmodel.Transcribe ||
			strings.HasPrefix(info.UpstreamModelName, "imagen") {
			return IRTarget{}, false
		}
		var shim *ir.Shim
		if model_setting.GetGeminiSettings().RemoveFunctionResponseIdEnabled {
			shim = ir.VertexGeminiShim
		}
		return IRTarget{
			Format:    ir.FormatGoogleGenAI,
			Shim:      shim,
			RelayMode: constant.RelayModeGemini,
		}, true

	case RequestModeOpenSource:
		if a.capability.Family == vertexmodel.OCR || a.capability.Family == vertexmodel.FIM {
			return IRTarget{}, false
		}
		if a.capability.Family == vertexmodel.Grok && info.RelayMode == constant.RelayModeResponses {
			// Grok Responses stays on Vertex's native stateless Grok Responses path.
			return IRTarget{
				Format:       ir.FormatOpenAIResponses,
				ModelRewrite: a.capability.OpenAIModel(),
				RelayMode:    constant.RelayModeResponses,
			}, true
		}
		modelRewrite := a.capability.Model
		clearStream := true
		if a.capability.Family == vertexmodel.OpenAPI || a.capability.Family == vertexmodel.Grok {
			modelRewrite = a.capability.OpenAIModel()
			clearStream = false
		}
		return IRTarget{
			Format:       ir.FormatOpenAIChat,
			ModelRewrite: modelRewrite,
			RelayMode:    constant.RelayModeChatCompletions,
			ClearStream:  clearStream,
		}, true
	}

	return IRTarget{}, false
}
