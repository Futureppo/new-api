package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestOpenRouterDecisionsNativeRequest(t *testing.T) {
	input := `{"model":"typesafe/jev-1.14","state":{"enabled":false,"n":0},"questions":{"bug":{"type":"noul","instructions":["Is this broken?"],"criteria":{"true":{},"false":"no"}},"team":{"type":"choice","instructions":"Choose","criteria":{"payments":null,"frontend":[]}},"urgency":{"type":"score","instructions":"How urgent?","criteria":["low",{"high":"checkout down"}]}},"provider":{"allow_fallbacks":false},"trace":{"generation_name":"test"},"stream":false}`
	var request OpenRouterDecisionsRequest
	require.NoError(t, common.Unmarshal([]byte(input), &request))
	output, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, input, string(output))
	require.Contains(t, request.GetTokenCountMeta().CombineText, "checkout down")
	request.SetModelName("upstream/mapped")
	output, err = common.Marshal(request)
	require.NoError(t, err)
	require.Contains(t, string(output), `"model":"upstream/mapped"`)
	for _, questions := range []string{
		`[]`, `{}`, `{"a":{"type":"noul","instructions":true}}`,
		`{"a":{"type":"noul","instructions":"?","criteria":{"true":"yes"}}}`,
		`{"a":{"type":"choice","instructions":"?","criteria":{"x":1}}}`,
		`{"a":{"type":"score","instructions":"?","criteria":[]}}`,
		`{"a":{"type":"score","instructions":"?","criteria":[null]}}`,
	} {
		require.Error(t, common.Unmarshal([]byte(`{"model":"m","state":"s","questions":`+questions+`}`), &request), questions)
	}
}
