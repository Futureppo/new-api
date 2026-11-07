package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
	"time"
)

type CheckinRiskAssessment struct {
	Score    int
	Evidence model.CheckinRiskEvidence
}

func checkinRiskHash(kind, value string) string {
	if value == "" {
		return ""
	}
	h := hmac.New(sha256.New, []byte(common.CryptoSecret))
	h.Write([]byte(kind + "\x00" + value))
	return hex.EncodeToString(h.Sum(nil))
}

// Server-signed first-party marker, used only as corroborating evidence. Clearing
// it is not an offence. Raw addresses and cookie values are never stored.
func EnsureCheckinRiskDevice(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if cached, ok := c.Get("checkin_risk_device"); ok {
		return cached.(string)
	}
	raw, _ := c.Cookie("checkin_device")
	parts := strings.Split(raw, ".")
	valid := len(parts) == 2 && len(parts[0]) == 32 && hmac.Equal([]byte(parts[1]), []byte(checkinRiskHash("device-signature", parts[0])))
	if !valid {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return ""
		}
		id := hex.EncodeToString(nonce)
		raw = id + "." + checkinRiskHash("device-signature", id)
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie("checkin_device", raw, 30*86400, "/", "", c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https", true)
	}
	value := checkinRiskHash("device", raw)
	c.Set("checkin_risk_device", value)
	return value
}

func AssessCheckinRisk(c *gin.Context, userID int) CheckinRiskAssessment {
	return assessCheckinRiskAt(c, userID, time.Now())
}
func assessCheckinRiskAt(c *gin.Context, userID int, now time.Time) CheckinRiskAssessment {
	result := CheckinRiskAssessment{Score: 100}
	if !operation_setting.GetCheckinSetting().ClientCheckEnabled {
		return result
	}
	reasons := []string{}
	header := clientEnvironmentScore(c)
	behavior := checkinBehaviorScore(userID, now)
	result.Score = header + behavior
	if header < checkinHeaderMaxScore {
		reasons = append(reasons, "client_inconsistent")
	}
	if behavior < checkinBehaviorMaxScore {
		reasons = append(reasons, "precise_schedule_without_usage")
	}
	if c != nil && c.Request != nil {
		result.Evidence.Network = checkinRiskHash("network", c.ClientIP())
		result.Evidence.Client = checkinRiskHash("client", strings.ToLower(strings.TrimSpace(c.Request.UserAgent())))
		result.Evidence.Device = EnsureCheckinRiskDevice(c)
	}
	stats, err := model.GetCheckinClusterStats(userID, result.Evidence.Network, result.Evidence.Client, result.Evidence.Device, now.Unix())
	if err != nil {
		reasons = append(reasons, "cluster_data_unavailable")
	} else {
		// IP sharing alone never penalises a user. Require device reuse or both a
		// matching client and a tightly synchronized burst of distinct accounts.
		if stats.DeviceAccounts >= 4 {
			result.Score -= min(55, 15+(stats.DeviceAccounts-4)*5)
			reasons = append(reasons, "multi_account_device")
		}
		// The same browser version behind NAT is common. A network burst needs
		// independent client/behaviour/device evidence before lowering rewards.
		corroborated := header < checkinHeaderMaxScore || behavior < checkinBehaviorMaxScore || stats.DeviceAccounts >= 4
		if stats.ClientAccounts >= 10 && stats.ClientMinuteAccounts >= 5 && corroborated {
			result.Score -= min(40, 15+(stats.ClientAccounts-10)*2)
			reasons = append(reasons, "synchronized_account_cluster")
		}
	}
	result.Score = max(0, min(100, result.Score))
	detail := struct {
		Version  int                       `json:"version"`
		Header   int                       `json:"header"`
		Behavior int                       `json:"behavior"`
		Clusters model.CheckinClusterStats `json:"clusters"`
		Reasons  []string                  `json:"reasons"`
	}{2, header, behavior, stats, reasons}
	if data, err := common.Marshal(detail); err == nil {
		result.Evidence.Detail = string(data)
	}
	return result
}
