package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const keywordRedirectBufferLimit = 64 << 20

type keywordEdit struct {
	original string
	value    string
}

// Frames stay in wire order until every text fragment they contain is resolved.
// A match spanning frames is written into the first fragment; the consumed
// prefix of subsequent fragments becomes empty. No synthetic SSE events or
// sequence numbers are needed.
type keywordFrame struct {
	raw     []byte
	payload []byte
	event   string
	edits   map[string]*keywordEdit
	pending int
}

func parseKeywordFrame(raw []byte) *keywordFrame {
	f := &keywordFrame{raw: raw}
	var data [][]byte
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\r"))
		if bytes.HasPrefix(line, []byte("data:")) {
			data = append(data, bytes.TrimPrefix(line[5:], []byte(" ")))
		} else if bytes.Equal(line, []byte("data")) {
			data = append(data, nil)
		} else if bytes.HasPrefix(line, []byte("event:")) {
			f.event = strings.TrimSpace(string(line[6:]))
		}
	}
	f.payload = bytes.Join(data, []byte("\n"))
	return f
}

func (f *keywordFrame) get(path string) gjson.Result {
	if path == "" {
		return gjson.ParseBytes(f.payload)
	}
	return gjson.GetBytes(f.payload, path)
}

func (f *keywordFrame) edit(path string) *keywordEdit {
	if edit := f.edits[path]; edit != nil {
		return edit
	}
	value := f.get(path)
	if value.Type != gjson.String {
		return nil
	}
	if f.edits == nil {
		f.edits = make(map[string]*keywordEdit)
	}
	edit := &keywordEdit{original: value.Str, value: value.Str}
	f.edits[path] = edit
	return edit
}

