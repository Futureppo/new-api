package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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

func TestClientEnvironmentScoreBrowserScoresHigh(t *testing.T) {
	require.Equal(t, 100, clientEnvironmentScore(contextWithHeaders(browserHeaders())))
}

func TestClientEnvironmentScoreBareScriptScoresZero(t *testing.T) {
	// python-requests 的默认请求头
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
	require.Less(t, score, 30, "伪造 UA 不应接近浏览器分数")
}

func TestClientEnvironmentScoreHasNoSingleDecisiveSignal(t *testing.T) {
	// 逐个删除信号，确认没有任何单一请求头能把分数打到 0 或独自撑到 100，
	// 否则攻击者可以用二分法定位到具体规则。
	headerNames := []string{
		"User-Agent", "Accept", "Accept-Language", "Accept-Encoding",
		"Sec-Ch-Ua", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest",
		"Origin", "Referer",
	}
	for _, name := range headerNames {
		h := browserHeaders()
		delete(h, name)
		score := clientEnvironmentScore(contextWithHeaders(h))
		require.Greater(t, score, 50,
			"删除单个请求头 %s 不应让分数崩塌，否则该头就是可被定位的判定点", name)
		require.LessOrEqual(t, score, 100)
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

func TestCheckinClientScoreReturns100WhenDisabled(t *testing.T) {
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.ClientCheckEnabled = false
	t.Cleanup(func() { *setting = old })

	c := contextWithHeaders(map[string]string{"User-Agent": "curl/8.5.0"})
	require.Equal(t, 100, CheckinClientScore(c), "未启用时不得影响任何发放")
}

func TestCheckinClientScoreAppliesWhenEnabled(t *testing.T) {
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.ClientCheckEnabled = true
	t.Cleanup(func() { *setting = old })

	c := contextWithHeaders(map[string]string{"User-Agent": "curl/8.5.0"})
	require.Equal(t, 0, CheckinClientScore(c))
	require.Equal(t, 100, CheckinClientScore(contextWithHeaders(browserHeaders())))
}

// 端到端：脚本环境在 0.01-10 的区间下应恰好拿到下沿。
func TestScriptClientIsPinnedToMinimumReward(t *testing.T) {
	setting := operation_setting.GetCheckinSetting()
	old := *setting
	setting.Enabled = true
	setting.ClientCheckEnabled = true
	setting.MinQuota = 5000    // $0.01
	setting.MaxQuota = 5000000 // $10
	t.Cleanup(func() { *setting = old })

	scriptScore := CheckinClientScore(contextWithHeaders(map[string]string{
		"User-Agent": "python-requests/2.32.3",
		"Accept":     "*/*",
	}))
	// 无论那天摇到区间里的哪个值，脚本最终都会被压到最小金额
	for _, roll := range []int{5000, 1234567, 5000000} {
		require.Equal(t, setting.MinQuota, setting.ApplyClientScore(roll, scriptScore))
	}
	// 浏览器则原样保留
	browserScore := CheckinClientScore(contextWithHeaders(browserHeaders()))
	require.Equal(t, 1234567, setting.ApplyClientScore(1234567, browserScore))
}
