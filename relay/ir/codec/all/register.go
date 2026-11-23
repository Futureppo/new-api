package all

import (
	"github.com/QuantumNous/new-api/relay/ir"
	"github.com/QuantumNous/new-api/relay/ir/codec/anthropic"
	"github.com/QuantumNous/new-api/relay/ir/codec/google"
	"github.com/QuantumNous/new-api/relay/ir/codec/openaichat"
	"github.com/QuantumNous/new-api/relay/ir/codec/openairesponses"
)

func init() {
	Register(ir.DefaultRegistry())
}

// Register binds all four canonical wire protocol codecs onto reg.
func Register(reg *ir.Registry) {
	if reg == nil {
		return
	}
	reg.Register(openaichat.New())
	reg.Register(openairesponses.New())
	reg.Register(anthropic.New())
	reg.Register(google.New())
}
