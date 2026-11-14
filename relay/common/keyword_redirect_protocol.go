package common

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/types"
	"github.com/tidwall/gjson"
)

func keywordPath(prefix, suffix string) string {
	if prefix == "" {
		return suffix
	}
	return prefix + "." + suffix
}

func keywordArray(f *keywordFrame, path string, visit func(string, gjson.Result, int)) {
	array := f.get(path)
	if !array.IsArray() {
		return
	}
	array.ForEach(func(key, value gjson.Result) bool {
		i := int(key.Int())
		visit(keywordPath(path, strconv.Itoa(i)), value, i)
		return true
	})
}

func (r *keywordRedirector) text(f *keywordFrame, path string) {
	if edit := f.edit(path); edit != nil {
		edit.value = r.matcher.replace(edit.original)
	}
}

// Only protocol-defined assistant text fields are visited. In particular, tool
// input/arguments, model names, citations, encrypted reasoning and signatures
// are never traversed as arbitrary strings.
func keywordContent(f *keywordFrame, path string, visit func(string)) {
	if f.get(path).Type == gjson.String {
		visit(path)
		return
	}
	keywordArray(f, path, func(partPath string, part gjson.Result, _ int) {
		switch part.Get("type").String() {
		case "text", "output_text":
			visit(partPath + ".text")
		case "refusal":
			visit(partPath + ".refusal")
		}
	})
}

func keywordResponseItem(f *keywordFrame, path string, visit func(string, string)) {
	itemType := f.get(keywordPath(path, "type")).String()
	switch itemType {
	case "message":
		role := f.get(keywordPath(path, "role")).String()
		if role != "" && role != "assistant" {
			return
		}
		keywordArray(f, keywordPath(path, "content"), func(partPath string, part gjson.Result, i int) {
			switch part.Get("type").String() {
			case "output_text", "text":
				visit(partPath+".text", "content/"+strconv.Itoa(i)+"/text")
			case "refusal":
				visit(partPath+".refusal", "content/"+strconv.Itoa(i)+"/refusal")
			}
		})
	case "reasoning":
		keywordArray(f, keywordPath(path, "summary"), func(partPath string, part gjson.Result, i int) {
			if part.Get("type").String() == "summary_text" {
				visit(partPath+".text", "summary/"+strconv.Itoa(i)+"/text")
			}
		})
		keywordArray(f, keywordPath(path, "content"), func(partPath string, part gjson.Result, i int) {
			if part.Get("type").String() == "reasoning_text" {
				visit(partPath+".text", "reasoning/"+strconv.Itoa(i)+"/text")
			}
		})
	}
}

func (r *keywordRedirector) responseSnapshot(f *keywordFrame, prefix string) {
	r.text(f, keywordPath(prefix, "output_text"))
	keywordArray(f, keywordPath(prefix, "output"), func(path string, _ gjson.Result, _ int) {
		keywordResponseItem(f, path, func(path, _ string) { r.text(f, path) })
	})
}

func (r *keywordRedirector) rewriteJSON(data []byte) ([]byte, error) {
	f := &keywordFrame{payload: data}
	if keywordErrorPayload(f) {
		return data, nil
	}
	switch r.format {
	case types.RelayFormatOpenAI:
		keywordArray(f, "choices", func(path string, _ gjson.Result, _ int) {
			role := f.get(path + ".message.role").String()
			if role == "" || role == "assistant" {
				keywordContent(f, path+".message.content", func(path string) { r.text(f, path) })
				for _, field := range []string{"reasoning_content", "reasoning", "refusal"} {
					r.text(f, path+".message."+field)
				}
			}
			r.text(f, path+".text")
		})
	case types.RelayFormatOpenAIResponses:
		r.responseSnapshot(f, "")
	case types.RelayFormatClaude:
		if role := f.get("role").String(); role != "" && role != "assistant" {
			break
		}
		keywordArray(f, "content", func(path string, part gjson.Result, _ int) {
			switch part.Get("type").String() {
			case "text":
				r.text(f, path+".text")
			case "thinking":
				if part.Get("signature").String() == "" {
					r.text(f, path+".thinking")
				}
			}
		})
	case types.RelayFormatGemini:
		if f.get("").IsArray() {
			keywordArray(f, "", func(path string, _ gjson.Result, _ int) {
				r.geminiCandidates(f, path)
			})
		} else {
			r.geminiCandidates(f, "")
		}
		r.finishPrefix("", true)
	}
	return f.renderJSON()
}

