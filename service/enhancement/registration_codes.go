package enhancement

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

const MaxGenerateRegistrationCode = 100

type RegistrationCodeSummary struct {
	Id           int    `json:"id"`
	UserId       int    `json:"user_id"`
	Code         string `json:"code"`
	Status       int    `json:"status"`
	Name         string `json:"name"`
	MaxUses      int    `json:"max_uses"`
	UsedCount    int    `json:"used_count"`
	OpenTime     int64  `json:"open_time"`
	CreatedTime  int64  `json:"created_time"`
	LastUsedTime int64  `json:"last_used_time"`
}

type GenerateRegistrationCodesRequest struct {
	Count    int    `json:"count"`
	Name     string `json:"name"`
	MaxUses  int    `json:"max_uses"`
	OpenTime int64  `json:"open_time"`
	Code     string `json:"code"`
}

type RegistrationCodeConfigRequest struct {
	RegistrationCodeRequired       bool  `json:"registration_code_required"`
	RegistrationCodeForceStartTime int64 `json:"registration_code_force_start_time"`
}

func RegistrationCodeConfig() map[string]interface{} {
	cfg := setting.GetEnhancementSetting()
	return map[string]interface{}{
		"registration_code_required":         cfg.RegistrationCodeRequired,
		"registration_code_force_active":     setting.IsRegistrationCodeForceActive(),
		"registration_code_force_start_time": cfg.RegistrationCodeForceStartTime,
	}
}

func SaveRegistrationCodeConfig(req RegistrationCodeConfigRequest, operatorId int) error {
	if req.RegistrationCodeForceStartTime < 0 {
		return errors.New("registration_code_force_start_time is invalid")
	}
	if err := model.UpdateOption("enhancement_setting.registration_code_required", strconv.FormatBool(req.RegistrationCodeRequired)); err != nil {
		return err
	}
	if err := model.UpdateOption("enhancement_setting.registration_code_force_start_time", strconv.FormatInt(req.RegistrationCodeForceStartTime, 10)); err != nil {
		return err
	}
	audit(operatorId, "enhancements.registration_codes", "save_config", map[string]interface{}{
		"required":         req.RegistrationCodeRequired,
		"force_start_time": req.RegistrationCodeForceStartTime,
	})
	return nil
}

func GenerateRegistrationCodes(req GenerateRegistrationCodesRequest, operatorId int) ([]RegistrationCodeSummary, error) {
	if req.Count <= 0 || req.Count > MaxGenerateRegistrationCode {
		return nil, fmt.Errorf("count must be between 1 and %d", MaxGenerateRegistrationCode)
	}
	if req.MaxUses <= 0 {
		return nil, errors.New("max_uses must be greater than 0")
	}
	if req.OpenTime < 0 {
		return nil, errors.New("open_time is invalid")
	}
	name := strings.TrimSpace(req.Name)
	if len([]rune(name)) > 64 {
		return nil, errors.New("name is too long")
	}
	if name == "" {
		name = "registration"
	}
	manualCode := strings.TrimSpace(req.Code)
	if manualCode != "" {
		if req.Count != 1 {
			return nil, errors.New("manual code can only be used when count is 1")
		}
		if len([]rune(manualCode)) > 64 {
			return nil, errors.New("code is too long")
		}
	}

	now := common.GetTimestamp()
	codes := make([]model.RegistrationCode, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code := common.GetUUID()
		if manualCode != "" {
			code = manualCode
		}
		codes = append(codes, model.RegistrationCode{
			UserId:      operatorId,
			Code:        code,
			Status:      common.RegistrationCodeStatusEnabled,
			Name:        name,
			MaxUses:     req.MaxUses,
			OpenTime:    req.OpenTime,
			CreatedTime: now,
		})
	}
	if err := model.DB.Create(&codes).Error; err != nil {
		return nil, err
	}
	audit(operatorId, "enhancements.registration_codes", "generate", map[string]interface{}{
		"count":     req.Count,
		"max_uses":  req.MaxUses,
		"open_time": req.OpenTime,
	})
	out := make([]RegistrationCodeSummary, 0, len(codes))
	for _, code := range codes {
		out = append(out, registrationCodeToSummary(code))
	}
	return out, nil
}

