package vertexmodel

import "testing"

func TestCapabilityDispatch(t *testing.T) {
	for _, test := range []struct {
		Name, Publisher, Action string
		Family                  Family
	}{
		{"publishers/mistralai/models/mistral-medium-3", "mistralai", "rawPredict", PartnerChat},
		{"ai21/jamba-1.5-large", "ai21", "rawPredict", PartnerChat},
		{"xai/grok-4", "xai", "chat/completions", Grok},
		{"text-embedding-005", "google", "predict", Embedding},
		{"gemini-embedding-2", "google", "embedContent", Embedding},
		{"lyria-002", "google", "predict", Music},
		{"mistralai/mistral-ocr-2505", "mistralai", "rawPredict", OCR},
		{"meta/llama3-405b-instruct-maas", "meta", "chat/completions", OpenAPI},
	} {
		c, err := Resolve(test.Name)
		if err != nil {
			t.Fatal(err)
		}
		if c.Publisher != test.Publisher || c.Action != test.Action || c.Family != test.Family {
			t.Fatalf("%s: %+v", test.Name, c)
		}
		if c.Allows("delete") {
			t.Fatal("resource operation must not be allowed")
		}
	}
	for _, name := range []string{"google/claude-opus-4-6", "../../other", "projects/x/locations/y/models/z", "gemini-live-2.5-flash", "gemini-2.5-flash-native-audio-latest"} {
		if _, err := Resolve(name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, name := range Catalog {
		if _, err := Resolve(name); err != nil {
			t.Fatal(err)
		}
	}
}
