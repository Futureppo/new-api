package ratio_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// Saved option maps replace the in-memory defaults. Fill only absent Agnes/TypeSafe/MiMo
// entries, including when an older installation reloads its saved options.
// An explicitly configured zero is a price and must never be overwritten.
func loadWithAgnesDefaults(target *types.RWMap[string, float64], jsonStr string, defaults map[string]float64) error {
	var values map[string]float64
	if err := common.UnmarshalJsonStr(jsonStr, &values); err != nil {
		return err
	}
	if values == nil {
		values = make(map[string]float64)
	}
	for name, value := range defaults {
		if strings.HasPrefix(name, "agnes-") || strings.HasPrefix(name, "mimo-") || name == "jev-latest" || name == "jev-preview" || name == "jev-1.13.0" {
			if _, exists := values[name]; !exists {
				values[name] = value
			}
		}
	}
	data, err := common.Marshal(values)
	if err != nil {
		return err
	}
	return types.LoadFromJsonStringWithCallback(target, string(data), InvalidateExposedDataCache)
}
