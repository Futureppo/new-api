package service

import (
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

// checkinScoreSignal 单个信号及其权重，权重合计 checkinHeaderMaxScore。
type checkinScoreSignal struct {
	weight int
	match  func(c *gin.Context) bool
}

// 刻意使用多个小权重信号而非单一判定条件：
// 改动任何一个请求头只会让分数小幅移动，无法反推出具体命中了哪条规则。
var checkinScoreSignals = []checkinScoreSignal{
	// Fetch Metadata 系列由浏览器强制附加，脚本极少凑齐
	{7, func(c *gin.Context) bool { return c.GetHeader("Sec-Fetch-Mode") != "" }},
	{7, func(c *gin.Context) bool { return c.GetHeader("Sec-Fetch-Site") != "" }},
	{7, func(c *gin.Context) bool { return c.GetHeader("Sec-Fetch-Dest") != "" }},
	// 浏览器必发 Accept-Language，简单脚本通常缺失
	{8, func(c *gin.Context) bool { return strings.TrimSpace(c.GetHeader("Accept-Language")) != "" }},
	// 现代浏览器会协商 br / zstd，裸 HTTP 客户端一般只有 gzip 或没有
	{6, func(c *gin.Context) bool {
		enc := strings.ToLower(c.GetHeader("Accept-Encoding"))
		return strings.Contains(enc, "br") || strings.Contains(enc, "zstd")
	}},
	// Chromium 客户端提示
	{5, func(c *gin.Context) bool { return c.GetHeader("Sec-Ch-Ua") != "" }},
	// 同源 XHR 会带 Origin 或 Referer
	{6, func(c *gin.Context) bool {
		return c.GetHeader("Origin") != "" || c.GetHeader("Referer") != ""
	}},
	// UA 形似浏览器且不在脚本特征表里
	{6, func(c *gin.Context) bool { return looksLikeBrowserUserAgent(c.Request.UserAgent()) }},
	// 浏览器的 Accept 通常带具体类型，而非只有 */*
	{3, func(c *gin.Context) bool {
		accept := strings.TrimSpace(c.GetHeader("Accept"))
		return accept != "" && accept != "*/*"
	}},
}

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

// clientEnvironmentScore 计算原始环境分，0-100，越高越像真实浏览器。
func clientEnvironmentScore(c *gin.Context) int {
	if c == nil || c.Request == nil {
		return 0
	}
	score := 0
	for _, signal := range checkinScoreSignals {
		if signal.match(c) {
			score += signal.weight
		}
	}
	if score > checkinHeaderMaxScore {
		score = checkinHeaderMaxScore
	}
	return score
}

// CheckinClientScore 返回本次签到的客户端可信度分（0-100）。
// 功能未启用时恒定返回 100，即完全不影响发放。
//
// 分数由两部分互补的信号组成：
//   - 请求头特征（上限 55）：识别裸 HTTP 客户端，对无头浏览器无效
//   - 历史行为特征（上限 45）：识别「定时任务驱动、且从不真正使用服务」的账号，
//     无头浏览器只要挂在 cron 上就会在这里失分
//
// 这里对可疑环境压低发放额度而不是拒绝请求：拒绝会给出明确的成功/失败信号，
// 攻击者可以逐个调整请求头做二分测试直到绕过。压低额度则没有任何反馈——
// 由于结果始终落在配置的 [MinQuota, MaxQuota] 区间内，被压制的签到与
// 一次运气不好的正常签到在响应上完全一致。
//
// 所有信号都是小权重累加并线性映射、不设阈值：改动任何单个请求头或单次
// 签到时间只会让结果小幅移动，无法定位到具体命中了哪条规则。
func CheckinClientScore(c *gin.Context, userId int) int {
	setting := operation_setting.GetCheckinSetting()
	if setting == nil || !setting.ClientCheckEnabled {
		return 100
	}
	return clientEnvironmentScore(c) + checkinBehaviorScore(userId, time.Now())
}
