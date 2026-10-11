package common

import (
	basecommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// GetBillingModelName never changes the model used for permissions, routing or
// upstream requests. Decisions is priced by the client's model, even on retries.
func (info *RelayInfo) GetBillingModelName() string {
	if info.RelayMode == constant.RelayModeDecisions {
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
