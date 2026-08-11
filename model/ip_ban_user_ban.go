package model

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// IPBanUserBan 记录 IP 封禁规则连带封禁的账号
// 用于精确关联 IPBan.Id → User.Id，避免依赖 disable_reason 字符串匹配
type IPBanUserBan struct {
	Id        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	BanId     int    `json:"ban_id" gorm:"index;not null"`
	UserId    int    `json:"user_id" gorm:"index;not null"`
	BannedIP  string `json:"banned_ip" gorm:"type:varchar(64);index"`
	Reason    string `json:"reason" gorm:"type:varchar(255)"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

func (IPBanUserBan) TableName() string {
	return "ip_ban_user_bans"
}

// RecordIPBanUserBan 写入一条关联记录；同一 (ban_id, user_id) 若已存在则更新 banned_ip / reason / created_at
func RecordIPBanUserBan(banId, userId int, bannedIP, reason string) error {
	if banId <= 0 || userId <= 0 {
		return nil
	}
	record := IPBanUserBan{
		BanId:     banId,
		UserId:    userId,
		BannedIP:  bannedIP,
		Reason:    reason,
		CreatedAt: time.Now().Unix(),
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ban_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"banned_ip", "reason", "created_at",
		}),
	}).Create(&record).Error
}

// IPBanRelatedUser 查询结果条目
type IPBanRelatedUser struct {
	UserId        int    `json:"user_id"`
	Username      string `json:"username"`
	DisplayName   string `json:"display_name"`
	Email         string `json:"email"`
	Status        int    `json:"status"`
	DisableReason string `json:"disable_reason"`
	BannedIP      string `json:"banned_ip"`
	BannedAt      int64  `json:"banned_at"`
}

// GetIPBanRelatedUsersByBanId 按 ban_id 查询该规则关联封禁的账号列表
func GetIPBanRelatedUsersByBanId(banId int, limit int) ([]IPBanRelatedUser, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var rows []IPBanRelatedUser
	err := DB.Table("ip_ban_user_bans AS b").
		Select("b.user_id AS user_id, u.username AS username, u.display_name AS display_name, u.email AS email, u.status AS status, u.disable_reason AS disable_reason, b.banned_ip AS banned_ip, b.created_at AS banned_at").
		Joins("LEFT JOIN users u ON u.id = b.user_id").
		Where("b.ban_id = ?", banId).
		Order("b.created_at DESC").
		Limit(limit).
		Scan(&rows).Error
	return rows, err
}

// DeleteIPBanUserBansByBanIds 级联删除关联记录（IP 封禁规则删除时调用）
func DeleteIPBanUserBansByBanIds(banIds []int) error {
	if len(banIds) == 0 {
		return nil
	}
	return DB.Where("ban_id IN ?", banIds).Delete(&IPBanUserBan{}).Error
}

// EnsureIPBanUserBanUniqueIndex 为 (ban_id, user_id) 建立唯一索引；AutoMigrate 后调用
func EnsureIPBanUserBanUniqueIndex(db *gorm.DB) error {
	return db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_ip_ban_user_bans_unique ON ip_ban_user_bans (ban_id, user_id)`).Error
}
