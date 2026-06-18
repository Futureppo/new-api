package model

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	IPBanTypePermanent = "permanent"
	IPBanTypeTemporary = "temporary"
)

type IPBan struct {
	Id          int            `json:"id"`
	Target      string         `json:"target" gorm:"type:varchar(128);index"`
	Reason      string         `json:"reason" gorm:"type:varchar(255);not null"`
	ExpiresAt   int64          `json:"expires_at" gorm:"bigint;index;default:0"`
	AutoBanUser bool           `json:"auto_ban_user" gorm:"default:false"`
	CreatedAt   int64          `json:"created_at" gorm:"bigint;index"`
	UpdatedAt   int64          `json:"updated_at" gorm:"bigint"`
	CreatedBy   int            `json:"created_by" gorm:"index"`
	DeletedAt   gorm.DeletedAt `gorm:"index"`
}

type ipBanCacheEntry struct {
	Id          int
	Target      string
	Reason      string
	ExpiresAt   int64
	AutoBanUser bool
	Addr        netip.Addr
	Prefix      netip.Prefix
	IsPrefix    bool
}

var (
	ipBanCache     []ipBanCacheEntry
	ipBanCacheLock sync.RWMutex
)

func NormalizeIPBanTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("IP或IP段不能为空")
	}

	if strings.Contains(target, "/") {
		prefix, err := netip.ParsePrefix(target)
		if err != nil {
			return "", fmt.Errorf("无效的IP段: %s", target)
		}
		return prefix.Masked().String(), nil
	}

	addr, err := netip.ParseAddr(target)
	if err != nil {
		return "", fmt.Errorf("无效的IP: %s", target)
	}
	return addr.Unmap().String(), nil
}

func parseIPBanCacheEntry(ban *IPBan) (ipBanCacheEntry, error) {
	entry := ipBanCacheEntry{
		Id:          ban.Id,
		Target:      ban.Target,
		Reason:      ban.Reason,
		ExpiresAt:   ban.ExpiresAt,
		AutoBanUser: ban.AutoBanUser,
	}
	if strings.Contains(ban.Target, "/") {
		prefix, err := netip.ParsePrefix(ban.Target)
		if err != nil {
			return entry, err
		}
		entry.Prefix = prefix.Masked()
		entry.IsPrefix = true
		return entry, nil
	}

	addr, err := netip.ParseAddr(ban.Target)
	if err != nil {
		return entry, err
	}
	entry.Addr = addr.Unmap()
	return entry, nil
}

func InitIPBanCache() {
	if DB == nil {
		return
	}

	now := common.GetTimestamp()
	var bans []*IPBan
	if err := DB.Where("expires_at = ? OR expires_at > ?", 0, now).Find(&bans).Error; err != nil {
		common.SysLog("failed to sync ip bans from database: " + err.Error())
		return
	}

	entries := make([]ipBanCacheEntry, 0, len(bans))
	for _, ban := range bans {
		entry, err := parseIPBanCacheEntry(ban)
		if err != nil {
			common.SysLog(fmt.Sprintf("skipping invalid ip ban #%d target=%q: %s", ban.Id, ban.Target, err.Error()))
			continue
		}
		entries = append(entries, entry)
	}

	ipBanCacheLock.Lock()
	ipBanCache = entries
	ipBanCacheLock.Unlock()
}

func SyncIPBanCache(frequency int) {
	if frequency <= 0 {
		frequency = 60
	}
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		InitIPBanCache()
	}
}

func MatchIPBan(clientIP string) (*IPBan, bool) {
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return nil, false
	}
	addr = addr.Unmap()

	now := common.GetTimestamp()
	ipBanCacheLock.RLock()
	defer ipBanCacheLock.RUnlock()

	for _, entry := range ipBanCache {
		if entry.ExpiresAt != 0 && entry.ExpiresAt <= now {
			continue
		}
		if entry.IsPrefix {
			if entry.Prefix.Contains(addr) {
				return &IPBan{
					Id:          entry.Id,
					Target:      entry.Target,
					Reason:      entry.Reason,
					ExpiresAt:   entry.ExpiresAt,
					AutoBanUser: entry.AutoBanUser,
				}, true
			}
			continue
		}
		if entry.Addr == addr {
			return &IPBan{
				Id:          entry.Id,
				Target:      entry.Target,
				Reason:      entry.Reason,
				ExpiresAt:   entry.ExpiresAt,
				AutoBanUser: entry.AutoBanUser,
			}, true
		}
	}
	return nil, false
}

func IsIPBanTargetMatchClient(target string, clientIP string) (bool, error) {
	normalized, err := NormalizeIPBanTarget(target)
	if err != nil {
		return false, err
	}
	entry, err := parseIPBanCacheEntry(&IPBan{Target: normalized})
	if err != nil {
		return false, err
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return false, fmt.Errorf("无法解析当前IP: %s", clientIP)
	}
	addr = addr.Unmap()
	if entry.IsPrefix {
		return entry.Prefix.Contains(addr), nil
	}
	return entry.Addr == addr, nil
}

func applyIPBanTypeFilter(tx *gorm.DB, banType string) *gorm.DB {
	switch banType {
	case IPBanTypePermanent:
		return tx.Where("expires_at = ?", 0)
	case IPBanTypeTemporary:
		return tx.Where("expires_at != ?", 0)
	default:
		return tx
	}
}

func GetAllIPBans(banType string, startIdx int, num int) (bans []*IPBan, total int64, err error) {
	tx := applyIPBanTypeFilter(DB.Model(&IPBan{}), banType)
	if err = tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = applyIPBanTypeFilter(DB.Model(&IPBan{}), banType).Order("id desc").Limit(num).Offset(startIdx).Find(&bans).Error
	return bans, total, err
}

func SearchIPBans(banType string, keyword string, startIdx int, num int) (bans []*IPBan, total int64, err error) {
	keyword = strings.TrimSpace(keyword)
	tx := applyIPBanTypeFilter(DB.Model(&IPBan{}), banType)
	if keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where("target LIKE ? OR reason LIKE ?", like, like)
	}
	if err = tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	tx = applyIPBanTypeFilter(DB.Model(&IPBan{}), banType)
	if keyword != "" {
		like := "%" + keyword + "%"
		tx = tx.Where("target LIKE ? OR reason LIKE ?", like, like)
	}
	err = tx.Order("id desc").Limit(num).Offset(startIdx).Find(&bans).Error
	return bans, total, err
}

func GetIPBanById(id int) (*IPBan, error) {
	if id == 0 {
		return nil, errors.New("id为空")
	}
	ban := IPBan{Id: id}
	err := DB.First(&ban, "id = ?", id).Error
	return &ban, err
}

func GetIPBanByTarget(target string) (*IPBan, error) {
	ban := IPBan{}
	err := DB.Where("target = ?", target).First(&ban).Error
	return &ban, err
}

func CreateIPBan(ban *IPBan) error {
	now := common.GetTimestamp()
	ban.CreatedAt = now
	ban.UpdatedAt = now
	return DB.Create(ban).Error
}

func UpdateIPBan(ban *IPBan) error {
	ban.UpdatedAt = common.GetTimestamp()
	return DB.Model(ban).Select("target", "reason", "expires_at", "auto_ban_user", "updated_at").Updates(ban).Error
}

func DeleteIPBanById(id int) error {
	if id == 0 {
		return errors.New("id为空")
	}
	return DB.Delete(&IPBan{Id: id}).Error
}
