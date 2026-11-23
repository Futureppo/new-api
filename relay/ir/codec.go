package ir

import (
	"fmt"
	"sync"
)

// SSEFrame represents a single Server-Sent Event frame to emit downstream.
type SSEFrame struct {
	Event string // optional SSE event: name (e.g., "content_block_delta", "response.output_text.delta")
	Data  []byte // raw JSON payload or literal "[DONE]"
}

// Codec implements bidirectional translation between one canonical wire Format and the IR.
type Codec interface {
	Format() Format
	DecodeRequest(raw []byte, ctx *ConversionContext) (*Request, error)
	EncodeRequest(req *Request, ctx *ConversionContext) ([]byte, error)
	DecodeResponse(raw []byte, ctx *ConversionContext) (*Response, error)
	EncodeResponse(resp *Response, ctx *ConversionContext) ([]byte, error)
	DecodeStreamEvent(sseEvent string, rawData []byte, ctx *StreamContext) ([]StreamEvent, error)
	EncodeStreamEvent(ev StreamEvent, ctx *StreamContext) ([]SSEFrame, error)
	FlushStream(ctx *StreamContext) ([]SSEFrame, error)
}

// Registry maps Format identifiers to their Codec implementations.
type Registry struct {
	mu     sync.RWMutex
	codecs map[Format]Codec
}

// NewRegistry creates an empty codec registry.
func NewRegistry() *Registry {
	return &Registry{
		codecs: make(map[Format]Codec),
	}
}

// Register binds a Codec to its Format.
func (r *Registry) Register(codec Codec) {
	if r == nil || codec == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codecs[codec.Format()] = codec
}

// Get returns the Codec registered for format.
func (r *Registry) Get(format Format) (Codec, error) {
	if r == nil {
		return nil, fmt.Errorf("ir: codec registry is nil")
	}
	r.mu.RLock()
	codec, ok := r.codecs[format]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("ir: unsupported format %q", format)
	}
	return codec, nil
}

var defaultRegistry = NewRegistry()

// DefaultRegistry returns the process-wide codec registry populated by codec/all.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// RegisterDefault registers a codec in the default global registry.
func RegisterDefault(codec Codec) {
	defaultRegistry.Register(codec)
}
