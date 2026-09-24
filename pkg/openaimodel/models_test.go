package openaimodel

import "testing"

func TestResolveExactModels(t *testing.T) {
	for _, name := range []string{"ollama", "orcarouter/model", "openrouter/auto", "openai/gpt-6-astra", "gpt-5-unknown", "gpt-6-astra-2099-01-01", "custom-o3-pro", "gpt-5.2-high-high"} {
		base, effort, _, known := Resolve(name)
		if known || base != name || effort != "" {
			t.Errorf("unexpected model match: %q => %q, %q, %v", name, base, effort, known)
		}
	}
	for _, tc := range []struct{ name, base, effort string }{
		{"o3-mini", "o3-mini", ""},
		{"o3-mini-2025-01-31-high", "o3-mini-2025-01-31", "high"},
		{"gpt-5.1-codex-max", "gpt-5.1-codex-max", ""},
		{"gpt-5.1-codex-max-high", "gpt-5.1-codex-max", "high"},
		{"gpt-6-astra-max", "gpt-6-astra", "max"},
		{"gpt-5.2-2025-12-11-none", "gpt-5.2-2025-12-11", "none"},
		{"gpt-5.6", "gpt-5.6", ""},
	} {
		base, effort, _, known := Resolve(tc.name)
		if !known || base != tc.base || effort != tc.effort {
			t.Errorf("Resolve(%q) = %q, %q, %v", tc.name, base, effort, known)
		}
	}
}

func TestDocumentedEffortsAndSampling(t *testing.T) {
	for _, tc := range []struct {
		model, effort   string
		invalid, remove bool
	}{
		{"gpt-5", "minimal", false, true},
		{"gpt-5", "none", true, true},
		{"gpt-5.1", "", false, false},
		{"gpt-5.2", "none", false, false},
		{"gpt-5.2", "high", false, true},
		{"gpt-5.4", "", false, false},
		{"gpt-5.5", "xhigh", false, false}, // sampling not documented in this profile
		{"gpt-5.6-sol", "max", false, false},
		{"gpt-6-astra", "none", true, true},
		{"gpt-6-astra", "max", false, true},
		{"gpt-6-luna", "", false, true},
		{"gpt-6-sol", "none", false, false},
		{"gpt-6-sol", "minimal", true, true},
		{"o3", "low", false, true},
		{"gpt-5-pro", "low", true, true},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			c, ok := Lookup(tc.model)
			if !ok || (c.ValidateEffort(tc.model, "reasoning_effort", tc.effort) != nil) != tc.invalid || c.RemoveSampling(tc.effort) != tc.remove {
				t.Fatalf("incorrect capabilities for %s/%s: %+v", tc.model, tc.effort, c)
			}
		})
	}
}
