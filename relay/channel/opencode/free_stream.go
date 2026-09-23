package opencode

// Adapted from piexian/new-api (AGPL-3.0), commit afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f.
// https://github.com/piexian/new-api/tree/afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f/relay/channel/opencode

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/tidwall/gjson"
)

const freeEventLimit = 8 << 20
const freeBufferedLimit = 32 << 20

// freeStream validates complete SSE events before any compatibility-only tool can reach the client.
type freeStream struct {
	body        io.ReadCloser
	scanner     *bufio.Scanner
	mode        constant.OpenCodeEndpoint
	injected    map[string]bool
	names       map[string]string
	declared    map[string]bool
	unresolved  map[string]bool
	held        []byte
	finished    map[int]bool
	pending     []byte
	done        atomic.Bool
	mu          sync.Mutex
	err         error
	closeOnce   sync.Once
	stopContext func() bool
	timer       *time.Timer
	timeout     time.Duration
}

func newFreeStream(ctx context.Context, body io.ReadCloser, mode constant.OpenCodeEndpoint, injected map[string]bool, timeout time.Duration) *freeStream {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	s := &freeStream{body: body, mode: mode, injected: injected, names: map[string]string{}, finished: map[int]bool{}, timeout: timeout}
	s.unresolved = map[string]bool{}
	s.scanner = bufio.NewScanner(body)
	s.scanner.Buffer(make([]byte, 64<<10), freeEventLimit)
	s.stopContext = context.AfterFunc(ctx, func() { _ = body.Close() })
	s.timer = time.AfterFunc(timeout, func() { _ = body.Close() })
	return s
}

func (s *freeStream) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *freeStream) fail(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
	return s.err
}
func (s *freeStream) Close() error {
	s.closeOnce.Do(func() {
		if !s.done.Load() {
			s.fail(errors.New("upstream stream closed without a terminal event"))
		}
		s.stopContext()
		s.timer.Stop()
		_ = s.body.Close()
	})
	return nil
}
func (s *freeStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(s.pending) == 0 {
		if err := s.Err(); err != nil {
			return 0, err
		}
		if s.done.Load() {
			return 0, io.EOF
		}
		packet, data, err := s.nextEvent()
		if err != nil {
			return 0, s.fail(err)
		}
		if err = s.validate(data); err != nil {
			return 0, s.fail(err)
		}
		if strings.Contains(data, "\n") {
			var raw json.RawMessage
			if err := common.Unmarshal([]byte(data), &raw); err != nil {
				return 0, s.fail(errors.New("invalid upstream stream event"))
			}
			compact, err := common.Marshal(raw)
			if err != nil {
				return 0, s.fail(err)
			}
			var normalized strings.Builder
			for _, line := range strings.Split(string(packet), "\n") {
				if line != "" && !strings.HasPrefix(line, "data:") {
					normalized.WriteString(line + "\n")
				}
			}
			normalized.WriteString("data: " + string(compact) + "\n\n")
			packet = []byte(normalized.String())
		}
		if len(s.unresolved) > 0 || len(s.held) > 0 {
			if len(s.held)+len(packet) > freeEventLimit {
				return 0, s.fail(errors.New("upstream tool name exceeds buffer limit"))
			}
			s.held = append(s.held, packet...)
			if len(s.unresolved) > 0 {
				continue
			}
			packet, s.held = s.held, nil
		}
		s.pending = packet
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *freeStream) nextEvent() ([]byte, string, error) {
	var packet, data strings.Builder
	for s.scanner.Scan() {
		s.timer.Reset(s.timeout)
		line := s.scanner.Text()
		if line == "" {
			if packet.Len() == 0 {
				continue
			}
			packet.WriteByte('\n')
			return []byte(packet.String()), strings.TrimSuffix(data.String(), "\n"), nil
		}
		if packet.Len()+len(line)+2 > freeEventLimit {
			return nil, "", errors.New("upstream stream event exceeds size limit")
		}
		packet.WriteString(line)
		packet.WriteByte('\n')
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimPrefix(line[5:], " "))
			data.WriteByte('\n')
		}
	}
	if err := s.scanner.Err(); err != nil {
		return nil, "", errors.New("upstream stream interrupted")
	}
	if packet.Len() > 0 {
		packet.WriteByte('\n')
		return []byte(packet.String()), strings.TrimSuffix(data.String(), "\n"), nil
	}
	return nil, "", errors.New("upstream stream ended without a terminal event")
}

