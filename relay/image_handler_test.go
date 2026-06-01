package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

func TestShouldPassThroughImageRequestSkipsAgnesAI(t *testing.T) {
	original := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	defer func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = original
	}()

	model_setting.GetGlobalSettings().PassThroughRequestEnabled = true
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeAgnesAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}

	if shouldPassThroughImageRequest(info) {
		t.Fatal("expected Agnes AI image requests to be converted even when pass-through is enabled")
	}
}

func TestShouldPassThroughImageRequestHonorsNonAgnesChannels(t *testing.T) {
	original := model_setting.GetGlobalSettings().PassThroughRequestEnabled
	defer func() {
		model_setting.GetGlobalSettings().PassThroughRequestEnabled = original
	}()

	model_setting.GetGlobalSettings().PassThroughRequestEnabled = false
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeOpenAI,
			ChannelSetting: dto.ChannelSettings{
				PassThroughBodyEnabled: true,
			},
		},
	}

	if !shouldPassThroughImageRequest(info) {
		t.Fatal("expected non-Agnes image requests to honor channel pass-through")
	}
}
