package common

import (
	"testing"

	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNamedDecisionsPreparationPreservesOriginal(t *testing.T) {
	const body = `{"model":"alias","state":{"large":9007199254740993},"questions":{"yes":{"type":"noul","instructions":"Yes?"}},"provider":{"allow_fallbacks":false},"session_id":"s","trace":{},"extra":false}`
	var request dto.TypeSafeRequest
	require.NoError(t, basecommon.Unmarshal([]byte(body), &request))
	info := &RelayInfo{Request: &request, RelayMode: relayconstant.RelayModeDecisions, OriginModelName: "alias", ChannelMeta: &ChannelMeta{UpstreamModelName: "alias"}}
	for _, channel := range []int{constant.ChannelTypeTypeSafe, constant.ChannelTypeQianwenAI, constant.ChannelTypeOpenRouter} {
		info.ChannelType = channel
		info.ParamOverride = map[string]interface{}{"zero": 0, "trace": map[string]interface{}{"generation_name": "override"}}
		out, err := PrepareNamedDecisionsRequest(info, []byte(body))
		require.NoError(t, err)
		require.Equal(t, "alias", info.GetBillingModelName())
		require.Equal(t, "9007199254740993", gjson.GetBytes(out, "state.large").Raw)
		require.Equal(t, "0", gjson.GetBytes(out, "zero").Raw)
		require.Equal(t, "false", gjson.GetBytes(out, "extra").Raw)
		require.Equal(t, channel == constant.ChannelTypeOpenRouter, gjson.GetBytes(out, "provider").Exists())
		original, err := basecommon.Marshal(request)
		require.NoError(t, err)
		require.JSONEq(t, body, string(original))
	}
}

func TestRequestFilteringAndOverridesPreserveRawNumbers(t *testing.T) {
	const body = `{"state":{"n":9007199254740993,"fraction":0.1234567890123456789,"zero":0,"flag":false,"nil":null},"service_tier":"priority","stream_options":{"include_obfuscation":false,"large":9007199254740993}}`
	filtered, err := RemoveDisabledFields([]byte(body), dto.ChannelOtherSettings{}, false)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(filtered, "service_tier").Exists())
	require.False(t, gjson.GetBytes(filtered, "stream_options.include_obfuscation").Exists())
	require.Equal(t, "9007199254740993", gjson.GetBytes(filtered, "stream_options.large").Raw)
	for _, override := range []map[string]interface{}{
		{"threshold": 0, "key.with.dot": false},
		{"threshold": 0, "operations": []interface{}{map[string]interface{}{"mode": "set", "path": "added", "value": false}}},
	} {
		out, err := ApplyParamOverride(filtered, override, nil)
		require.NoError(t, err)
		require.Equal(t, gjson.Get(body, "state").Raw, gjson.GetBytes(out, "state").Raw)
		require.Equal(t, "0", gjson.GetBytes(out, "threshold").Raw)
	}
}
