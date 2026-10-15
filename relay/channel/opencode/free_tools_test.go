package opencode

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func TestFreeToolsHaveCompleteSchemasAndPreserveCallerDefinitions(t *testing.T) {
	for _, mode := range []constant.OpenCodeEndpoint{constant.OpenCodeEndpointChat, constant.OpenCodeEndpointResponses} {
		t.Run(string(mode), func(t *testing.T) {
			a := &Adaptor{freeRequestMode: mode}
			shaped, injected, err := a.shapeFreeRequest([]byte(`{"messages":[],"temperature":0,"store":false}`))
			require.NoError(t, err)
			require.Len(t, injected, len(freeCoreTools))
			require.NotContains(t, injected, "bash")
			var callerTools []json.RawMessage
			for _, tool := range gjson.GetBytes(shaped, "tools").Array() {
				prefix := ""
				if mode == constant.OpenCodeEndpointChat {
					prefix = "function."
				}
				require.True(t, injected[tool.Get(prefix+"name").String()])
				require.NotEmpty(t, tool.Get(prefix+"description").String())
				require.NotEmpty(t, tool.Get(prefix+"parameters.properties").Map(), "empty tool stubs cause FreeTierError")
				// Real agents may use these same names with completely different
				// schemas. Replacing them would change the arguments clients execute.
				custom, err := sjson.Set(tool.Raw, prefix+"parameters", map[string]any{
					"type": "object", "properties": map[string]any{"caller_value": map[string]any{"type": "integer", "default": 0}}, "additionalProperties": false,
				})
				require.NoError(t, err)
				custom, err = sjson.Set(custom, prefix+"description", "caller's description")
				require.NoError(t, err)
				callerTools = append(callerTools, json.RawMessage(custom))
			}
			body, err := common.Marshal(map[string]any{"tools": callerTools, "tool_choice": "auto"})
			require.NoError(t, err)
			shaped, injected, err = a.shapeFreeRequest(body)
			require.NoError(t, err)
			require.Empty(t, injected)
			for i, tool := range gjson.GetBytes(shaped, "tools").Array() {
				require.JSONEq(t, string(callerTools[i]), tool.Raw)
			}
			require.Equal(t, "auto", gjson.GetBytes(shaped, "tool_choice").String())
		})
	}
}
