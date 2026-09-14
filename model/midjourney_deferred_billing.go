package model

import (
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Legacy MJ records have no private_data column. Keep trusted billing metadata
// in their existing JSON properties storage, stripping it before public use.
const mjBillingProperty = "_new_api_deferred_billing_v1"

func (m *Midjourney) storageCopy() (*Midjourney, error) {
	copy := *m
	properties := map[string]json.RawMessage{}
	if m.Properties != "" {
		if err := common.UnmarshalJsonStr(m.Properties, &properties); err != nil {
			if m.DeferredBilling == nil {
				return &copy, nil
			}
			return nil, err
		}
	}
	if properties == nil {
		properties = map[string]json.RawMessage{}
	}
	if _, exists := properties[mjBillingProperty]; !exists && m.DeferredBilling == nil {
		return &copy, nil
	}
	// Never accept a billing marker supplied through upstream properties.
	delete(properties, mjBillingProperty)
	if m.DeferredBilling != nil {
		metadata, err := common.Marshal(m.DeferredBilling)
		if err != nil {
			return nil, err
		}
		properties[mjBillingProperty] = metadata
	}
	if len(properties) == 0 && m.Properties == "" {
		return &copy, nil
	}
	data, err := common.Marshal(properties)
	copy.Properties = string(data)
	return &copy, err
}

func (m *Midjourney) AfterFind(_ *gorm.DB) error {
	if m.Properties == "" {
		return nil
	}
	var properties map[string]json.RawMessage
	if err := common.UnmarshalJsonStr(m.Properties, &properties); err != nil {
		return nil
	}
	metadata, ok := properties[mjBillingProperty]
	if !ok {
		return nil
	}
	if err := common.Unmarshal(metadata, &m.DeferredBilling); err != nil {
		return err
	}
	delete(properties, mjBillingProperty)
	data, err := common.Marshal(properties)
	m.Properties = string(data)
	return err
}

func (m *Midjourney) HasDeferredBilling() bool {
	return m.DeferredBilling != nil && m.DeferredBilling.BillingContext != nil && m.DeferredBilling.BillingContext.DeferredSettlement
}

func CompleteDeferredMidjourney(m *Midjourney, fromStatus string) (bool, error) {
	if !m.HasDeferredBilling() || fromStatus == TaskStatusSuccess || fromStatus == TaskStatusFailure ||
		(m.Status != TaskStatusSuccess && m.Status != TaskStatusFailure) {
		return false, errors.New("invalid deferred midjourney transition")
	}
	actual := 0
	if m.Status == TaskStatusSuccess {
		actual = m.DeferredBilling.BillingContext.EstimatedQuota
	}
	stored, err := m.storageCopy()
	if err != nil {
		return false, err
	}
	stored.Quota = actual
	delta := actual - m.Quota
	accounting := &Task{UserId: m.UserId, ChannelId: m.ChannelId, Status: TaskStatus(m.Status), PrivateData: *m.DeferredBilling}
	won := false
	var tokenKey string
	err = DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(stored).Where("status = ?", fromStatus).Select("*").Updates(stored)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		won = true
		var err error
		tokenKey, err = applyDeferredTaskFunding(tx, accounting, actual, delta)
		return err
	})
	if err != nil {
		return false, err
	}
	if !won {
		return false, nil
	}
	m.Quota = actual
	syncDeferredTaskFundingCache(accounting, delta, tokenKey)
	return true, nil
}
