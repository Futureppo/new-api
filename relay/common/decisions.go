package common

import (
	"errors"

	basecommon "github.com/QuantumNous/new-api/common"
	baseconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func IsNamedDecisionsRequest(request dto.Request) bool {
	switch request.(type) {
	case *dto.TypeSafeRequest, *dto.OpenRouterDecisionsRequest:
		return true
	default:
		return false
	}
}

// Only OpenRouter's native preparation bypasses mapping and overrides in
// passthrough mode. TypeSafe and Qianwen retain their existing mapping behavior.
func NamedDecisionsPassThrough(info *RelayInfo) bool {
	return info.ChannelType == baseconstant.ChannelTypeOpenRouter &&
		(model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled)
}

// GetBillingModelName never changes the model used for permissions, routing or
// upstream requests. Decisions is priced by the client's model, even on retries.
func (info *RelayInfo) GetBillingModelName() string {
	if info.RelayMode == constant.RelayModeDecisions {
		if IsNamedDecisionsRequest(info.Request) {
			return info.OriginModelName
		}
		if info.ChannelMeta != nil && baseconstant.SupportsNamedDecisions(info.ChannelType) {
			return info.OriginModelName
		}
		model := info.ClientModelName
		if model == "" {
			model = info.OriginModelName
		}
		return ratio_setting.DecisionsBillingModel(model)
	}
	return info.OriginModelName
}

// PrepareDecisionsRequest is shared by relay and channel tests so overrides and
// privacy settings cannot produce a different protocol in the test path.
func PrepareDecisionsRequest(info *RelayInfo, body []byte) ([]byte, error) {
	if IsNamedDecisionsRequest(info.Request) || (info.ChannelMeta != nil && baseconstant.SupportsNamedDecisions(info.ChannelType)) {
		return PrepareNamedDecisionsRequest(info, body)
	}
	var err error
	body, err = RemoveDisabledFields(body, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		body, err = ApplyParamOverrideWithRelayInfo(body, info)
		if err != nil {
			return nil, err
		}
	}
	var effective dto.OpenAIDecisionsRequest
	if err := basecommon.Unmarshal(body, &effective); err != nil {
		return nil, err
	}
	info.UpstreamModelName = effective.Model
	return body, nil
}

// PrepareNamedDecisionsRequest runs after model mapping, for both relay and
// channel tests. All changes are confined to this attempt's serialized payload.
func PrepareNamedDecisionsRequest(info *RelayInfo, body []byte) ([]byte, error) {
	if !baseconstant.SupportsNamedDecisions(info.ChannelType) {
		return nil, errors.New("named Decisions requires an OpenRouter, TypeSafe or QianwenAI channel")
	}
	if info.ChannelType == baseconstant.ChannelTypeOpenRouter && info.ChannelOtherSettings.IsOpenRouterEnterprise() {
		return nil, errors.New("OpenRouter native Decisions requires a standard channel")
	}
	var err error
	if !NamedDecisionsPassThrough(info) {
		if info.ChannelType == baseconstant.ChannelTypeOpenRouter {
			body, err = RemoveDisabledFields(body, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
			if err != nil {
				return nil, err
			}
		}
		if len(info.ParamOverride) > 0 {
			body, err = ApplyParamOverrideWithRelayInfo(body, info)
			if err != nil {
				return nil, err
			}
		}
	}
	if info.ChannelType == baseconstant.ChannelTypeOpenRouter {
		var effective dto.OpenRouterDecisionsRequest
		if err := basecommon.Unmarshal(body, &effective); err != nil {
			return nil, err
		}
		info.UpstreamModelName = effective.Model
		return body, nil
	}
	var effective dto.TypeSafeRequest
	if err := basecommon.Unmarshal(body, &effective); err != nil {
		return nil, err
	}
	// Qianwen model changes must go through the channel's model mapping.
	if info.ChannelType == baseconstant.ChannelTypeQianwenAI && effective.Model != info.UpstreamModelName {
		return nil, errors.New("System One model overrides must use channel model mapping")
	}
	for _, key := range []string{"provider", "trace", "session_id"} {
		delete(effective.Fields, key)
	}
	return basecommon.Marshal(effective)
}
