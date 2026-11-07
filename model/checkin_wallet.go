package model

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

var ErrInsufficientWalletQuota = errors.New("wallet quota insufficient")

// WalletCharge records actual sources independently of optional consumption logs.
// Quota is the signed target debit; allocations are always nonnegative.
type WalletCharge struct {
	Id             int    `gorm:"primaryKey;autoIncrement"`
	UserId         int    `gorm:"not null;index"`
	Quota          int    `gorm:"not null;default:0"`
	PermanentQuota int    `gorm:"not null;default:0"`
	Allocations    string `gorm:"type:text"`
	Refunded       bool   `gorm:"not null;default:false"`
	CreatedAt      int64
}

// Collect audit entries inside the transaction and emit them only after commit.
// This includes sign-in reservations refunded after their grant has expired.
type walletExpiryLogsKey struct{}

func withWalletExpiryLogs(tx *gorm.DB, logs *[]Checkin) *gorm.DB {
	return tx.WithContext(context.WithValue(tx.Statement.Context, walletExpiryLogsKey{}, logs))
}
func addWalletExpiryLog(tx *gorm.DB, row Checkin, amount int) {
	if logs, ok := tx.Statement.Context.Value(walletExpiryLogsKey{}).(*[]Checkin); ok && amount > 0 {
		row.ExpiredQuota = amount
		*logs = append(*logs, row)
	}
}

type walletPart struct {
	CheckinId int `json:"checkin_id"`
	Quota     int `json:"quota"`
}

func lockWalletUser(tx *gorm.DB, userID int) (*User, error) {
	var user User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "quota").First(&user, userID).Error
	return &user, err
}

func expireCheckinWalletTx(tx *gorm.DB, user *User, now int64) ([]Checkin, error) {
	var rows []Checkin
	err := tx.Where("user_id = ? AND wallet_tracked = ? AND settled_at = 0 AND expires_at > 0 AND expires_at <= ?", user.Id, true, now).Order("id asc").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		row := &rows[i]
		amount := min(max(0, user.Quota), max(0, row.RemainingQuota))
		if amount > 0 {
			if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota - ?", amount)).Error; err != nil {
				return nil, err
			}
			user.Quota -= amount
		}
		if err := tx.Model(row).Updates(map[string]interface{}{"settled_at": now, "remaining_quota": 0, "expired_quota": gorm.Expr("expired_quota + ?", amount)}).Error; err != nil {
			return nil, err
		}
		row.ExpiredQuota = amount
		addWalletExpiryLog(tx, *row, amount)
	}
	return rows, nil
}

