package opencode

// Adapted from piexian/new-api (AGPL-3.0), commit afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f.
// https://github.com/piexian/new-api/tree/afd0be51b8e70b922cbd6cd1cc94e1c3a43cf55f/relay/channel/opencode

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var freeCoreTools = []string{"bash", "edit", "glob", "grep", "read"}

func (a *Adaptor) needsFreeCompatibility(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil || info.ChannelType != constant.ChannelTypeOpenCode || info.ChannelSetting.PassThroughBodyEnabled || model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeUnknown && info.RelayMode != relayconstant.RelayModeChatCompletions && info.RelayMode != relayconstant.RelayModeResponses {
		return false
	}
	if a.freeRequestMode != constant.OpenCodeEndpointChat && a.freeRequestMode != constant.OpenCodeEndpointResponses {
		return false
	}
	model := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(info.UpstreamModelName, "models/")))
	return constant.IsOpenCodeFreeChatModel(model)
}

// shapeFreeRequest preserves caller tools; compatibility-only tools are never executable downstream.
func (a *Adaptor) shapeFreeRequest(body []byte) ([]byte, map[string]bool, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, nil, errors.New("invalid request body")
	}
	var tools []json.RawMessage
	rawTools := gjson.GetBytes(body, "tools")
	if rawTools.Exists() && rawTools.Type != gjson.Null {
		if !rawTools.IsArray() || common.Unmarshal([]byte(rawTools.Raw), &tools) != nil {
			return nil, nil, errors.New("invalid tools")
		}
	}
	originalCount := len(tools)
	existing := map[string]bool{}
	for _, tool := range tools {
		path := "name"
		if a.freeRequestMode == constant.OpenCodeEndpointChat {
			path = "function.name"
		}
		if gjson.GetBytes(tool, "type").String() == "function" {
			existing[gjson.GetBytes(tool, path).String()] = true
		}
	}
	injected := map[string]bool{}
	for _, name := range freeCoreTools {
		if existing[name] {
			continue
		}
		function := map[string]any{"name": name, "description": name, "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}
		var tool any = map[string]any{"type": "function", "function": function}
		if a.freeRequestMode == constant.OpenCodeEndpointResponses {
			function["type"] = "function"
			tool = function
		}
		encoded, err := common.Marshal(tool)
		if err != nil {
			return nil, nil, err
		}
		tools = append(tools, encoded)
		injected[name] = true
	}
	var err error
	for path, value := range map[string]any{"stream": true, "tools": tools} {
		body, err = sjson.SetBytes(body, path, value)
		if err != nil {
			return nil, nil, err
		}
	}
	if a.freeRequestMode == constant.OpenCodeEndpointChat {
		body, err = sjson.SetBytes(body, "stream_options.include_usage", true)
		if err != nil {
			return nil, nil, err
		}
		// The Responses free provider only accepts auto; its stream guard remains mandatory.
		if originalCount == 0 && !gjson.GetBytes(body, "tool_choice").Exists() {
			body, err = sjson.SetBytes(body, "tool_choice", "none")
		}
	}
	return body, injected, err
}