func DeleteRegistrationCode(id int, operatorId int, force bool) error {
	if id <= 0 {
		return errors.New("invalid registration code id")
	}
	var code model.RegistrationCode
	if err := model.DB.Where("id = ?", id).First(&code).Error; err != nil {
		return err
	}
	if !force && code.UsedCount > 0 {
		return errors.New("used registration codes require root permission to delete")
	}
	if err := model.DB.Delete(&code).Error; err != nil {
		return err
	}
	audit(operatorId, "enhancements.registration_codes", "delete", map[string]interface{}{
		"registration_code_id": id,
		"force":                force,
	})
	return nil
}

func DisableRegistrationCode(id int, operatorId int) (RegistrationCodeSummary, error) {
	if id <= 0 {
		return RegistrationCodeSummary{}, errors.New("invalid registration code id")
	}
	var code model.RegistrationCode
	if err := model.DB.Where("id = ?", id).First(&code).Error; err != nil {
		return RegistrationCodeSummary{}, err
	}
	if code.Status != common.RegistrationCodeStatusDisabled {
		code.Status = common.RegistrationCodeStatusDisabled
		if err := model.DB.Model(&code).Update("status", common.RegistrationCodeStatusDisabled).Error; err != nil {
			return RegistrationCodeSummary{}, err
		}
		audit(operatorId, "enhancements.registration_codes", "disable", map[string]interface{}{
			"registration_code_id": id,
		})
	}
	return registrationCodeToSummary(code), nil
}

func EnableRegistrationCode(id int, operatorId int) (RegistrationCodeSummary, error) {
	if id <= 0 {
		return RegistrationCodeSummary{}, errors.New("invalid registration code id")
	}
	var code model.RegistrationCode
	if err := model.DB.Where("id = ?", id).First(&code).Error; err != nil {
		return RegistrationCodeSummary{}, err
	}
	if code.Status != common.RegistrationCodeStatusEnabled {
		code.Status = common.RegistrationCodeStatusEnabled
		if err := model.DB.Model(&code).Update("status", common.RegistrationCodeStatusEnabled).Error; err != nil {
			return RegistrationCodeSummary{}, err
		}
		audit(operatorId, "enhancements.registration_codes", "enable", map[string]interface{}{
			"registration_code_id": id,
		})
	}
	return registrationCodeToSummary(code), nil
}

func registrationCodeToSummary(code model.RegistrationCode) RegistrationCodeSummary {
	return RegistrationCodeSummary{
		Id:           code.Id,
		UserId:       code.UserId,
		Code:         code.Code,
		Status:       code.Status,
		Name:         code.Name,
		MaxUses:      code.MaxUses,
		UsedCount:    code.UsedCount,
		OpenTime:     code.OpenTime,
		CreatedTime:  code.CreatedTime,
		LastUsedTime: code.LastUsedTime,
	}
}

func registrationCodeMatchesQuery(item RegistrationCodeSummary, query ListQuery) bool {
	if !matchesKeyword(query.Keyword,
		strconv.Itoa(item.Id),
		strconv.Itoa(item.UserId),
		item.Code,
		strconv.Itoa(item.Status),
		item.Name,
		strconv.Itoa(item.MaxUses),
		strconv.Itoa(item.UsedCount),
		strconv.FormatInt(item.OpenTime, 10),
		strconv.FormatInt(item.CreatedTime, 10),
		strconv.FormatInt(item.LastUsedTime, 10),
	) {
		return false
	}
	return matchesFilters(query.Filters, map[string]func(string) bool{
		"id":             matchInt(int64(item.Id)),
		"user_id":        matchInt(int64(item.UserId)),
		"code":           matchText(item.Code),
		"status":         matchInt(int64(item.Status)),
		"name":           matchText(item.Name),
		"max_uses":       matchInt(int64(item.MaxUses)),
		"used_count":     matchInt(int64(item.UsedCount)),
		"open_time":      matchInt(item.OpenTime),
		"created_time":   matchInt(item.CreatedTime),
		"last_used_time": matchInt(item.LastUsedTime),
	})
}

