package ir

import (
	"encoding/json"
	"fmt"
)

// ConvertPayload converts a structured request object from sourceFormat into raw target JSON bytes
// via the default registry and TurnBridge, suitable for returning directly as json.RawMessage from Adaptor.Convert*Request.
func ConvertPayload(sourceFormat, targetFormat Format, sourceReq any, targetShim *Shim, bridgeScope string, options map[string]any) (json.RawMessage, error) {
	if sourceReq == nil {
		return nil, fmt.Errorf("ir: request is nil")
	}
	raw, err := json.Marshal(sourceReq)
	if err != nil {
		return nil, fmt.Errorf("ir: marshal source %s request: %w", sourceFormat, err)
	}
	var bridge *TurnBridge
	if bridgeScope != "" {
		bridge = DefaultTurnBridge()
	}
	p, err := NewPipeline(PipelineConfig{
		Bridge:       bridge,
		BridgeScope:  bridgeScope,
		SourceFormat: sourceFormat,
		TargetFormat: targetFormat,
		TargetShim:   targetShim,
		Options:      options,
	})
	if err != nil {
		return nil, err
	}
	out, _, _, err := p.ConvertRequest(raw)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(out), nil
}
