package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func recordSessionIdentity(c *gin.Context, log *Log) {
	if c == nil || !setting.IsSessionRiskRecordingEnabled() {
		return
	}
	value, ok := c.Get(common.RequestSessionIdentityKey)
	identity, valid := value.(common.RequestSessionIdentity)
	if !ok || !valid {
		return
	}
	log.SessionRecorded = true
	log.SessionId, log.SessionSource = identity.SessionID, identity.SessionSource
	log.CacheKey, log.CacheSource = identity.CacheKey, identity.CacheSource
	log.SessionHash = common.SessionIdentifierHash(identity.SessionID)
	log.CacheHash = common.SessionIdentifierHash(identity.CacheKey)
}

type LogSessionFilter struct {
	SessionID string
	CacheKey  string
}

func applyLogSessionFilters(tx *gorm.DB, filters []LogSessionFilter) *gorm.DB {
	for _, filter := range filters {
		for column, value := range map[string]string{"session_hash": filter.SessionID, "cache_hash": filter.CacheKey} {
			if value == "" {
				continue
			}
			value = common.NormalizeSessionIdentifier(value)
			if value == "" {
				tx = tx.Where("1 = 0")
			} else {
				tx = tx.Where("logs."+column+" = ?", common.SessionIdentifierHash(value))
			}
		}
	}
	return tx
}
