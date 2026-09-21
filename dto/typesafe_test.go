package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestTypeSafeRequestPreservesNativePayload(t *testing.T) {
	for _, state := range []string{`"urgent"`, `{"text":"urgent","enabled":false,"count":0}`, `["urgent",{"text":"help"}]`} {
		body := `{"model":"jev-latest","state":` + state + `,"questions":{"yes":{"type":"noul","instructions":{"text":"urgent?"}},"team":{"type":"choice","instructions":["pick"],"criteria":{"a":null,"b":{"text":"billing"}}},"score":{"type":"score","instructions":"rate","criteria":["low","high"]}},"stream":false,"extra":{"zero":0,"false":false,"large":9007199254740993}}`
		var request TypeSafeRequest
		require.NoError(t, common.Unmarshal([]byte(body), &request))
		data, err := common.Marshal(request)
		require.NoError(t, err)
		require.JSONEq(t, body, string(data))
		require.Contains(t, string(data), "9007199254740993")
		copy := request
		copy.SetModelName("jev-preview")
		mapped, err := common.Marshal(copy)
		require.NoError(t, err)
		require.Contains(t, string(mapped), `"model":"jev-preview"`)
		require.Equal(t, "jev-latest", request.Model)
		require.False(t, request.IsStream(nil))
		require.Contains(t, request.GetTokenCountMeta().CombineText, state)
		require.Contains(t, request.GetTokenCountMeta().CombineText, `"criteria"`)
	}
}

func TestTypeSafeRequestValidation(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"model":false,"state":"x","questions":{}}`,
		`{"model":" ","state":"x","questions":{}}`,
		`{"model":"jev-latest","questions":{}}`,
		`{"model":"jev-latest","state":null,"questions":{}}`,
		`{"model":"jev-latest","state":false,"questions":{}}`,
		`{"model":"jev-latest","state":0,"questions":{}}`,
		`{"model":"jev-latest","state":"x","questions":[]}`,
		`{"model":"jev-latest","state":"x","questions":null}`,
		`{"model":"jev-latest","state":"x","questions":{},"stream":true}`,
		`{"model":"jev-latest","state":"x","questions":{},"stream":"false"}`,
		`{"model":"jev-latest","state":"x","questions":{},"stream_options":{}}`,
	} {
		var request TypeSafeRequest
		require.Error(t, common.Unmarshal([]byte(body), &request), body)
	}
}
