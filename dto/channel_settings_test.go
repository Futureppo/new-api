package dto_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestChannelSettingsRPMProtectionBackwardCompatibility(t *testing.T) {
	var settings dto.ChannelSettings
	require.NoError(t, common.Unmarshal([]byte(`{"force_format":true}`), &settings))
	require.True(t, settings.ForceFormat)
	require.Nil(t, settings.RPMProtection)
	require.NoError(t, settings.Validate())
}

func TestChannelSettingsRPMProtectionJSONRoundTrip(t *testing.T) {
	want := dto.ChannelSettings{
		RPMProtection: &dto.ChannelRPMProtectionSettings{
			Enabled:                    true,
			RPMLimit:                   1000,
			ProtectionThresholdPercent: 60,
			RampMinutes:                5,
		},
	}

	data, err := common.Marshal(want)
	require.NoError(t, err)

	var got dto.ChannelSettings
	require.NoError(t, common.Unmarshal(data, &got))
	require.Equal(t, want, got)
	require.NoError(t, got.Validate())
}

func TestChannelRPMProtectionSettingsValidation(t *testing.T) {
	tests := []struct {
		name    string
		setting *dto.ChannelRPMProtectionSettings
		wantErr bool
	}{
		{name: "nil", setting: nil},
		{name: "zero RPM disables limit", setting: &dto.ChannelRPMProtectionSettings{RPMLimit: 0, ProtectionThresholdPercent: 1, RampMinutes: 1}},
		{name: "upper threshold boundary", setting: &dto.ChannelRPMProtectionSettings{RPMLimit: 1, ProtectionThresholdPercent: 100, RampMinutes: 1}},
		{name: "negative RPM", setting: &dto.ChannelRPMProtectionSettings{RPMLimit: -1, ProtectionThresholdPercent: 60, RampMinutes: 5}, wantErr: true},
		{name: "zero threshold", setting: &dto.ChannelRPMProtectionSettings{RPMLimit: 1, ProtectionThresholdPercent: 0, RampMinutes: 5}, wantErr: true},
		{name: "threshold above 100", setting: &dto.ChannelRPMProtectionSettings{RPMLimit: 1, ProtectionThresholdPercent: 101, RampMinutes: 5}, wantErr: true},
		{name: "zero ramp", setting: &dto.ChannelRPMProtectionSettings{RPMLimit: 1, ProtectionThresholdPercent: 60, RampMinutes: 0}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.setting.Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestChannelOtherSettingsRemoveGifImagesDefaultsOn(t *testing.T) {
	var settings dto.ChannelOtherSettings
	require.NoError(t, common.Unmarshal([]byte(`{}`), &settings))
	require.Nil(t, settings.RemoveGifImagesEnabled)
	require.True(t, settings.ShouldRemoveGifImages())
}

func TestChannelOtherSettingsRemoveGifImagesExplicitValueRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "enabled", payload: `{"remove_gif_images_enabled":true}`, want: true},
		{name: "disabled", payload: `{"remove_gif_images_enabled":false}`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var settings dto.ChannelOtherSettings
			require.NoError(t, common.Unmarshal([]byte(tt.payload), &settings))
			require.NotNil(t, settings.RemoveGifImagesEnabled)
			require.Equal(t, tt.want, settings.ShouldRemoveGifImages())

			data, err := common.Marshal(settings)
			require.NoError(t, err)
			require.JSONEq(t, tt.payload, string(data))
		})
	}
}
