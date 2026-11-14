package dto

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

type ChannelKeywordRedirectSettings struct {
	Enabled bool              `json:"enabled"`
	Rules   map[string]string `json:"rules"`
}

// Decode values explicitly: encoding/json otherwise accepts null as an empty
// string, which would unintentionally turn an invalid rule into a deletion.
func (s *ChannelKeywordRedirectSettings) UnmarshalJSON(data []byte) error {
	var raw struct {
		Enabled bool                       `json:"enabled"`
		Rules   map[string]json.RawMessage `json:"rules"`
	}
	if err := common.Unmarshal(data, &raw); err != nil {
		return err
	}
	rules := make(map[string]string, len(raw.Rules))
	for keyword, value := range raw.Rules {
		value = bytes.TrimSpace(value)
		if len(value) == 0 || value[0] != '"' {
			return fmt.Errorf("关键词重定向的替换内容必须是字符串")
		}
		var replacement string
		if err := common.Unmarshal(value, &replacement); err != nil {
			return err
		}
		rules[keyword] = replacement
	}
	s.Enabled, s.Rules = raw.Enabled, rules
	return s.Validate()
}

func (s *ChannelKeywordRedirectSettings) Validate() error {
	if s == nil {
		return nil
	}
	if _, exists := s.Rules[""]; exists {
		return fmt.Errorf("关键词重定向的关键词不能为空")
	}
	return nil
}

func (s *ChannelKeywordRedirectSettings) Active() bool {
	return s != nil && s.Enabled && len(s.Rules) != 0
}
