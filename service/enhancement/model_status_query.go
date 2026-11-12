package enhancement

import (
	"context"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

// A refresh reads a bounded time range once, instead of scanning the lifetime
// history for each model/group (including groups with no matching requests).
type modelStatusBucket struct {
	ModelName        string
	GroupName        string
	BucketIndex      int
	Type             int
	Requests         int64
	Quota            int64
	PromptTokens     int64
	CompletionTokens int64
	UseTime          int64
	LastRequestAt    int64
}

func quotedColumn(db *gorm.DB, name string) string {
	var b strings.Builder
	db.Dialector.QuoteTo(&b, name)
	return b.String()
}

func statusTargetQuery(db *gorm.DB, targets []modelStatusTarget, window modelStatusWindow) *gorm.DB {
	models, groups := make([]string, 0), make([]string, 0)
	modelSet, groupSet := map[string]bool{}, map[string]bool{}
	for _, t := range targets {
		if !modelSet[t.Model] {
			models = append(models, t.Model)
			modelSet[t.Model] = true
		}
		if !groupSet[t.Group] {
			groups = append(groups, t.Group)
			groupSet[t.Group] = true
		}
	}
	return db.Model(&model.Log{}).Where("created_at >= ? AND created_at <= ?", window.Start, window.End).
		Where("model_name IN ?", models).Where(quotedColumn(db, "group")+" IN ?", groups)
}

func excludeStatusErrors(query *gorm.DB) *gorm.DB {
	keywords := modelStatusIgnoredErrorKeywordsForMatch()
	if len(keywords) == 0 {
		return query
	}
	text := "LOWER(COALESCE(content, '') || char(10) || COALESCE(other, ''))"
	switch query.Dialector.Name() {
	case "postgres":
		text = "LOWER(COALESCE(content, '') || chr(10) || COALESCE(other, ''))"
	case "mysql":
		text = "LOWER(CONCAT(COALESCE(content, ''), CHAR(10), COALESCE(other, '')))"
	}
	conditions, args := make([]string, 0, len(keywords)), []interface{}{model.LogTypeError}
	escape := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	for _, keyword := range keywords {
		conditions = append(conditions, text+" LIKE ? ESCAPE '!'")
		args = append(args, "%"+escape.Replace(keyword)+"%")
	}
	return query.Where("NOT (type = ? AND ("+strings.Join(conditions, " OR ")+"))", args...)
}

func readRecentStatusSamples(db *gorm.DB, targets []modelStatusTarget, window modelStatusWindow) ([]model.Log, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	var ids []int
	if db.Dialector.Name() == "mysql" {
		// MySQL 5.7 has no window functions. Only active targets reach this
		// fallback, and every lookup is restricted to the reporting window.
		for _, target := range targets {
			var rows []model.Log
			if err := statusTargetQuery(db, []modelStatusTarget{target}, window).
				Select("id").Where("type = ?", model.LogTypeConsume).
				Order("created_at DESC, id DESC").Limit(recentModelStatusLogLimit).Find(&rows).Error; err != nil {
				return nil, err
			}
			for _, row := range rows {
				ids = append(ids, row.Id)
			}
		}
	} else {
		// Rank narrow rows first; fetch the JSON payload only for the selected
		// samples, never for every request in a busy reporting window.
		group := quotedColumn(db, "group")
		ranked := statusTargetQuery(db, targets, window).Where("type = ?", model.LogTypeConsume).
			Select("id, ROW_NUMBER() OVER (PARTITION BY model_name, " + group + " ORDER BY created_at DESC, id DESC) AS sample_rank")
		if err := db.Table("(?) AS ranked_samples", ranked).Where("sample_rank <= ?", recentModelStatusLogLimit).Pluck("id", &ids).Error; err != nil {
			return nil, err
		}
	}
	var samples []model.Log
	// Bound bind variables for SQLite and avoid oversized IN clauses.
	for start := 0; start < len(ids); start += 500 {
		end := start + 500
		if end > len(ids) {
			end = len(ids)
		}
		var rows []model.Log
		if err := db.Model(&model.Log{}).Select([]string{"id", "model_name", "group", "created_at", "completion_tokens", "use_time", "is_stream", "other"}).Where("id IN ?", ids[start:end]).Find(&rows).Error; err != nil {
			return nil, err
		}
		samples = append(samples, rows...)
	}
	return samples, nil
}

func modelStatusesForTargets(targets []modelStatusTarget, window string) ([]ModelStatus, error) {
	return aggregateModelStatuses(targets, resolveModelStatusWindow(window))
}

func aggregateModelStatuses(targets []modelStatusTarget, resolved modelStatusWindow) ([]ModelStatus, error) {
	out := make([]ModelStatus, 0, len(targets))
	if len(targets) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := model.LOG_DB.WithContext(ctx)
	group := quotedColumn(db, "group")
	bucket := "((created_at - ?) / ?)"
	if db.Dialector.Name() == "mysql" {
		bucket = "FLOOR(" + bucket + ")"
	}
	var buckets []modelStatusBucket
	query := excludeStatusErrors(statusTargetQuery(db, targets, resolved).Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeError}))
	err := query.Select("model_name, "+group+" AS group_name, "+bucket+" AS bucket_index, type, COUNT(*) AS requests, COALESCE(SUM(quota),0) AS quota, COALESCE(SUM(prompt_tokens),0) AS prompt_tokens, COALESCE(SUM(completion_tokens),0) AS completion_tokens, COALESCE(SUM(use_time),0) AS use_time, MAX(created_at) AS last_request_at", resolved.Start, resolved.SlotSeconds).
		Group("model_name, " + group + ", bucket_index, type").Find(&buckets).Error
	if err != nil {
		return nil, err
	}
	positions := make(map[modelStatusTarget]int, len(targets))
	for _, target := range targets {
		positions[target] = len(out)
		status := ModelStatus{ModelName: target.Model, Group: target.Group, GroupName: target.Group, DisplayName: target.Model, TimeWindow: resolved.Key, TimeWindowMinutes: resolved.Minutes, GeneratedAt: resolved.End, SlotData: make([]ModelStatusSlot, resolved.SlotCount)}
		for i := range status.SlotData {
			start := resolved.Start + int64(i)*resolved.SlotSeconds
			end := start + resolved.SlotSeconds
			if end > resolved.End {
				end = resolved.End
			}
			status.SlotData[i] = ModelStatusSlot{Slot: i, StartTime: start, EndTime: end}
		}
		out = append(out, status)
	}
	useTimes := make([]int64, len(out))
	for _, row := range buckets {
		pos, ok := positions[modelStatusTarget{Group: row.GroupName, Model: row.ModelName}]
		if !ok {
			continue
		}
		status := &out[pos]
		slot := row.BucketIndex
		if slot >= len(status.SlotData) {
			slot = len(status.SlotData) - 1
		}
		if slot < 0 {
			continue
		}
		status.TotalRequests += row.Requests
		status.SlotData[slot].TotalRequests += row.Requests
		status.LastRequestAt = maxInt64(status.LastRequestAt, row.LastRequestAt)
		if row.Type == model.LogTypeConsume {
			// Preserve the existing log-type accounting. Relay outcomes and
			// 524 logging are deliberately outside this query optimization.
			status.SuccessCount += row.Requests
			status.SlotData[slot].SuccessCount += row.Requests
			status.Quota += row.Quota
			status.PromptTokens += row.PromptTokens
			status.CompletionTokens += row.CompletionTokens
			useTimes[pos] += row.UseTime
		} else {
			status.ErrorCount += row.Requests
			status.SlotData[slot].ErrorCount += row.Requests
		}
	}
	active := make([]modelStatusTarget, 0)
	for i := range out {
		s := &out[i]
		s.SuccessRate = 100
		if s.TotalRequests > 0 {
			s.SuccessRate = roundedPercent(float64(s.SuccessCount) / float64(s.TotalRequests) * 100)
			s.ErrorRate = roundedPercent(float64(s.ErrorCount) / float64(s.TotalRequests))
		}
		if s.SuccessCount > 0 {
			s.AvgUseTime = roundedPercent(float64(useTimes[i]) / float64(s.SuccessCount))
			active = append(active, targets[i])
		}
		s.CurrentStatus = modelStatusColor(s.SuccessRate, s.TotalRequests)
		s.Status = legacyModelStatus(s.CurrentStatus)
		s.Requests = s.SuccessCount
		for j := range s.SlotData {
			b := &s.SlotData[j]
			b.SuccessRate = 100
			if b.TotalRequests > 0 {
				b.SuccessRate = roundedPercent(float64(b.SuccessCount) / float64(b.TotalRequests) * 100)
			}
			b.Status = modelStatusColor(b.SuccessRate, b.TotalRequests)
		}
	}
	samples, err := readRecentStatusSamples(db, active, resolved)
	if err != nil {
		return nil, err
	}
	byTarget := make(map[modelStatusTarget][]model.Log)
	for _, sample := range samples {
		key := modelStatusTarget{Group: sample.Group, Model: sample.ModelName}
		byTarget[key] = append(byTarget[key], sample)
	}
	for target, pos := range positions {
		applyRecentModelStatusMetrics(&out[pos], byTarget[target])
	}
	return out, nil
}
