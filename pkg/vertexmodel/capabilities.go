// Package vertexmodel describes the supported Vertex inference protocols. It
// deliberately has no relay dependencies so routing and discovery share it.
package vertexmodel

import (
	"fmt"
	"regexp"
	"strings"
)

type Family string

const (
	Gemini              Family = "gemini"
	GeminiImage         Family = "gemini_image"
	Speech              Family = "speech"
	Transcribe          Family = "transcribe"
	Embedding           Family = "embedding"
	MultimodalEmbedding Family = "multimodal_embedding"
	Imagen              Family = "imagen"
	Video               Family = "video"
	Music               Family = "music"
	Claude              Family = "claude"
	PartnerChat         Family = "partner_chat"
	OCR                 Family = "ocr"
	FIM                 Family = "fim"
	OpenAPI             Family = "openapi"
	Grok                Family = "grok"
)

type Capability struct {
	Publisher string
	Model     string
	Family    Family
	Version   string
	Action    string
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@+\-]*$`)

func Resolve(name string) (Capability, error) {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "publishers/") {
		p := strings.Split(name, "/")
		if len(p) != 4 || p[2] != "models" {
			return Capability{}, fmt.Errorf("invalid Vertex model %q", name)
		}
		name = p[1] + "/" + p[3]
	}
	publisher, model := "", name
	if p := strings.Split(name, "/"); len(p) == 2 {
		publisher, model = p[0], p[1]
	} else if len(p) != 1 {
		return Capability{}, fmt.Errorf("invalid Vertex model %q", name)
	}
	if !identifier.MatchString(model) || (publisher != "" && !identifier.MatchString(publisher)) {
		return Capability{}, fmt.Errorf("invalid Vertex model %q", name)
	}
	c := Capability{Publisher: publisher, Model: model, Version: "v1"}
	n := strings.ToLower(model)
	if strings.Contains(n, "live") || strings.Contains(n, "native-audio") || strings.Contains(n, "deep-research") {
		return Capability{}, fmt.Errorf("Vertex model %q requires an unsupported Live or Interactions API", name)
	}
	switch {
	case strings.HasPrefix(n, "claude"):
		c.Publisher, c.Family, c.Action = "anthropic", Claude, "rawPredict"
	case strings.HasPrefix(n, "mistral-ocr"):
		c.Publisher, c.Family, c.Action = "mistralai", OCR, "rawPredict"
	case strings.HasPrefix(n, "codestral"):
		c.Publisher, c.Family, c.Action = "mistralai", FIM, "rawPredict"
	case strings.HasPrefix(n, "mistral") || strings.HasPrefix(n, "ministral"):
		c.Publisher, c.Family, c.Action = "mistralai", PartnerChat, "rawPredict"
	case strings.HasPrefix(n, "jamba"):
		c.Publisher, c.Family, c.Action = "ai21", PartnerChat, "rawPredict"
	case strings.HasPrefix(n, "grok"):
		c.Publisher, c.Family, c.Action = "xai", Grok, "chat/completions"
	case strings.Contains(n, "-maas") || strings.HasPrefix(n, "llama") || isOpenPublisher(publisher):
		if c.Publisher == "" {
			c.Publisher = inferOpenPublisher(n)
		}
		if c.Publisher == "" {
			return Capability{}, fmt.Errorf("Vertex open model %q requires an explicit publisher", model)
		}
		c.Family, c.Action, c.Version = OpenAPI, "chat/completions", "v1beta1"
	case strings.HasPrefix(n, "gemini-embedding"), strings.HasPrefix(n, "text-embedding"), strings.HasPrefix(n, "text-multilingual-embedding"), strings.HasPrefix(n, "textembedding-"):
		c.Publisher, c.Family, c.Action = "google", Embedding, "predict"
		if strings.HasPrefix(n, "gemini-embedding-2") {
			c.Action = "embedContent"
		}
	case strings.HasPrefix(n, "multimodalembedding"):
		c.Publisher, c.Family, c.Action = "google", MultimodalEmbedding, "predict"
	case strings.HasPrefix(n, "imagen"), strings.HasPrefix(n, "imagegeneration"), strings.HasPrefix(n, "virtual-try-on"):
		c.Publisher, c.Family, c.Action = "google", Imagen, "predict"
	case strings.HasPrefix(n, "veo"):
		c.Publisher, c.Family, c.Action = "google", Video, "predictLongRunning"
	case strings.HasPrefix(n, "lyria"):
		c.Publisher, c.Family, c.Action = "google", Music, "predict"
	case strings.HasPrefix(n, "gemini"):
		c.Publisher, c.Family, c.Action = "google", Gemini, "generateContent"
		if strings.Contains(n, "-tts") {
			c.Family = Speech
			if strings.HasPrefix(n, "gemini-2.") || strings.HasPrefix(n, "gemini-3.1-") {
				c.Version = "v1beta1"
			}
		}
		if strings.Contains(n, "-transcribe") {
			c.Family = Transcribe
		}
		if strings.Contains(n, "-image") {
			c.Family = GeminiImage
		}
	default:
		return Capability{}, fmt.Errorf("unsupported Vertex model %q; use a documented publisher/model ID", name)
	}
	if publisher != "" && publisher != c.Publisher {
		return Capability{}, fmt.Errorf("Vertex model %q belongs to publisher %s", model, c.Publisher)
	}
	return c, nil
}

func isOpenPublisher(p string) bool {
	switch p {
	case "meta", "deepseek-ai", "deepseek", "qwen", "openai", "moonshotai", "minimax", "zai-org", "zai":
		return true
	}
	return false
}
func inferOpenPublisher(n string) string {
	switch {
	case strings.HasPrefix(n, "llama"):
		return "meta"
	case strings.HasPrefix(n, "deepseek"):
		return "deepseek-ai"
	case strings.HasPrefix(n, "qwen"):
		return "qwen"
	case strings.HasPrefix(n, "gpt-oss"):
		return "openai"
	case strings.HasPrefix(n, "gemma"):
		return "google"
	}
	return ""
}
func (c Capability) OpenAIModel() string {
	if c.Publisher == "" {
		return c.Model
	}
	return c.Publisher + "/" + c.Model
}
func (c Capability) Allows(action string) bool {
	if action == c.Action {
		return true
	}
	switch c.Family {
	case Gemini, GeminiImage, Speech, Transcribe:
		return action == "streamGenerateContent" || action == "countTokens" || action == "computeTokens"
	case Embedding:
		return action == "embedContent" || action == "batchEmbedContents"
	case Claude, PartnerChat, FIM:
		return action == "streamRawPredict"
	case Video:
		return action == "fetchPredictOperation"
	}
	return false
}
func IsTokenTool(action string) bool { return action == "countTokens" || action == "computeTokens" }
func (c Capability) RequiresCallPrice() bool {
	return c.Family == Imagen || c.Family == Music || c.Family == OCR || c.Family == MultimodalEmbedding
}
func NativeAction(path string) string {
	if strings.HasPrefix(path, "/v1/models/") || strings.HasPrefix(path, "/v1beta/models/") {
		if i := strings.LastIndex(path, ":"); i >= 0 {
			return strings.SplitN(path[i+1:], "?", 2)[0]
		}
	}
	switch path {
	case "/v1/ocr":
		return "rawPredict"
	case "/v1/fim/completions":
		return "rawPredict"
	}
	return ""
}
