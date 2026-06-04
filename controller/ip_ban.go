package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxIPBanReasonLength = 255

type IPBanRequest struct {
	Id              int    `json:"id"`
	Target          string `json:"target"`
	Reason          string `json:"reason"`
	ExpiresAt       int64  `json:"expires_at"`
	ConfirmSelfLock bool   `json:"confirm_self_lock"`
}

type IPBanBatchRequest struct {
	Lines           string `json:"lines"`
	DefaultReason   string `json:"default_reason"`
	ExpiresAt       int64  `json:"expires_at"`
	ConfirmSelfLock bool   `json:"confirm_self_lock"`
}

type IPBanBatchEntry struct {
	LineNumber int    `json:"line_number"`
	Target     string `json:"target"`
	Reason     string `json:"reason"`
}

type IPBanBatchInvalidLine struct {
	LineNumber int    `json:"line_number"`
	Content    string `json:"content"`
	Message    string `json:"message"`
}

func GetAllIPBans(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	bans, total, err := model.GetAllIPBans(c.Query("type"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(bans)
	common.ApiSuccess(c, pageInfo)
}

func SearchIPBans(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	bans, total, err := model.SearchIPBans(c.Query("type"), c.Query("keyword"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(bans)
	common.ApiSuccess(c, pageInfo)
}

func GetIPBan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ban, err := model.GetIPBanById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, ban)
}

func AddIPBan(c *gin.Context) {
	req := IPBanRequest{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}

	target, reason, err := validateIPBanInput(req.Target, req.Reason, req.ExpiresAt)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if selfLockConfirmationRequired(c, req.ConfirmSelfLock, []string{target}) {
		return
	}
	if _, err := model.GetIPBanByTarget(target); err == nil {
		common.ApiErrorMsg(c, "该IP或IP段已存在")
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiError(c, err)
		return
	}

	ban := &model.IPBan{
		Target:    target,
		Reason:    reason,
		ExpiresAt: req.ExpiresAt,
		CreatedBy: c.GetInt("id"),
	}
	if err := model.CreateIPBan(ban); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitIPBanCache()
	common.ApiSuccess(c, ban)
}

func UpdateIPBan(c *gin.Context) {
	req := IPBanRequest{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Id == 0 {
		common.ApiErrorMsg(c, "id为空")
		return
	}

	ban, err := model.GetIPBanById(req.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	target, reason, err := validateIPBanInput(req.Target, req.Reason, req.ExpiresAt)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if selfLockConfirmationRequired(c, req.ConfirmSelfLock, []string{target}) {
		return
	}
	if existing, err := model.GetIPBanByTarget(target); err == nil && existing.Id != req.Id {
		common.ApiErrorMsg(c, "该IP或IP段已存在")
		return
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiError(c, err)
		return
	}

	ban.Target = target
	ban.Reason = reason
	ban.ExpiresAt = req.ExpiresAt
	if err := model.UpdateIPBan(ban); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitIPBanCache()
	common.ApiSuccess(c, ban)
}

func DeleteIPBan(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteIPBanById(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitIPBanCache()
	common.ApiSuccess(c, gin.H{"id": id})
}

func BatchCreateIPBans(c *gin.Context) {
	req := IPBanBatchRequest{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateIPBanExpiresAt(req.ExpiresAt); err != nil {
		common.ApiError(c, err)
		return
	}

	entries, invalidLines := parseIPBanBatchLines(req.Lines, req.DefaultReason)
	targets := make([]string, 0, len(entries))
	for _, entry := range entries {
		targets = append(targets, entry.Target)
	}
	if len(targets) > 0 && selfLockConfirmationRequired(c, req.ConfirmSelfLock, targets) {
		return
	}

	created := make([]*model.IPBan, 0, len(entries))
	skipped := make([]IPBanBatchEntry, 0)
	for _, entry := range entries {
		if _, err := model.GetIPBanByTarget(entry.Target); err == nil {
			skipped = append(skipped, entry)
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiError(c, err)
			return
		}
		ban := &model.IPBan{
			Target:    entry.Target,
			Reason:    entry.Reason,
			ExpiresAt: req.ExpiresAt,
			CreatedBy: c.GetInt("id"),
		}
		if err := model.CreateIPBan(ban); err != nil {
			common.ApiError(c, err)
			return
		}
		created = append(created, ban)
	}
	if len(created) > 0 {
		model.InitIPBanCache()
	}
	common.ApiSuccess(c, gin.H{
		"created":       len(created),
		"skipped":       len(skipped),
		"invalid":       invalidLines,
		"created_items": created,
		"skipped_items": skipped,
	})
}

func validateIPBanInput(target string, reason string, expiresAt int64) (string, string, error) {
	normalizedTarget, err := model.NormalizeIPBanTarget(target)
	if err != nil {
		return "", "", err
	}
	reason = strings.TrimSpace(reason)
	if err := validateIPBanReason(reason); err != nil {
		return "", "", err
	}
	if err := validateIPBanExpiresAt(expiresAt); err != nil {
		return "", "", err
	}
	return normalizedTarget, reason, nil
}

func validateIPBanReason(reason string) error {
	if reason == "" {
		return errors.New("封禁原因不能为空")
	}
	if utf8.RuneCountInString(reason) > maxIPBanReasonLength {
		return errors.New("封禁原因不能超过255个字符")
	}
	return nil
}

func validateIPBanExpiresAt(expiresAt int64) error {
	if expiresAt != 0 && expiresAt <= common.GetTimestamp() {
		return errors.New("临时封禁过期时间必须晚于当前时间")
	}
	return nil
}

func selfLockConfirmationRequired(c *gin.Context, confirmed bool, targets []string) bool {
	if confirmed {
		return false
	}
	clientIP := c.ClientIP()
	for _, target := range targets {
		matched, err := model.IsIPBanTargetMatchClient(target, clientIP)
		if err != nil {
			continue
		}
		if matched {
			c.JSON(http.StatusOK, gin.H{
				"success": false,
				"message": "该规则会封禁你当前的IP，请确认后再提交",
				"data": gin.H{
					"requires_confirmation": true,
					"target":                target,
					"client_ip":             clientIP,
				},
			})
			return true
		}
	}
	return false
}

func parseIPBanBatchLines(lines string, defaultReason string) ([]IPBanBatchEntry, []IPBanBatchInvalidLine) {
	defaultReason = strings.TrimSpace(defaultReason)
	entries := make([]IPBanBatchEntry, 0)
	invalidLines := make([]IPBanBatchInvalidLine, 0)
	seenTargets := make(map[string]struct{})

	for idx, rawLine := range strings.Split(lines, "\n") {
		lineNumber := idx + 1
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		target, reason := splitIPBanBatchLine(line)
		if reason == "" {
			reason = defaultReason
		}
		normalizedTarget, err := model.NormalizeIPBanTarget(target)
		if err != nil {
			invalidLines = append(invalidLines, IPBanBatchInvalidLine{
				LineNumber: lineNumber,
				Content:    line,
				Message:    err.Error(),
			})
			continue
		}
		if err := validateIPBanReason(reason); err != nil {
			invalidLines = append(invalidLines, IPBanBatchInvalidLine{
				LineNumber: lineNumber,
				Content:    line,
				Message:    err.Error(),
			})
			continue
		}
		if _, ok := seenTargets[normalizedTarget]; ok {
			continue
		}
		seenTargets[normalizedTarget] = struct{}{}
		entries = append(entries, IPBanBatchEntry{
			LineNumber: lineNumber,
			Target:     normalizedTarget,
			Reason:     reason,
		})
	}

	return entries, invalidLines
}

func splitIPBanBatchLine(line string) (string, string) {
	line = strings.TrimSpace(line)
	sep := strings.IndexFunc(line, unicode.IsSpace)
	if sep < 0 {
		return line, ""
	}
	return strings.TrimSpace(line[:sep]), strings.TrimSpace(line[sep:])
}
