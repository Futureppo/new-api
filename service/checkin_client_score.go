package service

import (
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// 常见脚本 / SDK 的 User-Agent 特征片段（小写匹配）。
var scriptUserAgentMarkers = []string{
	"python-requests", "python-urllib", "aiohttp", "httpx", "urllib3",
	"curl/", "wget/", "go-http-client", "okhttp", "axios/", "node-fetch",
	"undici", "java/", "apache-httpclient", "postmanruntime", "insomnia",
	"scrapy", "guzzlehttp", "restsharp", "libwww-perl", "powershell",
	"reqwest", "hyper/", "winhttp", "http_request2", "httpie",
}

// checkinHeaderMaxScore 请求头信号的分数上限，其余分数由行为特征贡献。
const checkinHeaderMaxScore = 55

func looksLikeBrowserUserAgent(ua string) bool {
	ua = strings.ToLower(strings.TrimSpace(ua))
	if ua == "" {
		return false
	}
	for _, marker := range scriptUserAgentMarkers {
		if strings.Contains(ua, marker) {
			return false
		}
	}
	return strings.Contains(ua, "mozilla/5.0")
}

// clientEnvironmentScore 计算环境一致性分，0-checkinHeaderMaxScore。
func clientEnvironmentScore(c *gin.Context) int {
	if c == nil || c.Request == nil {
		return 0
	}
	ua := strings.ToLower(c.Request.UserAgent())
	if !looksLikeBrowserUserAgent(ua) || strings.Contains(ua, "headlesschrome") || strings.Contains(ua, "phantomjs") {
		return 0
	}
	score := checkinHeaderMaxScore
	// Missing optional browser/privacy headers is neutral. Supplied contradictions are evidence.
	for name, allowed := range map[string]map[string]bool{
		"Sec-Fetch-Mode": {"cors": true, "same-origin": true},
		"Sec-Fetch-Site": {"same-origin": true, "same-site": true},
		"Sec-Fetch-Dest": {"empty": true},
	} {
		if value := strings.ToLower(strings.TrimSpace(c.GetHeader(name))); value != "" && !allowed[value] {
			score -= 12
		}
	}
	for _, name := range []string{"Origin", "Referer"} {
		value := c.GetHeader(name)
		if value == "" {
			continue
		}
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Host, c.Request.Host) {
			score -= 12
		}
	}
	if c.GetHeader("Accept-Language") == "" && c.GetHeader("Sec-Fetch-Mode") == "" && c.GetHeader("Origin") == "" && c.GetHeader("Referer") == "" {
		score -= 45
	}
	return max(0, score)
}

// CheckinClientScore scores only check-in requests. Browser headers are weak evidence;
// persistent timing and cross-account clusters are evaluated separately. The existing
// client_check_enabled setting controls this system; it does not enable Turnstile.
func CheckinClientScore(c *gin.Context, userId int) int {
	return checkinClientScoreAt(c, userId, time.Now())
}

func checkinClientScoreAt(c *gin.Context, userId int, now time.Time) int {
	setting := operation_setting.GetCheckinSetting()
	if setting == nil || !setting.ClientCheckEnabled {
		return 100
	}
	return assessCheckinRiskAt(c, userId, now).Score
}
