package openaimodel

// Azure capabilities follow Microsoft's model/version and tool support tables:
// https://learn.microsoft.com/azure/foundry/openai/how-to/reasoning
// Deployment IDs are not model IDs; callers must resolve deployment metadata first.
var azureModels = func() map[string]Capabilities {
	m := make(map[string]Capabilities)
	add := func(base, version string, c Capabilities) {
		m[base], m[base+"-"+version] = c, c
	}
	c := Capabilities{CompletionTokens: true, DeveloperRole: true, Reasoning: true,
		Efforts: "none,low,medium,high,xhigh,max", DefaultEffort: "medium",
		Sampling: SamplingWithoutReasoning, Tools: ResponsesReasoningTools}
	add("gpt-6-sol", "2026-09-22", c)
	add("gpt-6-luna", "2026-09-22", c)
	add("gpt-6.1-terra", "2026-10-26", c)
	c.Efforts, c.Sampling, c.Tools = "low,medium,high,xhigh,max", SamplingNever, ResponsesTools
	add("gpt-6-astra", "2026-09-03", c)
	add("gpt-6.1-sol", "2026-09-29", c)
	add("gpt-6.2-astra", "2026-11-17", c)
	add("gpt-6.2-sol", "2026-11-17", c)
	// Azure snapshot dates can differ from the public OpenAI model catalog.
	for _, entry := range []struct{ model, version, source string }{
		{"gpt-5.4-mini", "2026-03-17", "gpt-5.4"},
		{"gpt-5.4-nano", "2026-03-17", "gpt-5.4"},
		{"gpt-5.5", "2026-04-24", "gpt-5.5"},
		{"gpt-5.6-sol", "2026-06-25", "gpt-5.6-sol"},
		{"gpt-5.6-terra", "2026-06-25", "gpt-5.6-terra"},
		{"gpt-5.6-luna", "2026-06-25", "gpt-5.6-luna"},
		{"gpt-5.1-chat", "2025-11-13", "gpt-5.1-chat-latest"},
		{"gpt-5.2-codex", "2026-01-14", "gpt-5.2-codex"},
		{"gpt-5.3-codex", "2026-02-24", "gpt-5.3-codex"},
		{"gpt-5.1-codex-max", "2025-12-04", "gpt-5.1-codex-max"},
	} {
		if caps, ok := Lookup(entry.source); ok {
			add(entry.model, entry.version, caps)
		}
	}
	return m
}()

func ResolveAzure(model string) (base, effort string, capabilities Capabilities, known bool) {
	if c, ok := azureModels[model]; ok {
		return model, "", c, true
	}
	return Resolve(model)
}
