package model

import "strings"

// GetMissingModels returns model names that are referenced in the system
func GetMissingModels() ([]string, error) {
	// 1. 获取所有已启用模型（去重）
	models := GetEnabledModels()
	if len(models) == 0 {
		return []string{}, nil
	}

	// 2. 查询已有的模型元数据规则。GORM 会自动排除软删除记录。
	var configured []struct {
		ModelName string
		NameRule  int
	}
	if err := DB.Model(&Model{}).Select("model_name", "name_rule").Find(&configured).Error; err != nil {
		return nil, err
	}

	exactSet := make(map[string]struct{})
	prefixRules := make([]string, 0)
	suffixRules := make([]string, 0)
	containsRules := make([]string, 0)
	for _, item := range configured {
		if item.ModelName == "" {
			continue
		}
		switch item.NameRule {
		case NameRuleExact:
			exactSet[item.ModelName] = struct{}{}
		case NameRulePrefix:
			prefixRules = append(prefixRules, item.ModelName)
		case NameRuleSuffix:
			suffixRules = append(suffixRules, item.ModelName)
		case NameRuleContains:
			containsRules = append(containsRules, item.ModelName)
		default:
			exactSet[item.ModelName] = struct{}{}
		}
	}

	// 3. 收集缺失模型
	var missing []string
	for _, name := range models {
		if !isModelNameConfigured(name, exactSet, prefixRules, suffixRules, containsRules) {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

func isModelNameConfigured(name string, exactSet map[string]struct{}, prefixRules, suffixRules, containsRules []string) bool {
	if _, ok := exactSet[name]; ok {
		return true
	}
	for _, rule := range prefixRules {
		if strings.HasPrefix(name, rule) {
			return true
		}
	}
	for _, rule := range suffixRules {
		if strings.HasSuffix(name, rule) {
			return true
		}
	}
	for _, rule := range containsRules {
		if strings.Contains(name, rule) {
			return true
		}
	}
	return false
}
