package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func contextWithHeaders(headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/api/user/checkin", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.Request = req
	return c
}

// 典型 Chrome 同源 XHR
func browserHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36",
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Accept-Encoding": "gzip, deflate, br, zstd",
		"Sec-Ch-Ua":       `"Chromium";v="140", "Not=A?Brand";v="24"`,
		"Sec-Fetch-Site":  "same-origin",
		"Sec-Fetch-Mode":  "cors",
		"Sec-Fetch-Dest":  "empty",
		"Origin":          "https://example.com",
		"Referer":         "https://example.com/personal",
	}
}

// setupCheckinTestDB 按 service 包既有做法临时替换 model.DB / model.LOG_DB。
func setupCheckinTestDB(t *testing.T) {
	t.Helper()
	oldDB, oldLogDB := model.DB, model.LOG_DB
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Checkin{}, &model.Log{}))
	require.NoError(t, db.Exec("DELETE FROM checkins").Error)
	require.NoError(t, db.Exec("DELETE FROM logs").Error)
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		db.Exec("DELETE FROM checkins")
		db.Exec("DELETE FROM logs")
		model.DB, model.LOG_DB = oldDB, oldLogDB
	})
}

// seedCheckinAt 在指定日期的指定「当天秒数」写入一条历史签到。
func seedCheckinAt(t *testing.T, userId int, daysAgo int, secondOfDay int) {
	t.Helper()
	day := time.Now().In(time.Local).AddDate(0, 0, -daysAgo)
	midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.Local)
	require.NoError(t, model.DB.Create(&model.Checkin{
		UserId:       userId,
		CheckinDate:  midnight.Format("2006-01-02"),
		QuotaAwarded: 1,
		CreatedAt:    midnight.Unix() + int64(secondOfDay),
	}).Error)
}

// ---------- 请求头信号 ----------

func TestClientEnvironmentScoreBrowserHitsHeaderCeiling(t *testing.T) {
	require.Equal(t, checkinHeaderMaxScore,
		clientEnvironmentScore(contextWithHeaders(browserHeaders())))
}

func TestClientEnvironmentScoreBareScriptScoresZero(t *testing.T) {
	c := contextWithHeaders(map[string]string{
		"User-Agent":      "python-requests/2.32.3",
		"Accept":          "*/*",
		"Accept-Encoding": "gzip, deflate",
	})
	require.Equal(t, 0, clientEnvironmentScore(c))
}

func TestClientEnvironmentScoreSpoofedUserAgentStillLow(t *testing.T) {
	// 只把 UA 换成浏览器字符串远远不够：Fetch Metadata 等信号仍然缺失
	c := contextWithHeaders(map[string]string{
		"User-Agent":      browserHeaders()["User-Agent"],
		"Accept":          "*/*",
		"Accept-Encoding": "gzip",
	})
	score := clientEnvironmentScore(c)
	require.Greater(t, score, 0)
	require.Less(t, score, checkinHeaderMaxScore/3, "伪造 UA 不应接近浏览器分数")
}

func TestClientEnvironmentScoreHasNoSingleDecisiveSignal(t *testing.T) {
	// 逐个删除信号，确认没有任何单一请求头能独自决定结果，
	// 否则攻击者可以用二分法定位到那条规则。
	for _, name := range []string{
		"User-Agent", "Accept", "Accept-Language", "Accept-Encoding",
		"Sec-Ch-Ua", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest",
		"Origin", "Referer",
	} {
		h := browserHeaders()
		delete(h, name)
		score := clientEnvironmentScore(contextWithHeaders(h))
		require.Greater(t, score, checkinHeaderMaxScore*4/5,
			"删除单个请求头 %s 不应让分数崩塌，否则该头就是可被定位的判定点", name)
	}
}

func TestClientEnvironmentScoreKnownScriptAgentsAreRecognised(t *testing.T) {
	for _, ua := range []string{
		"python-requests/2.32.3", "curl/8.5.0", "Go-http-client/2.0",
		"okhttp/4.12.0", "axios/1.7.2", "node-fetch/3.3.2",
		"PostmanRuntime/7.39.0", "Java/17.0.2", "",
	} {
		require.False(t, looksLikeBrowserUserAgent(ua), "UA %q 不应被判为浏览器", ua)
	}
	require.True(t, looksLikeBrowserUserAgent(browserHeaders()["User-Agent"]))
}

// ---------- 圆周统计 ----------

func TestCircularStdDevHandlesMidnightWrap(t *testing.T) {
	// 23:59:30 与 00:00:30 实际只差 1 分钟。用普通方差会算成相差近 24 小时，
	// 正好把最像定时任务的跨零点样本误判成最分散的人类作息。
	wrap := []float64{86370, 30, 86340, 60}
	require.Less(t, circularStdDevSeconds(wrap), 300.0,
		"跨零点的密集样本必须被识别为高度集中")

	spread := []float64{9 * 3600, 14 * 3600, 20 * 3600, 11 * 3600}
	require.Greater(t, circularStdDevSeconds(spread), 3600.0)
}

func TestCircularStdDevIdenticalTimesIsZero(t *testing.T) {
	require.Equal(t, 0.0, circularStdDevSeconds([]float64{3600, 3600, 3600}))
}

// ---------- 行为特征 ----------