func sortRegistrationCodeSummaries(items []RegistrationCodeSummary, sortKey string, order string) {
	desc := sortDesc(order)
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		result := 0
		switch sortKey {
		case "user_id":
			result = compareInt(int64(left.UserId), int64(right.UserId), desc)
		case "code":
			result = compareString(left.Code, right.Code, desc)
		case "status":
			result = compareInt(int64(left.Status), int64(right.Status), desc)
		case "name":
			result = compareString(left.Name, right.Name, desc)
		case "max_uses":
			result = compareInt(int64(left.MaxUses), int64(right.MaxUses), desc)
		case "used_count":
			result = compareInt(int64(left.UsedCount), int64(right.UsedCount), desc)
		case "open_time":
			result = compareInt(left.OpenTime, right.OpenTime, desc)
		case "created_time":
			result = compareInt(left.CreatedTime, right.CreatedTime, desc)
		case "last_used_time":
			result = compareInt(left.LastUsedTime, right.LastUsedTime, desc)
		case "id", "":
			result = compareInt(int64(left.Id), int64(right.Id), desc)
		}
		if result != 0 {
			return result < 0
		}
		return left.Id > right.Id
	})
}

func ListRegistrationCodes(query ListQuery) (PageResult[RegistrationCodeSummary], error) {
	query = normalizeListQuery(query)
	var codes []model.RegistrationCode
	if err := model.DB.Model(&model.RegistrationCode{}).Order("id DESC").Find(&codes).Error; err != nil {
		return PageResult[RegistrationCodeSummary]{}, err
	}
	items := make([]RegistrationCodeSummary, 0, len(codes))
	for _, code := range codes {
		item := registrationCodeToSummary(code)
		if registrationCodeMatchesQuery(item, query) {
			items = append(items, item)
		}
	}
	sortRegistrationCodeSummaries(items, query.Sort, query.Order)
	return pageResult(items, query.Page, query.PageSize), nil
}

func RegistrationCodeStats() (map[string]interface{}, error) {
	out := map[string]interface{}{}
	now := common.GetTimestamp()
	var total int64
	if err := model.DB.Model(&model.RegistrationCode{}).Count(&total).Error; err != nil {
		return nil, err
	}
	out["total"] = total

	statuses := map[string]int{
		"enabled":  common.RegistrationCodeStatusEnabled,
		"disabled": common.RegistrationCodeStatusDisabled,
	}
	for key, status := range statuses {
		var count int64
		if err := model.DB.Model(&model.RegistrationCode{}).Where("status = ?", status).Count(&count).Error; err != nil {
			return nil, err
		}
		out[key] = count
	}

	var exhausted int64
	if err := model.DB.Model(&model.RegistrationCode{}).
		Where("max_uses > 0 AND used_count >= max_uses").
		Count(&exhausted).Error; err != nil {
		return nil, err
	}
	out["exhausted"] = exhausted

	var notOpen int64
	if err := model.DB.Model(&model.RegistrationCode{}).
		Where("status = ? AND open_time > ?", common.RegistrationCodeStatusEnabled, now).
		Count(&notOpen).Error; err != nil {
		return nil, err
	}
	out["not_open"] = notOpen

	var usedCount int64
	if err := model.DB.Model(&model.RegistrationCode{}).
		Select("COALESCE(SUM(used_count), 0)").
		Scan(&usedCount).Error; err != nil {
		return nil, err
	}
	out["used_count"] = usedCount
	return out, nil
}
