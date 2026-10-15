package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterEndpointTypes(t *testing.T) {
	families := map[constant.EndpointType][]string{
		constant.EndpointTypeOpenAIDecisions:    {"~typesafe/jev-latest", "typesafe/jev-1.14", "~perplexity/pplx-decider-latest", "liquid/d1", "inception/mercury-decide:free", "respan/span-01-lite:free", "openai/gpt-6-luna-decisions", "cloudflare/clef-omni"},
		constant.EndpointTypeOpenAIVideo:        {"black-forest-labs/flux-video-edit", "black-forest-labs/flux-3-video", "minimax/hailuo-3", "bytedance/seedance-2.5", "alibaba/wan-3.0", "heygen/avatar-iv", "runway/aleph-2", "kwaivgi/kling-v3.0-pro", "google/veo-3.1", "x-ai/grok-imagine-video-1.5"},
		constant.EndpointTypeEmbeddings:         {"liquid/lfm-2.5-embedding-350m:free", "nvidia/nemotron-3-embed-1b:free", "nvidia/llama-nemotron-embed-vl-1b-v2:free", "voyageai/voyage-code-4", "intfloat/e5-large-v2", "baai/bge-m3", "sentence-transformers/all-minilm-l6-v2"},
		constant.EndpointTypeJinaRerank:         {"nvidia/llama-nemotron-rerank-vl-1b-v2:free", "cohere/rerank-4-pro", "qwen/qwen3-reranker-8b"},
		constant.EndpointTypeImageGeneration:    {"black-forest-labs/flux-3-image", "google/gemini-3.1-flash-image-preview", "recraft/recraft-v4.1-vector", "sourceful/riverflow-v2.5-pro", "microsoft/mai-image-2.6", "qwen/qwen-image-3", "krea/krea-2-medium", "tencent/hy-image-v3.5-preview"},
		constant.EndpointTypeAudioSpeech:        {"deepgram/flux-tts", "fish-audio/s2.1-pro-free:free", "fish-audio/drama-3", "elevenlabs/eleven-v4", "deepgram/aura-2", "minimax/speech-2.8-hd", "canopylabs/orpheus-3b-0.1-ft", "sesame/csm-1b", "hexgrad/kokoro-82m", "bytedance-seed/seed-audio-1-0"},
		constant.EndpointTypeAudioTranscription: {"meta/muse-voice-transcribe-1.0", "fish-audio/transcribe-1", "assemblyai/universal-3-6-pro", "elevenlabs/scribe-v2", "nvidia/nemotron-3.5-asr-streaming-multilingual-0.6b", "deepgram/nova-3", "google/chirp-3", "mistralai/voxtral-mini-3b-2507", "x-ai/grok-stt-1.0"},
	}
	for endpoint, models := range families {
		for _, name := range models {
			t.Run(name, func(t *testing.T) {
				require.Equal(t, []constant.EndpointType{endpoint}, GetEndpointTypesByChannelType(constant.ChannelTypeOpenRouter, name))
			})
		}
	}
	for _, name := range []string{"google/lyria-3-pro-preview", "openai/gpt-audio-mini", "nvidia/nemotron-3.5-super-vl:free", "mistralai/voxtral-small-24b-2507", "unknown/new-model"} {
		require.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAIResponse}, GetEndpointTypesByChannelType(constant.ChannelTypeOpenRouter, name), name)
	}
}