func (r *keywordRedirector) chatEvent(f *keywordFrame) {
	keywordArray(f, "choices", func(path string, choice gjson.Result, i int) {
		index := choice.Get("index")
		if index.Exists() {
			i = int(index.Int())
		}
		prefix := "chat/" + strconv.Itoa(i) + "/"
		for _, field := range []string{"reasoning_content", "reasoning"} {
			if f.get(path+".delta."+field).String() != "" {
				r.finishPrefix(prefix+"content/", true)
			}
			r.delta(f, path+".delta."+field, prefix+"thinking/"+field, false)
		}
		keywordContent(f, path+".delta.content", func(fieldPath string) {
			if f.get(fieldPath).String() != "" {
				r.finishPrefix(prefix+"thinking/", true)
			}
			suffix := strings.TrimPrefix(fieldPath, path+".delta.")
			r.delta(f, fieldPath, prefix+"content/"+suffix, false)
		})
		r.delta(f, path+".delta.refusal", prefix+"content/refusal", false)
		r.delta(f, path+".text", prefix+"content/text", false)
		if choice.Get("finish_reason").Type == gjson.String && choice.Get("finish_reason").String() != "" {
			r.finishPrefix(prefix, true)
		}
	})
}

func (r *keywordRedirector) responsesEvent(f *keywordFrame) {
	event := f.get("type").String()
	if event == "" {
		event = f.event
	}
	prefix := "responses/" + strconv.FormatInt(f.get("output_index").Int(), 10) + "/"
	content := strconv.FormatInt(f.get("content_index").Int(), 10)
	summary := strconv.FormatInt(f.get("summary_index").Int(), 10)
	var lane, donePath string
	switch event {
	case "response.output_text.delta", "response.output_text.done":
		lane, donePath = prefix+"content/"+content+"/text", "text"
	case "response.refusal.delta", "response.refusal.done":
		lane, donePath = prefix+"content/"+content+"/refusal", "refusal"
	case "response.reasoning_text.delta", "response.reasoning_text.done":
		lane, donePath = prefix+"reasoning/"+content+"/text", "text"
	case "response.reasoning_summary_text.delta", "response.reasoning_summary_text.done":
		lane, donePath = prefix+"summary/"+summary+"/text", "text"
	}
	if lane != "" {
		if strings.HasSuffix(event, ".delta") {
			r.delta(f, "delta", lane, false)
		} else {
			r.finishLane(lane, true)
			r.text(f, donePath)
		}
		return
	}
	switch event {
	case "response.output_item.added", "response.output_item.done":
		if strings.HasSuffix(event, ".done") {
			r.finishPrefix(prefix, true)
			keywordResponseItem(f, "item", func(path, _ string) { r.text(f, path) })
		} else {
			keywordResponseItem(f, "item", func(path, suffix string) { r.delta(f, path, prefix+suffix, false) })
		}
	case "response.content_part.added", "response.content_part.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		partPrefix := prefix + "content/" + content + "/"
		if strings.Contains(event, "reasoning_summary_part") {
			partPrefix = prefix + "summary/" + summary + "/"
		} else if f.get("part.type").String() == "reasoning_text" {
			partPrefix = prefix + "reasoning/" + content + "/"
		}
		field := "text"
		if f.get("part.type").String() == "refusal" {
			field = "refusal"
		}
		if strings.HasSuffix(event, ".done") {
			r.finishPrefix(partPrefix, true)
			r.text(f, "part."+field)
		} else {
			r.delta(f, "part."+field, partPrefix+field, false)
		}
	case "response.completed", "response.incomplete":
		r.finishPrefix("", false)
		r.responseSnapshot(f, "response")
	case "response.created", "response.in_progress":
		r.responseSnapshot(f, "response")
	}
}

