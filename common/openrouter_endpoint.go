package common

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

// These are defaults for the channel test picker, not an inference allowlist.
// Match provider-qualified families to avoid treating text models which accept
// video/audio input as models which generate those modalities.
func openRouterEndpointTypes(model string) []constant.EndpointType {
	name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "~")
	prefix := func(values ...string) bool {
		for _, value := range values {
			if strings.HasPrefix(name, value) {
				return true
			}
		}
		return false
	}
	endpoint := constant.EndpointType("")
	switch {
	case prefix("typesafe/jev-", "perplexity/pplx-decider-", "scaledown/classify-", "nace-ai/drex-",
		"microsoft/microsoft-decision-", "cloudflare/clef", "upstage/solar-decide", "openai/gpt-6-luna-decisions",
		"togethercomputer/tev1-", "inception/mercury-decide", "respan/span-", "jaredpalmer/kev-") || strings.Split(name, ":")[0] == "liquid/d1":
		endpoint = constant.EndpointTypeOpenAIDecisions
	case IsVideoGenerationModel(name) || prefix("heygen/heygen-video-", "heygen/avatar-", "black-forest-labs/flux-video-",
		"black-forest-labs/flux-3-video", "minimax/hailuo-", "alibaba/wan-", "alibaba/happyhorse-",
		"bytedance/seedance-", "runway/aleph-", "runway/gen-"):
		endpoint = constant.EndpointTypeOpenAIVideo
	case strings.Contains(name, "rerank"):
		endpoint = constant.EndpointTypeJinaRerank
	case strings.Contains(name, "embed") || prefix("voyageai/voyage-", "thenlper/gte-", "intfloat/", "sentence-transformers/", "baai/bge-"):
		endpoint = constant.EndpointTypeEmbeddings
	case strings.Contains(name, "transcri") || strings.Contains(name, "whisper") || strings.Contains(name, "-asr") || strings.Contains(name, "-stt") ||
		prefix("assemblyai/universal-", "elevenlabs/scribe-", "deepgram/nova-", "nvidia/parakeet-", "google/chirp-", "mistralai/voxtral-mini-3b-2507"):
		endpoint = constant.EndpointTypeAudioTranscription
	case strings.Contains(name, "tts") || strings.Contains(name, "voice") ||
		prefix("fish-audio/s", "fish-audio/drama-", "elevenlabs/eleven-", "deepgram/aura-", "minimax/speech-",
			"canopylabs/orpheus-", "sesame/csm-", "hexgrad/kokoro-", "bytedance-seed/seed-audio-"):
		endpoint = constant.EndpointTypeAudioSpeech
	case IsImageGenerationModel(name) || (prefix("google/gemini-") && strings.Contains(name, "-image")) || prefix("black-forest-labs/flux", "bytedance-seed/seedream-", "recraft/", "google/imagen-",
		"tencent/hy-image-", "microsoft/mai-image-", "meta/muse-image", "qwen/qwen-image-", "x-ai/grok-imagine-image",
		"krea/krea-", "sourceful/riverflow-"):
		endpoint = constant.EndpointTypeImageGeneration
	}
	if endpoint != "" {
		return []constant.EndpointType{endpoint}
	}
	return []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAIResponse}
}
