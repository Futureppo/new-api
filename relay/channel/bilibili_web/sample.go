package bilibili_web

import (
	_ "embed"
	"encoding/base64"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// Synthetic English speech ("Hello. Good morning."), generated locally with
// Windows Speech, 16 kHz mono PCM. Channel tests never download external media.
//
//go:embed testdata/hello.wav
var testAudio []byte

func EchoTestRequest(model string) *dto.GeneralOpenAIRequest {
	return &dto.GeneralOpenAIRequest{
		Model: model, Stream: common.GetPointer(false),
		Modalities: []byte(`["text","audio"]`), Audio: []byte(`{"format":"wav","voice":"source"}`),
		BilibiliWeb: []byte(`{"source_language":"en","target_language":"zh"}`),
		Messages: []dto.Message{{Role: "user", Content: []any{map[string]any{
			"type": "input_audio", "input_audio": map[string]string{"data": base64.StdEncoding.EncodeToString(testAudio), "format": "wav"},
		}}}},
	}
}