func (s *freeStream) validate(data string) error {
	if data == "" {
		return nil
	}
	if data == "[DONE]" {
		if s.mode != constant.OpenCodeEndpointChat || len(s.finished) == 0 {
			return errors.New("upstream stream has no completed response")
		}
		for _, finished := range s.finished {
			if !finished {
				return errors.New("upstream stream has an unfinished choice")
			}
		}
		s.done.Store(true)
		return nil
	}
	if !gjson.Valid(data) {
		return errors.New("invalid upstream stream event")
	}
	root := gjson.Parse(data)
	if e := root.Get("error"); e.Exists() && e.Type != gjson.Null {
		return errors.New("upstream stream reported an error")
	}
	if e := root.Get("response.error"); e.Exists() && e.Type != gjson.Null {
		return errors.New("upstream stream reported an error")
	}
	if s.mode == constant.OpenCodeEndpointChat {
		for _, choice := range root.Get("choices").Array() {
			index := int(choice.Get("index").Int())
			if _, ok := s.finished[index]; !ok {
				s.finished[index] = false
			}
			for _, tool := range choice.Get("delta.tool_calls").Array() {
				key := fmt.Sprintf("%d/%d", index, tool.Get("index").Int())
				if err := s.checkToolName(key, tool.Get("function.name").String(), tool.Get("function.arguments").String()); err != nil {
					return err
				}
			}
			if call := choice.Get("delta.function_call"); call.IsObject() {
				if err := s.checkToolName(fmt.Sprintf("%d/legacy", index), call.Get("name").String(), call.Get("arguments").String()); err != nil {
					return err
				}
			}
			if finish := choice.Get("finish_reason"); finish.Exists() && finish.Type != gjson.Null {
				for key := range s.unresolved {
					if strings.HasPrefix(key, fmt.Sprintf("%d/", index)) {
						if s.injected[s.names[key]] || s.names[key] == "" {
							return errors.New("upstream returned a compatibility-only tool call")
						}
						delete(s.unresolved, key)
					}
				}
				s.finished[index] = true
			}
		}
		return nil
	}
	kind := root.Get("type").String()
	switch kind {
	case "error", "response.failed", "response.error":
		return errors.New("upstream stream reported an error")
	case "response.completed", "response.done", "response.incomplete":
		if !root.Get("response").IsObject() {
			return errors.New("upstream stream has no final response")
		}
		s.done.Store(true)
	}
	items := root.Get("response.output").Array()
	if item := root.Get("item"); item.IsObject() {
		items = append(items, item)
	}
	for _, item := range items {
		if item.Get("type").String() == "function_call" && s.injected[item.Get("name").String()] {
			return errors.New("upstream returned a compatibility-only tool call")
		}
	}
	return nil
}

func (s *freeStream) checkToolName(key, fragment, arguments string) error {
	s.names[key] += fragment
	if len(s.names[key]) > 512 || len(s.names) > 2048 {
		return errors.New("upstream tool metadata exceeds size limit")
	}
	delete(s.unresolved, key)
	if !s.injected[s.names[key]] {
		// Hold partial names such as "ba" until they can no longer become an
		// injected tool. In particular, never forward arguments before a name.
		for injected := range s.injected {
			if strings.HasPrefix(injected, s.names[key]) {
				if arguments == "" {
					s.unresolved[key] = true
				}
				if arguments != "" && s.names[key] == "" {
					return errors.New("upstream tool call has arguments before its name")
				}
				break
			}
		}
		return nil
	}
	for declared := range s.declared {
		if strings.HasPrefix(declared, s.names[key]) && declared != s.names[key] {
			s.unresolved[key] = true
			break
		}
	}
	if !s.unresolved[key] || arguments != "" {
		return errors.New("upstream returned a compatibility-only tool call")
	}
	return nil
}
