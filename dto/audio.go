package dto

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type AudioRequest struct {
	// OpenRouter routing and observability extensions.
	Provider               json.RawMessage `json:"provider,omitempty"`
	SessionID              json.RawMessage `json:"session_id,omitempty"`
	Trace                  json.RawMessage `json:"trace,omitempty"`
	InputTurns             []SpeechTurn    `json:"-"`
	InputAudio             json.RawMessage `json:"input_audio,omitempty"`
	InputReferences        json.RawMessage `json:"input_references,omitempty"`
	SourceURL              *string         `json:"source_url,omitempty"`
	Temperature            *float64        `json:"temperature,omitempty"`
	TimestampGranularities []string        `json:"timestamp_granularities,omitempty"`
	Diarize                *bool           `json:"diarize,omitempty"`
	Keyterms               []string        `json:"keyterms,omitempty"`
	User                   json.RawMessage `json:"user,omitempty"`

	Model          string          `json:"model"`
	Input          string          `json:"input"`
	Voice          AudioVoice      `json:"voice"`
	Instructions   string          `json:"instructions,omitempty"`
	ResponseFormat string          `json:"response_format,omitempty"`
	Speed          *float64        `json:"speed,omitempty"`
	StreamFormat   string          `json:"stream_format,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	// vllm-omini
	TaskType                json.RawMessage `json:"task_type,omitempty"`
	Language                json.RawMessage `json:"language,omitempty"`
	RefAudio                json.RawMessage `json:"ref_audio,omitempty"`
	RefText                 json.RawMessage `json:"ref_text,omitempty"`
	XVectorOnlyMode         json.RawMessage `json:"x_vector_only_mode,omitempty"`
	MaxNewTokens            json.RawMessage `json:"max_new_tokens,omitempty"`
	InitialCodecChunkFrames json.RawMessage `json:"initial_codec_chunk_frames,omitempty"`
	Stream                  *bool           `json:"stream,omitempty"`
}

// AudioVoice represents either a built-in voice name or a custom voice reference.
type AudioVoice struct {
	Name string
	ID   string
}

func (v AudioVoice) String() string { return v.Name }

func (v AudioVoice) MarshalJSON() ([]byte, error) {
	if v.ID != "" {
		if v.Name != "" {
			return nil, fmt.Errorf("voice must be a name or an id, not both")
		}
		return common.Marshal(struct {
			ID string `json:"id"`
		}{v.ID})
	}
	return common.Marshal(v.Name)
}

func (v *AudioVoice) UnmarshalJSON(data []byte) error {
	*v = AudioVoice{}
	if common.GetJsonType(data) == "string" {
		return common.Unmarshal(data, &v.Name)
	}
	var ref struct {
		ID string `json:"id"`
	}
	if err := common.Unmarshal(data, &ref); err != nil {
		return fmt.Errorf("voice must be a string or an object with id: %w", err)
	}
	if strings.TrimSpace(ref.ID) == "" {
		return fmt.Errorf("voice object requires a non-empty id")
	}
	v.ID = ref.ID
	return nil
}

func (r *AudioRequest) GetTokenCountMeta() *types.TokenCountMeta {
	meta := &types.TokenCountMeta{
		CombineText: r.Input,
		TokenType:   types.TokenTypeTextNumber,
	}
	if strings.Contains(r.Model, "gpt") {
		meta.TokenType = types.TokenTypeTokenizer
	}
	return meta
}

func (r *AudioRequest) IsStream(c *gin.Context) bool {
	return r.StreamFormat == "sse" || (r.Stream != nil && *r.Stream)
}

func (r *AudioRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

type AudioResponse struct {
	Text string `json:"text"`
}

type WhisperVerboseJSONResponse struct {
	Task     string    `json:"task,omitempty"`
	Language string    `json:"language,omitempty"`
	Duration float64   `json:"duration,omitempty"`
	Text     string    `json:"text,omitempty"`
	Segments []Segment `json:"segments,omitempty"`
}

type Segment struct {
	Id               int     `json:"id"`
	Seek             int     `json:"seek"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	Tokens           []int   `json:"tokens"`
	Temperature      float64 `json:"temperature"`
	AvgLogprob       float64 `json:"avg_logprob"`
	CompressionRatio float64 `json:"compression_ratio"`
	NoSpeechProb     float64 `json:"no_speech_prob"`
}

// SpeechTurn is an OpenRouter multi-speaker input turn.
type SpeechTurn struct {
	Text         string  `json:"text"`
	Voice        *string `json:"voice,omitempty"`
	Instructions *string `json:"instructions,omitempty"`
}

// Keep Input available to existing providers and token estimation while retaining
// the original turns on the wire. Only the OpenRouter channel accepts turns.
func (r *AudioRequest) UnmarshalJSON(data []byte) error {
	type alias AudioRequest
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	input := fields["input"]
	delete(fields, "input")
	rest, err := common.Marshal(fields)
	if err != nil {
		return err
	}
	var a alias
	if err = common.Unmarshal(rest, &a); err != nil {
		return err
	}
	*r = AudioRequest(a)
	if len(input) == 0 || string(input) == "null" {
		return nil
	}
	if common.GetJsonType(input) == "string" {
		return common.Unmarshal(input, &r.Input)
	}
	if err = common.Unmarshal(input, &r.InputTurns); err != nil {
		return fmt.Errorf("input must be a string or speech turns: %w", err)
	}
	if len(r.InputTurns) == 0 {
		return fmt.Errorf("input turns must not be empty")
	}
	var texts []string
	for _, turn := range r.InputTurns {
		if strings.TrimSpace(turn.Text) == "" {
			return fmt.Errorf("speech turn text is required")
		}
		texts = append(texts, turn.Text)
	}
	r.Input = strings.Join(texts, "\n")
	return nil
}
func (r AudioRequest) MarshalJSON() ([]byte, error) {
	type alias AudioRequest
	if len(r.InputTurns) == 0 {
		return common.Marshal(alias(r))
	}
	return common.Marshal(struct {
		alias
		Input []SpeechTurn `json:"input"`
	}{alias(r), r.InputTurns})
}