func (r *keywordRedirector) claudeEvent(f *keywordFrame) {
	event := f.get("type").String()
	if event == "" {
		event = f.event
	}
	key := "claude/" + strconv.FormatInt(f.get("index").Int(), 10)
	switch event {
	case "content_block_start":
		blockType := f.get("content_block.type").String()
		r.blocks[key] = blockType
		switch blockType {
		case "thinking":
			lane := r.lane(key, true)
			if f.get("content_block.signature").String() != "" {
				lane.protect()
			}
			r.delta(f, "content_block.thinking", key, true)
		case "text":
			r.delta(f, "content_block.text", key, false)
		}
	case "content_block_delta":
		switch f.get("delta.type").String() {
		case "text_delta":
			if r.blocks[key] == "text" || r.blocks[key] == "" {
				r.delta(f, "delta.text", key, false)
			}
		case "thinking_delta":
			r.delta(f, "delta.thinking", key, true)
		case "signature_delta":
			if f.get("delta.signature").String() != "" {
				r.lane(key, true).protect()
			}
		}
	case "content_block_stop":
		r.finishLane(key, true)
		delete(r.blocks, key)
	case "message_delta", "message_stop":
		r.finishPrefix("claude/", false)
	}
}

func (r *keywordRedirector) geminiEvent(f *keywordFrame) {
	r.geminiCandidates(f, "")
}

func (r *keywordRedirector) geminiCandidates(f *keywordFrame, root string) {
	keywordArray(f, keywordPath(root, "candidates"), func(path string, candidate gjson.Result, i int) {
		if candidate.Get("index").Exists() {
			i = int(candidate.Get("index").Int())
		}
		prefix := "gemini/" + strconv.Itoa(i) + "/"
		run := r.geminiRuns[prefix]
		if run == nil {
			run = &keywordGeminiRun{}
			r.geminiRuns[prefix] = run
		}
		keywordArray(f, path+".content.parts", func(partPath string, part gjson.Result, _ int) {
			signed := part.Get("thoughtSignature").String() != "" || part.Get("thought_signature").String() != ""
			// Gemini has no content_block_stop: a signature can arrive after
			// answer text has begun. Keep unresolved thinking until the
			// candidate ends, and protect it when a later signature arrives.
			if signed {
				for key, lane := range r.lanes {
					if strings.HasPrefix(key, prefix) && lane.hold {
						lane.protect()
					}
				}
			}
			if part.Get("text").Type != gjson.String || part.Get("text").Str == "" {
				if part.Get("functionCall").Exists() || part.Get("inlineData").Exists() || part.Get("executableCode").Exists() {
					if run.kind != "thinking" {
						r.finishLane(run.key, true)
					}
					run.kind = ""
				}
				return
			}
			kind := "content"
			if part.Get("thought").Bool() {
				kind = "thinking"
			}
			if run.kind != kind {
				if run.kind != "thinking" {
					r.finishLane(run.key, true)
				}
				run.number++
				run.kind, run.key = kind, prefix+strconv.Itoa(run.number)
			}
			if signed && kind == "thinking" {
				r.lane(run.key, true).protect()
			}
			r.delta(f, partPath+".text", run.key, kind == "thinking")
		})
		if candidate.Get("finishReason").String() != "" || candidate.Get("finish_reason").String() != "" {
			r.finishPrefix(prefix, true)
			delete(r.geminiRuns, prefix)
		}
	})
}