func (f *keywordFrame) renderJSON() ([]byte, error) {
	result := f.payload
	paths := make([]string, 0, len(f.edits))
	for path, edit := range f.edits {
		if edit.value != edit.original {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		value, err := basecommon.Marshal(f.edits[path].value)
		if err != nil {
			return nil, err
		}
		result, err = sjson.SetRawBytes(result, path, value)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (f *keywordFrame) renderSSE() ([]byte, error) {
	payload, err := f.renderJSON()
	if err != nil || bytes.Equal(payload, f.payload) {
		return f.raw, err
	}
	// A multiline JSON payload must not introduce unprefixed SSE data lines.
	// Marshaling RawMessage compacts whitespace without decoding its numbers.
	payload, err = basecommon.Marshal(json.RawMessage(payload))
	if err != nil {
		return nil, err
	}
	lines := bytes.SplitAfter(f.raw, []byte("\n"))
	written := false
	for i, line := range lines {
		body := bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
		if !bytes.HasPrefix(body, []byte("data:")) && !bytes.Equal(body, []byte("data")) {
			continue
		}
		if written {
			lines[i] = nil
			continue
		}
		ending := []byte("\n")
		if bytes.HasSuffix(line, []byte("\r\n")) {
			ending = []byte("\r\n")
		} else if !bytes.HasSuffix(line, []byte("\n")) {
			ending = nil
		}
		lines[i] = append(append([]byte("data: "), payload...), ending...)
		written = true
	}
	return bytes.Join(lines, nil), nil
}

type keywordPiece struct {
	frame *keywordFrame
	edit  *keywordEdit
	left  int
	out   strings.Builder
}

type keywordLane struct {
	text      string
	pieces    []*keywordPiece
	hold      bool // A signature may arrive after the text.
	protected bool
}

func (lane *keywordLane) append(f *keywordFrame, path string, matcher *keywordMatcher) {
	if lane.protected {
		return
	}
	edit := f.edit(path)
	if edit == nil || edit.original == "" {
		return
	}
	f.pending++
	lane.pieces = append(lane.pieces, &keywordPiece{frame: f, edit: edit, left: len(edit.original)})
	if !lane.hold {
		lane.text += edit.original
		lane.process(matcher, false)
	}
}

func (lane *keywordLane) process(matcher *keywordMatcher, final bool) {
	for len(lane.text) != 0 {
		n, replacement := matcher.next(lane.text, final)
		if n == 0 {
			break
		}
		lane.text = lane.text[n:]
		lane.pieces[0].out.WriteString(replacement)
		for n > 0 {
			piece := lane.pieces[0]
			consumed := min(piece.left, n)
			piece.left -= consumed
			n -= consumed
			if piece.left == 0 {
				piece.edit.value = piece.out.String()
				piece.frame.pending--
				lane.pieces[0] = nil
				lane.pieces = lane.pieces[1:]
			}
		}
	}
	// Do not retain a large delta's backing storage for a short unmatched suffix.
	lane.text = strings.Clone(lane.text)
}

func (lane *keywordLane) protect() {
	for _, piece := range lane.pieces {
		piece.edit.value = piece.edit.original
		piece.frame.pending--
	}
	lane.pieces, lane.text = nil, ""
	lane.protected = true
}

type keywordGeminiRun struct {
	kind   string
	number int
	key    string
}

type keywordRedirector struct {
	matcher    *keywordMatcher
	format     types.RelayFormat
	frames     []*keywordFrame
	lanes      map[string]*keywordLane
	blocks     map[string]string
	geminiRuns map[string]*keywordGeminiRun
	queued     int
}

func newKeywordRedirector(format types.RelayFormat, rules map[string]string) *keywordRedirector {
	return &keywordRedirector{
		matcher: newKeywordMatcher(rules), format: format,
		lanes: make(map[string]*keywordLane), blocks: make(map[string]string),
		geminiRuns: make(map[string]*keywordGeminiRun),
	}
}

func (r *keywordRedirector) lane(key string, hold bool) *keywordLane {
	if r.lanes[key] == nil {
		r.lanes[key] = &keywordLane{hold: hold}
	}
	return r.lanes[key]
}

func (r *keywordRedirector) delta(f *keywordFrame, path, key string, hold bool) {
	r.lane(key, hold).append(f, path, r.matcher)
}

func (r *keywordRedirector) finishLane(key string, complete bool) {
	lane := r.lanes[key]
	if lane == nil {
		return
	}
	if lane.hold && !complete {
		// EOF/error before the block's end cannot prove that it was unsigned.
		lane.protect()
	} else if !lane.protected {
		if lane.hold {
			var text strings.Builder
			for _, piece := range lane.pieces {
				text.WriteString(piece.edit.original)
			}
			lane.text = text.String()
		}
		lane.process(r.matcher, true)
	}
	delete(r.lanes, key)
}

func (r *keywordRedirector) finishPrefix(prefix string, complete bool) {
	for key := range r.lanes {
		if strings.HasPrefix(key, prefix) {
			r.finishLane(key, complete)
		}
	}
}

func (r *keywordRedirector) drain() ([][]byte, error) {
	var output [][]byte
	for len(r.frames) != 0 && r.frames[0].pending == 0 {
		frame := r.frames[0]
		data, err := frame.renderSSE()
		if err != nil {
			return nil, err
		}
		output = append(output, data)
		r.queued -= len(frame.raw)
		r.frames[0] = nil
		r.frames = r.frames[1:]
	}
	return output, nil
}

func (r *keywordRedirector) push(raw []byte) ([][]byte, error) {
	if r.queued+len(raw) > keywordRedirectBufferLimit {
		return nil, fmt.Errorf("relay keyword redirect exceeds response buffer limit")
	}
	f := parseKeywordFrame(raw)
	r.frames = append(r.frames, f)
	r.queued += len(raw)
	trimmed := bytes.TrimSpace(f.payload)
	if bytes.Equal(trimmed, []byte("[DONE]")) {
		r.finishPrefix("", false)
	} else if isErrorEventType(f.event) || keywordErrorPayload(f) {
		r.finishPrefix("", false)
		if r.format == types.RelayFormatOpenAIResponses && (f.event == "response.failed" || f.get("type").String() == "response.failed") {
			r.responseSnapshot(f, "response")
		}
	} else if gjson.ValidBytes(f.payload) {
		switch r.format {
		case types.RelayFormatOpenAI:
			r.chatEvent(f)
		case types.RelayFormatOpenAIResponses:
			r.responsesEvent(f)
		case types.RelayFormatClaude:
			r.claudeEvent(f)
		case types.RelayFormatGemini:
			r.geminiEvent(f)
		}
	}
	return r.drain()
}

func (r *keywordRedirector) finish() ([][]byte, error) {
	r.finishPrefix("", false)
	return r.drain()
}

func keywordErrorPayload(f *keywordFrame) bool {
	return isErrorEventType(f.get("type").String()) ||
		(f.get("error").Exists() && f.get("error").Type != gjson.Null)
}

// SSE uses an empty line as its delimiter. Scanning lines also handles mixed
// LF/CRLF endings and transport writes that split the delimiter itself.
func keywordFrameEnd(data []byte) int {
	for start := 0; start < len(data); {
		end := bytes.IndexByte(data[start:], '\n')
		if end < 0 {
			return 0
		}
		end += start
		if len(bytes.TrimSuffix(data[start:end], []byte("\r"))) == 0 {
			return end + 1
		}
		start = end + 1
	}
	return 0
}
