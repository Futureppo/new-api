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
	AutoBanUser     bool   `json:"auto_ban_user"`
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
		Target:      target,
		Reason:      reason,
		ExpiresAt:   req.ExpiresAt,
		AutoBanUser: ipBanAutoBanUserEnabled(req.ExpiresAt, req.AutoBanUser),
		CreatedBy:   c.GetInt("id"),
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
	ban.AutoBanUser = ipBanAutoBanUserEnabled(req.ExpiresAt, req.AutoBanUser)
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

type IPBanBatchIdsRequest struct {
	Ids []int `json:"ids"`
}

// BatchDeleteIPBans deletes multiple IP ban rules in one call.
func BatchDeleteIPBans(c *gin.Context) {
	req := IPBanBatchIdsRequest{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	ids := dedupIntSlice(req.Ids)
	if len(ids) == 0 {
		common.ApiErrorMsg(c, "请选择要删除的封禁规则")
		return
	}
	affected, err := model.DeleteIPBansByIds(ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if affected > 0 {
		model.InitIPBanCache()
	}
	common.ApiSuccess(c, gin.H{
		"deleted": affected,
		"ids":     ids,
	})
}

// IPBanBatchUpdateRequest describes fields that can be batch-updated on IP ban rules.
// Only fields with non-nil pointers are applied — this allows partial updates.
type IPBanBatchUpdateRequest struct {
	Ids             []int   `json:"ids"`
	Reason          *string `json:"reason,omitempty"`
	ExpiresAt       *int64  `json:"expires_at,omitempty"`       // 0 => 转永久；>0 => 临时并设置过期时间
	AutoBanUser     *bool   `json:"auto_ban_user,omitempty"`    // 仅永久规则生效
	ConfirmSelfLock bool    `json:"confirm_self_lock,omitempty"`
}

// BatchUpdateIPBans updates selected fields for multiple IP ban rules at once.
func BatchUpdateIPBans(c *gin.Context) {
	req := IPBanBatchUpdateRequest{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	ids := dedupIntSlice(req.Ids)
	if len(ids) == 0 {
		common.ApiErrorMsg(c, "请选择要修改的封禁规则")
		return
	}
	if req.Reason == nil && req.ExpiresAt == nil && req.AutoBanUser == nil {
		common.ApiErrorMsg(c, "请指定至少一个要修改的字段")
		return
	}

	updates := map[string]interface{}{}
	var trimmedReason string
	if req.Reason != nil {
		trimmedReason = strings.TrimSpace(*req.Reason)
		if err := validateIPBanReason(trimmedReason); err != nil {
			common.ApiError(c, err)
			return
		}
		updates["reason"] = trimmedReason
	}
	if req.ExpiresAt != nil {
		if err := validateIPBanExpiresAt(*req.ExpiresAt); err != nil {
			common.ApiError(c, err)
			return
		}
		updates["expires_at"] = *req.ExpiresAt
		// 转为临时时不能保留 auto_ban_user；转为永久时保持原值（除非同时显式指定）。
		if *req.ExpiresAt > 0 {
			updates["auto_ban_user"] = false
		}
	}
	if req.AutoBanUser != nil {
		// 只在最终为永久规则时启用；否则强制关闭。
		if req.ExpiresAt != nil {
			if *req.ExpiresAt == 0 {
				updates["auto_ban_user"] = *req.AutoBanUser
			}
		} else {
			// 未变更过期时间时，逐个检查目标记录当前是否为永久
			existing, err := model.GetIPBansByIds(ids)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			permanentIds := make([]int, 0, len(existing))
			for _, ban := range existing {
				if ban.ExpiresAt == 0 {
					permanentIds = append(permanentIds, ban.Id)
				}
			}
			if len(permanentIds) == 0 {
				common.ApiErrorMsg(c, "所选规则中没有永久封禁，无法修改命中后封禁账号")
				return
			}
			// 单独走一次更新
			if _, err := model.BatchUpdateIPBanFields(permanentIds, map[string]interface{}{
				"auto_ban_user": *req.AutoBanUser,
			}); err != nil {
				common.ApiError(c, err)
				return
			}
			// auto_ban_user 已单独处理，剔除，避免下面重复
		}
	}

	// 自锁检测：仅当修改会保留/引入生效状态时校验
	if !req.ConfirmSelfLock {
		existing, err := model.GetIPBansByIds(ids)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		targets := make([]string, 0, len(existing))
		for _, ban := range existing {
			targets = append(targets, ban.Target)
		}
		if len(targets) > 0 && selfLockConfirmationRequired(c, req.ConfirmSelfLock, targets) {
			return
		}
	}

	var affected int64
	if len(updates) > 0 {
		var err error
		affected, err = model.BatchUpdateIPBanFields(ids, updates)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}

	if affected > 0 || req.AutoBanUser != nil {
		model.InitIPBanCache()
	}
	common.ApiSuccess(c, gin.H{
		"updated": affected,
		"ids":     ids,
	})
}

// GetIPBanRelatedUsers returns users that were auto-banned by the given IP ban rule,
// resolved via the ip_ban_user_bans association table (accurate ban_id → user_id link).
func GetIPBanRelatedUsers(c *gin.Context) {
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
	users, err := model.GetIPBanRelatedUsersByBanId(id, 200)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"ban":   ban,
		"users": users,
	})
}

func dedupIntSlice(input []int) []int {
	if len(input) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(input))
	out := make([]int, 0, len(input))
	for _, v := range input {
		if v <= 0 {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
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

func ipBanAutoBanUserEnabled(expiresAt int64, requested bool) bool {
	return expiresAt == 0 && requested
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
