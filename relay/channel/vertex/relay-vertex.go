package vertex

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var vertexRegionPattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validateVertexRegion(region string) (string, error) {
	region = strings.TrimSpace(region)
	if region == "" {
		return "", errors.New("Vertex region cannot be empty")
	}
	if region == "global" {
		return region, nil
	}
	if !vertexRegionPattern.MatchString(region) {
		return "", fmt.Errorf("invalid Vertex region %q", region)
	}
	return region, nil
}

func parseVertexRegionMap(other string) (map[string]any, error) {
	var regionMap map[string]any
	if err := common.UnmarshalJsonStr(strings.TrimSpace(other), &regionMap); err != nil {
		return nil, err
	}
	if regionMap == nil {
		return nil, errors.New("Vertex region mapping cannot be null")
	}
	return regionMap, nil
}

func ValidateRegionConfig(other string) error {
	if strings.TrimSpace(other) == "" {
		return errors.New("部署地区不能为空")
	}
	regionMap, err := parseVertexRegionMap(other)
	if err != nil {
		return errors.New(`部署地区必须是标准的Json格式，例如{"default": "us-central1", "region2": "us-east1"}`)
	}
	if _, ok := regionMap["default"]; !ok {
		return errors.New("部署地区必须包含default字段")
	}
	for modelName, rawRegion := range regionMap {
		region, ok := rawRegion.(string)
		if !ok {
			return fmt.Errorf("部署地区 %s 必须是字符串", modelName)
		}
		if _, err := validateVertexRegion(region); err != nil {
			return fmt.Errorf("部署地区 %s 无效: %w", modelName, err)
		}
	}
	return nil
}

func ResolveModelRegion(other string, localModelName string) (string, error) {
	other = strings.TrimSpace(other)
	if other == "" {
		return "global", nil
	}
	if !common.IsJsonObject(other) {
		return validateVertexRegion(other)
	}
	regionMap, err := parseVertexRegionMap(other)
	if err != nil {
		return "", fmt.Errorf("invalid Vertex region mapping: %w", err)
	}
	rawRegion, ok := regionMap[localModelName]
	if !ok {
		rawRegion, ok = regionMap["default"]
	}
	if !ok {
		return "global", nil
	}
	region, ok := rawRegion.(string)
	if !ok {
		return "", fmt.Errorf("Vertex region for %q must be a string", localModelName)
	}
	return validateVertexRegion(region)
}

// GetModelRegion is kept for compatibility with callers that only need a safe
// fallback. Request-building code should use ResolveModelRegion and surface errors.
func GetModelRegion(other string, localModelName string) string {
	region, err := ResolveModelRegion(other, localModelName)
	if err != nil {
		return "global"
	}
	return region
}