// Every source mutation locks the same user first, then its grants/charge. The
// expiry marker and balance debit commit together, including lazy midnight expiry.
func walletUserTransaction(userID int, fn func(*gorm.DB, *User, int64) error) ([]Checkin, error) {
	var expired, expiryLogs []Checkin
	err := DB.Transaction(func(tx *gorm.DB) error {
		tx = withWalletExpiryLogs(tx, &expiryLogs)
		user, err := lockWalletUser(tx, userID)
		if err != nil {
			return err
		}
		now := common.GetTimestamp()
		expired, err = expireCheckinWalletTx(tx, user, now)
		if err != nil {
			return err
		}
		if fn != nil {
			return fn(tx, user, now)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	_ = InvalidateUserCache(userID)
	recordExpiredCheckinQuota(expiryLogs)
	return expired, nil
}

func recordExpiredCheckinQuota(expired []Checkin) {
	for _, row := range expired {
		if row.ExpiredQuota > 0 {
			RecordLog(row.UserId, LogTypeSystem, fmt.Sprintf("签到额度当日有效：%s 发放 %s，剩余回收 %s", row.CheckinDate, logger.LogQuota(row.QuotaAwarded), logger.LogQuota(row.ExpiredQuota)))
		}
	}
}

func ExpireUserCheckinQuota(userID int) error {
	_, err := walletUserTransaction(userID, nil)
	return err
}
func GetWalletQuota(userID int) (int, error) {
	var quota int
	_, err := walletUserTransaction(userID, func(_ *gorm.DB, u *User, _ int64) error { quota = u.Quota; return nil })
	return quota, err
}

func debitCheckinWalletTx(tx *gorm.DB, user *User, amount int, allowOverdraw bool, now int64) ([]walletPart, int, error) {
	if amount < 0 {
		return nil, 0, errors.New("negative wallet debit")
	}
	if !allowOverdraw && user.Quota < amount {
		return nil, 0, ErrInsufficientWalletQuota
	}
	parts := []walletPart{}
	left := amount
	if amount > 0 {
		var grants []Checkin
		err := tx.Where("user_id = ? AND wallet_tracked = ? AND settled_at = 0 AND remaining_quota > 0 AND expires_at > ?", user.Id, true, now).Order("expires_at asc, id asc").Find(&grants).Error
		if err != nil {
			return nil, 0, err
		}
		for _, grant := range grants {
			n := min(left, grant.RemainingQuota)
			if n <= 0 {
				break
			}
			if err := tx.Model(&Checkin{}).Where("id = ?", grant.Id).Update("remaining_quota", gorm.Expr("remaining_quota - ?", n)).Error; err != nil {
				return nil, 0, err
			}
			parts = append(parts, walletPart{grant.Id, n})
			left -= n
		}
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota - ?", amount)).Error; err != nil {
			return nil, 0, err
		}
		user.Quota -= amount
	}
	return parts, left, nil
}

func DebitWalletQuota(userID, amount int) error {
	_, err := walletUserTransaction(userID, func(tx *gorm.DB, u *User, now int64) error {
		_, _, err := debitCheckinWalletTx(tx, u, amount, true, now)
		return err
	})
	return err
}
func CreditWalletQuota(userID, amount int) error {
	if amount < 0 {
		return errors.New("negative wallet credit")
	}
	_, err := walletUserTransaction(userID, func(tx *gorm.DB, u *User, _ int64) error {
		return tx.Model(&User{}).Where("id = ?", u.Id).Update("quota", gorm.Expr("quota + ?", amount)).Error
	})
	return err
}

// Administrative overrides consume grants on a reduction, preserving permanent
// credits on later expiry. Increases always create permanent quota.
func setWalletQuotaTx(tx *gorm.DB, user *User, target int, now int64) error {
	if target < user.Quota {
		_, _, err := debitCheckinWalletTx(tx, user, user.Quota-target, true, now)
		return err
	}
	delta := target - user.Quota
	err := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + ?", delta)).Error
	if err == nil {
		user.Quota = target
	}
	return err
}
func SetWalletQuota(userID, target int) error {
	_, err := walletUserTransaction(userID, func(tx *gorm.DB, u *User, now int64) error { return setWalletQuotaTx(tx, u, target, now) })
	return err
}

func ReserveWalletCharge(userID, amount int) (*WalletCharge, error) {
	if amount < 0 {
		return nil, errors.New("negative wallet reservation")
	}
	row := &WalletCharge{UserId: userID, Allocations: "[]"}
	_, err := walletUserTransaction(userID, func(tx *gorm.DB, u *User, now int64) error {
		row.CreatedAt = now
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		return setWalletChargeTx(tx, u, row, amount, false, false, now)
	})
	return row, err
}

// Absolute target amounts make retries and asynchronous task redelivery safe.
func SetWalletChargeQuota(userID, chargeID, target int, allowOverdraw bool) error {
	_, err := walletUserTransaction(userID, func(tx *gorm.DB, u *User, now int64) error {
		return changeWalletChargeTx(tx, u, chargeID, target, allowOverdraw, false, now)
	})
	return err
}
func RefundWalletCharge(userID, chargeID int) error {
	_, err := walletUserTransaction(userID, func(tx *gorm.DB, u *User, now int64) error {
		return changeWalletChargeTx(tx, u, chargeID, 0, true, true, now)
	})
	return err
}
func changeWalletChargeTx(tx *gorm.DB, user *User, chargeID, target int, allowOverdraw, refund bool, now int64) error {
	var row WalletCharge
	if err := tx.Where("id = ? AND user_id = ?", chargeID, user.Id).First(&row).Error; err != nil {
		return err
	}
	return setWalletChargeTx(tx, user, &row, target, allowOverdraw, refund, now)
}
func setWalletChargeTx(tx *gorm.DB, user *User, row *WalletCharge, target int, allowOverdraw, refund bool, now int64) error {
	if row.Refunded {
		if refund {
			return nil
		}
		return errors.New("wallet charge already refunded")
	}
	parts := []walletPart{}
	if row.Allocations != "" {
		if err := common.UnmarshalJsonStr(row.Allocations, &parts); err != nil {
			return err
		}
	}
	delta := max(target, 0) - max(row.Quota, 0)
	if delta > 0 {
		added, paid, err := debitCheckinWalletTx(tx, user, delta, allowOverdraw, now)
		if err != nil {
			return err
		}
		row.PermanentQuota += paid
		for _, p := range added {
			found := false
			for i := range parts {
				if parts[i].CheckinId == p.CheckinId {
					parts[i].Quota += p.Quota
					found = true
					break
				}
			}
			if !found {
				parts = append(parts, p)
			}
		}
	} else if delta < 0 {
		left := -delta
		// Refund permanent funds first, retaining sign-in-first allocation of the
		// final bill. Expired sign-in reservations never turn into permanent credit.
		paid := min(left, row.PermanentQuota)
		row.PermanentQuota -= paid
		left -= paid
		credit := paid
		for i := len(parts) - 1; i >= 0 && left > 0; i-- {
			n := min(left, parts[i].Quota)
			if n == 0 {
				continue
			}
			var grant Checkin
			if err := tx.Where("id = ? AND user_id = ?", parts[i].CheckinId, user.Id).First(&grant).Error; err != nil {
				return err
			}
			if grant.WalletTracked && grant.SettledAt == 0 && grant.ExpiresAt > now {
				if err := tx.Model(&grant).Update("remaining_quota", gorm.Expr("remaining_quota + ?", n)).Error; err != nil {
					return err
				}
				credit += n
			} else {
				if err := tx.Model(&grant).Update("expired_quota", gorm.Expr("expired_quota + ?", n)).Error; err != nil {
					return err
				}
				addWalletExpiryLog(tx, grant, n)
			}
			parts[i].Quota -= n
			left -= n
		}
		if left != 0 {
			return errors.New("wallet charge allocation mismatch")
		}
		if credit > 0 {
			if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + ?", credit)).Error; err != nil {
				return err
			}
			user.Quota += credit
		}
	}
	// Signed negative prices remain permanent credits, applied once per target.
	creditDelta := max(-target, 0) - max(-row.Quota, 0)
	if creditDelta != 0 {
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + ?", creditDelta)).Error; err != nil {
			return err
		}
		user.Quota += creditDelta
	}
	encoded, err := common.Marshal(parts)
	if err != nil {
		return err
	}
	row.Quota = target
	row.Allocations = string(encoded)
	row.Refunded = refund
	return tx.Model(row).Select("quota", "permanent_quota", "allocations", "refunded").Updates(row).Error
}

func checkinExpiresAt(now time.Time) int64 {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1).Unix()
}
