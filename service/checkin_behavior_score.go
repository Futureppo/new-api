package service

import (
	"math"
	"time"

	"github.com/QuantumNous/new-api/model"
)

const (
	// 行为特征各项权重，合计 checkinBehaviorMaxScore。
	behaviorWeightTimingSpread = 25 // 签到时刻的离散度
	behaviorWeightMidnightRush = 10 // 是否紧贴零点触发
	behaviorWeightRealUsage    = 10 // 是否真的在用这个站

	checkinBehaviorMaxScore = behaviorWeightTimingSpread +
		behaviorWeightMidnightRush + behaviorWeightRealUsage

	// 少于这个次数就没有足够的历史可供分析，一律按满分处理，
	// 避免误伤新注册用户。
	behaviorMinSamples = 3
	behaviorSampleSize = 14

	// 圆周标准差低于该值视为定时任务，高于上限视为正常人类作息。
	behaviorTimingScriptSeconds = 300.0  // 5 分钟
	behaviorTimingHumanSeconds  = 3600.0 // 1 小时

	// 距离本地零点多久之内算「掐点触发」。
	behaviorMidnightScriptSeconds = 60.0
	behaviorMidnightHumanSeconds  = 600.0

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
	timestamps, err := model.RecentCheckinTimestamps(userId, behaviorSampleSize)
	if err != nil {
		return checkinBehaviorMaxScore // 查询失败时不惩罚用户
	}

	score := 0

	// 1. 签到时刻离散度
	if len(timestamps) < behaviorMinSamples {
		score += behaviorWeightTimingSpread
	} else {
		secondsOfDay := make([]float64, 0, len(timestamps))
		for _, ts := range timestamps {
			secondsOfDay = append(secondsOfDay, secondsIntoLocalDay(ts))
		}
		score += gradedScore(circularStdDevSeconds(secondsOfDay),
			behaviorTimingScriptSeconds, behaviorTimingHumanSeconds,
			behaviorWeightTimingSpread)
	}

	// 2. 本次是否掐着零点触发
	score += gradedScore(secondsIntoLocalDay(now.Unix()),
		behaviorMidnightScriptSeconds, behaviorMidnightHumanSeconds,
		behaviorWeightMidnightRush)

	// 3. 是否真的在使用这个站点。只签到从不消费正是要抑制的模式；
	//    但没有签到历史的新账号不参与该项判定。
	first, err := model.FirstCheckinTimestamp(userId)
	if err != nil || first == 0 || now.Unix()-first < behaviorUsageLookbackDays*86400 {
		score += behaviorWeightRealUsage
	} else {
		used, err := model.UserHasConsumptionSince(userId,
			now.Add(-behaviorUsageLookbackDays*24*time.Hour).Unix())
		if err != nil || used {
			score += behaviorWeightRealUsage
		}
	}

	return score
}
