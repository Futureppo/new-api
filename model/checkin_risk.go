package model

import "time"

type CheckinRiskEvidence struct{ Network, Client, Device, Detail string }
type CheckinClusterStats struct{ DeviceAccounts, ClientAccounts, ClientMinuteAccounts int }
type CheckinUsage struct{ Calls, Days int }

// Select only small billing fields, bound the sample, and do not accept free calls
// as evidence that an account genuinely uses its awarded quota.
func CheckinUsageSince(userID int, since int64) (CheckinUsage, error) {
	var rows []struct {
		CreatedAt int64
		Quota     int
	}
	err := LOG_DB.Model(&Log{}).Select("created_at, quota").Where("user_id = ? AND type = ? AND quota > 0 AND created_at >= ?", userID, LogTypeConsume, since).Order("id desc").Limit(200).Find(&rows).Error
	result := CheckinUsage{Calls: len(rows)}
	days := map[string]bool{}
	for _, row := range rows {
		days[time.Unix(row.CreatedAt, 0).In(time.Local).Format("2006-01-02")] = true
	}
	result.Days = len(days)
	if err == nil && result.Calls >= 3 && result.Days == 1 {
		// A busy user's newest 200 calls can all fall on one day. Look for one
		// earlier paid call so a bounded sample does not hide multi-day usage.
		latest := time.Unix(rows[0].CreatedAt, 0).In(time.Local)
		dayStart := time.Date(latest.Year(), latest.Month(), latest.Day(), 0, 0, 0, 0, latest.Location()).Unix()
		var earlier []int
		err = LOG_DB.Model(&Log{}).Where("user_id = ? AND type = ? AND quota > 0 AND created_at >= ? AND created_at < ?", userID, LogTypeConsume, since, dayStart).Order("id desc").Limit(1).Pluck("id", &earlier).Error
		if len(earlier) > 0 {
			result.Days = 2
		}
	}
	return result, err
}

func GetCheckinClusterStats(userID int, network, client, device string, now int64) (CheckinClusterStats, error) {
	var result CheckinClusterStats
	count := func(since int64, conditions string, args ...interface{}) (int, error) {
		var n int64
		err := DB.Model(&Checkin{}).Where("created_at >= ? AND created_at <= ? AND user_id <> ?", since, now, userID).Where(conditions, args...).Distinct("user_id").Count(&n).Error
		return int(n), err
	}
	var err error
	if device != "" {
		result.DeviceAccounts, err = count(now-86400, "risk_device = ?", device)
		if err != nil {
			return result, err
		}
	}
	if network != "" && client != "" {
		result.ClientAccounts, err = count(now-300, "risk_network = ? AND risk_client = ?", network, client)
		if err != nil {
			return result, err
		}
		result.ClientMinuteAccounts, err = count(now-60, "risk_network = ? AND risk_client = ?", network, client)
	}
	return result, err
}
