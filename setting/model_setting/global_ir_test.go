package model_setting

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
)

func TestIRConversionEnabledDefaultAndDynamicToggle(t *testing.T) {
	t.Cleanup(func() {
		SetIRConversionEnabled(true)
	})

	if !IsIRConversionEnabled() {
		t.Fatalf("expected IsIRConversionEnabled() to default to true, got false")
	}

	exported := config.GlobalConfig.ExportAllConfigs()
	if got, ok := exported["global.ir_conversion_enabled"]; !ok || got != "true" {
		t.Fatalf("expected exported config global.ir_conversion_enabled to be %q (present=true), got %q (present=%v)", "true", got, ok)
	}

	if err := config.UpdateConfigFromMap(GetGlobalSettings(), map[string]string{
		"ir_conversion_enabled": "false",
	}); err != nil {
		t.Fatalf("failed to disable ir_conversion_enabled via UpdateConfigFromMap: %v", err)
	}
	if IsIRConversionEnabled() {
		t.Fatalf("expected IsIRConversionEnabled() to be false after toggling off")
	}
	if got := config.GlobalConfig.ExportAllConfigs()["global.ir_conversion_enabled"]; got != "false" {
		t.Fatalf("expected exported global.ir_conversion_enabled to be %q after toggling off, got %q", "false", got)
	}

	if err := config.UpdateConfigFromMap(GetGlobalSettings(), map[string]string{
		"ir_conversion_enabled": "true",
	}); err != nil {
		t.Fatalf("failed to re-enable ir_conversion_enabled via UpdateConfigFromMap: %v", err)
	}
	if !IsIRConversionEnabled() {
		t.Fatalf("expected IsIRConversionEnabled() to be true after toggling back on")
	}
	if got := config.GlobalConfig.ExportAllConfigs()["global.ir_conversion_enabled"]; got != "true" {
		t.Fatalf("expected exported global.ir_conversion_enabled to be %q after toggling back on, got %q", "true", got)
	}
}

func TestIRConversionEnabledConcurrentAccess(t *testing.T) {
	t.Cleanup(func() {
		SetIRConversionEnabled(true)
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if (id+j)%2 == 0 {
					_ = config.UpdateConfigFromMap(GetGlobalSettings(), map[string]string{
						"ir_conversion_enabled": "false",
					})
				} else {
					SetIRConversionEnabled(true)
				}
				_ = IsIRConversionEnabled()
				_ = config.GlobalConfig.ExportAllConfigs()
				_ = ShouldPreserveThinkingSuffix("kimi-k2-thinking")
			}
		}(i)
	}
	wg.Wait()
}
