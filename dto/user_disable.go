package dto

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// UserDisableDurationMinutes defaults to zero when omitted, but rejects an
// explicit null instead of silently turning an invalid input into a permanent ban.
type UserDisableDurationMinutes int64

func (minutes *UserDisableDurationMinutes) UnmarshalJSON(data []byte) error {
	var value *int64
	if err := common.Unmarshal(data, &value); err != nil {
		return err
	}
	if value == nil || *value < 0 {
		return errors.New("duration_minutes must be a non-negative integer")
	}
	*minutes = UserDisableDurationMinutes(*value)
	return nil
}
