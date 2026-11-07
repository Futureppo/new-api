package service

import (
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	checkinBehaviorMaxScore = 45
	// 足够历史且多个信号相互印证时才降低奖励。
	behaviorMinSamples = 7
	behaviorSampleSize = 14

	behaviorUsageLookbackDays = 7
)

// secondsIntoLocalDay 取时间戳在本地时区当天已过去的秒数。
func secondsIntoLocalDay(ts int64) float64 {
	t := time.Unix(ts, 0).In(time.Local)
	return float64(t.Hour()*3600 + t.Minute()*60 + t.Second())
}

// circularStdDevSeconds 计算一组「当天第几秒」的圆周标准差。
//
// 必须用圆周统计而非普通方差：23:59 与 00:01 实际相差 2 分钟，
// 按线性方差会被算成相差近 24 小时，正好把最像脚本的跨零点样本判成最像人类。
//
// 返回值越小表示签到时刻越集中；完全固定的定时任务趋近于 0。
func circularStdDevSeconds(secondsOfDay []float64) float64 {
	n := float64(len(secondsOfDay))
	if n == 0 {
		return math.Inf(1)
	}
	var sumSin, sumCos float64
	for _, s := range secondsOfDay {
		angle := 2 * math.Pi * s / 86400.0
		sumSin += math.Sin(angle)
		sumCos += math.Cos(angle)
	}
	r := math.Hypot(sumSin/n, sumCos/n)
	if r <= 0 {
		return math.Inf(1) // 完全均匀分布
	}
	// 浮点误差会让完全重合的样本算出 r 略小于 1（如 0.9999999999999999），
	// 若不归零就会得到一个虚假的亚秒级离散度。留一点容差。
	if r >= 1-1e-12 {
		return 0 // 完全重合
	}
	stdRadians := math.Sqrt(-2 * math.Log(r))
	return stdRadians * 86400.0 / (2 * math.Pi)
}

// gradedScore 把一个观测值线性映射到 [0, weight]。
// value <= scriptAt 得 0 分，value >= humanAt 得满分，中间线性插值。
// 全程无阈值跳变，避免攻击者用二分法定位判定点。
func gradedScore(value, scriptAt, humanAt float64, weight int) int {
	if math.IsInf(value, 1) || value >= humanAt {
		return weight
	}
	if value <= scriptAt {
		return 0
	}
	ratio := (value - scriptAt) / (humanAt - scriptAt)
	return int(math.Round(ratio * float64(weight)))
}

// checkinBehaviorScore 根据用户的历史签到行为打分，0-checkinBehaviorMaxScore。
//
// 与请求头信号互补：请求头能识别裸 HTTP 客户端，但对无头浏览器无效；
// 行为特征则关注「这是不是一个由定时任务驱动、且从不真正使用服务的账号」，
// 无头浏览器只要挂在 cron 上就会在这里失分。
//
// 历史不足时一律给满分，避免误伤新用户。
func checkinBehaviorScore(userId int, now time.Time) int {
	if !common.LogConsumeEnabled {
		return checkinBehaviorMaxScore
	}
	timestamps, err := model.RecentCheckinTimestamps(userId, behaviorSampleSize)
	if err != nil {
		return checkinBehaviorMaxScore
	}
	cutoff := now.AddDate(0, 0, -behaviorSampleSize).Unix()
	recent := timestamps[:0]
	for _, ts := range timestamps {
		if ts >= cutoff && ts <= now.Unix() {
			recent = append(recent, ts)
		}
	}
	timestamps = recent
	if len(timestamps) < behaviorMinSamples {
		return checkinBehaviorMaxScore
	}
	usage, err := model.CheckinUsageSince(userId, now.Add(-behaviorUsageLookbackDays*24*time.Hour).Unix())
	if err != nil {
		return checkinBehaviorMaxScore
	}
	// A real user's regular routine is not evidence of automation. A single token
	// call or a zero-cost log cannot exempt an account from recurring timing evidence.
	if usage.Days >= 2 && usage.Calls >= 3 {
		return checkinBehaviorMaxScore
	}
	seconds := make([]float64, 0, len(timestamps))
	for _, ts := range timestamps {
		seconds = append(seconds, secondsIntoLocalDay(ts))
	}
	spread := circularStdDevSeconds(seconds)
	// Minute-level routines are common; require second-level repetition for a penalty.
	if spread >= 60 {
		return checkinBehaviorMaxScore
	}
	penalty := checkinBehaviorMaxScore - gradedScore(spread, 2, 60, checkinBehaviorMaxScore)
	return checkinBehaviorMaxScore - penalty
}
