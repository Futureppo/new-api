package controller

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/vertexmodel"
)

func buildVertexNativeTestRequest(model string) *dto.VertexRequest {
	cap, _ := vertexmodel.Resolve(model)
	fields := map[string]json.RawMessage{}
	switch cap.Family {
	case vertexmodel.Music:
		fields["instances"] = json.RawMessage(`[{"prompt":"A gentle acoustic melody"}]`)
	case vertexmodel.OCR:
		fields["document"] = json.RawMessage(`{"type":"document_url","document_url":"https://arxiv.org/pdf/2201.04234"}`)
	case vertexmodel.FIM:
		fields["prompt"] = json.RawMessage(`"def add(a, b):"`)
		fields["suffix"] = json.RawMessage(`"\n"`)
		fields["max_tokens"] = json.RawMessage(`16`)
	case vertexmodel.MultimodalEmbedding:
		fields["instances"] = json.RawMessage(`[{"text":"hello"}]`)
	case vertexmodel.Embedding:
		if cap.Action == "predict" {
			fields["instances"] = json.RawMessage(`[{"content":"hello"}]`)
		} else {
			fields["content"] = json.RawMessage(`{"parts":[{"text":"hello"}]}`)
		}
	case vertexmodel.Imagen:
		fields["instances"] = json.RawMessage(`[{"prompt":"A red apple"}]`)
		fields["parameters"] = json.RawMessage(`{"sampleCount":1}`)
	default:
		fields["contents"] = json.RawMessage(`[{"role":"user","parts":[{"text":"hello"}]}]`)
	}
	return &dto.VertexRequest{Model: model, Action: cap.Action, Fields: fields}
}