func TestBehaviorScoreNewUserIsNotPenalised(t *testing.T) {
	setupCheckinTestDB(t)
	// 没有任何历史的新用户必须拿满分，否则等于惩罚新注册
	noon := time.Date(2026, 8, 21, 12, 0, 0, 0, time.Local)
	require.Equal(t, checkinBehaviorMaxScore, checkinBehaviorScore(9001, noon))
}

func TestBehaviorScoreCronLikeUserScoresLow(t *testing.T) {
	setupCheckinTestDB(t)
	// 连续 7 天都在 00:00:05 签到：典型定时任务
	for d := 1; d <= 7; d++ {
		seedCheckinAt(t, 9002, d, 5)
	}
	midnight := time.Now().In(time.Local)
	midnight = time.Date(midnight.Year(), midnight.Month(), midnight.Day(), 0, 0, 5, 0, time.Local)

	score := checkinBehaviorScore(9002, midnight)
	require.Less(t, score, checkinBehaviorMaxScore/3,
		"固定时刻 + 掐零点 + 无消费应当拿到很低的行为分")
}

func TestBehaviorScoreHumanLikeUserScoresHigh(t *testing.T) {
	setupCheckinTestDB(t)
	// 时刻分散在白天不同时段
	for i, sec := range []int{9 * 3600, 15 * 3600, 21 * 3600, 11 * 3600, 19 * 3600} {
		seedCheckinAt(t, 9003, i+1, sec)
	}
	// 并且确实在使用这个站
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId: 9003, Type: model.LogTypeConsume, Quota: 100,
		CreatedAt: time.Now().Add(-2 * time.Hour).Unix(),
	}).Error)

	afternoon := time.Now().In(time.Local)
	afternoon = time.Date(afternoon.Year(), afternoon.Month(), afternoon.Day(), 14, 23, 0, 0, time.Local)

	require.Equal(t, checkinBehaviorMaxScore, checkinBehaviorScore(9003, afternoon))
}

func TestBehaviorScoreGradesSmoothlyWithoutCliff(t *testing.T) {
	setupCheckinTestDB(t)
	// 时刻抖动逐步增大时，分数应平滑上升而非在某点突跳，
	// 否则那个跳变点就是可被二分定位的判定阈值。
	var previous int
	for i, jitter := range []int{0, 200, 600, 1200, 2400, 4800} {
		userId := 9100 + i
		for d := 1; d <= 5; d++ {
			seedCheckinAt(t, userId, d, 10*3600+(d%2)*jitter)
		}
		noon := time.Now().In(time.Local)
		noon = time.Date(noon.Year(), noon.Month(), noon.Day(), 12, 0, 0, 0, time.Local)
		score := checkinBehaviorScore(userId, noon)
		require.GreaterOrEqual(t, score, previous, "抖动增大时行为分不应下降")
		previous = score
	}
}

// ---------- 组合 ----------

func TestCheckinClientScoreReturns100WhenDisabled(t *testing.T) {
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.ClientCheckEnabled = false
	t.Cleanup(func() { *setting = old })

	c := contextWithHeaders(map[string]string{"User-Agent": "curl/8.5.0"})
	require.Equal(t, 100, CheckinClientScore(c, 9004), "未启用时不得影响任何发放")
}

func TestCheckinClientScoreCombinesHeaderAndBehaviour(t *testing.T) {
	setupCheckinTestDB(t)
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.ClientCheckEnabled = true
	t.Cleanup(func() { *setting = old })

	// 全新用户 + 浏览器请求头 = 满分
	require.Equal(t, 100, CheckinClientScore(contextWithHeaders(browserHeaders()), 9005))

	// 无头浏览器（请求头完美）挂在 cron 上：请求头满分但行为分很低
	for d := 1; d <= 7; d++ {
		seedCheckinAt(t, 9006, d, 5)
	}
	headless := CheckinClientScore(contextWithHeaders(browserHeaders()), 9006)
	require.Less(t, headless, 100, "cron 驱动的无头浏览器不应拿到满分")
	require.GreaterOrEqual(t, headless, checkinHeaderMaxScore,
		"请求头完美时不应低于请求头上限")
}

// 端到端：脚本环境在 $0.01-$10 的区间下应恰好拿到下沿。
func TestScriptClientIsPinnedToMinimumReward(t *testing.T) {
	setupCheckinTestDB(t)
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.Enabled = true
	setting.ClientCheckEnabled = true
	setting.MinQuota = 5000    // $0.01
	setting.MaxQuota = 5000000 // $10
	t.Cleanup(func() { *setting = old })

	// 裸脚本 + 每天固定零点签到 + 从不消费
	for d := 1; d <= 7; d++ {
		seedCheckinAt(t, 9007, d, 5)
	}
	// 让该账号「够老」以参与消费关联判定
	seedCheckinAt(t, 9007, 30, 5)

	midnight := time.Now().In(time.Local)
	midnight = time.Date(midnight.Year(), midnight.Month(), midnight.Day(), 0, 0, 3, 0, time.Local)
	scriptScore := clientEnvironmentScore(contextWithHeaders(map[string]string{
		"User-Agent": "python-requests/2.32.3",
		"Accept":     "*/*",
	})) + checkinBehaviorScore(9007, midnight)

	require.Equal(t, 0, scriptScore, "裸脚本 + cron + 无消费应当得 0 分")
	for _, roll := range []int{5000, 1234567, 5000000} {
		require.Equal(t, setting.MinQuota, setting.ApplyClientScore(roll, scriptScore))
	}
}
