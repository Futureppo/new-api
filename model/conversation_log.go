package model

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

type ConversationLog struct {
	Id                   int    `json:"id" gorm:"index:idx_conversation_logs_created_id,priority:2"`
	CreatedAt            int64  `json:"created_at" gorm:"bigint;index:idx_conversation_logs_created_id,priority:1"`
	RequestId            string `json:"request_id" gorm:"type:varchar(64);index;default:''"`
	UserId               int    `json:"user_id" gorm:"index"`
	Username             string `json:"username" gorm:"index;default:''"`
	TokenId              int    `json:"token_id" gorm:"index"`
	TokenName            string `json:"token_name" gorm:"index;default:''"`
	ChannelId            int    `json:"channel_id" gorm:"index"`
	Group                string `json:"group" gorm:"column:group;index;default:''"`
	ModelName            string `json:"model_name" gorm:"index;default:''"`
	UpstreamModelName    string `json:"upstream_model_name" gorm:"index;default:''"`
	RelayFormat          string `json:"relay_format" gorm:"type:varchar(64);default:''"`
	FinalRequestFormat   string `json:"final_request_format" gorm:"type:varchar(64);default:''"`
	RequestPath          string `json:"request_path" gorm:"type:varchar(255);default:''"`
	IsStream             bool   `json:"is_stream"`
	StatusCode           int    `json:"status_code" gorm:"default:200"`
	ClientRequestBody    string `json:"client_request_body,omitempty" gorm:"type:text"`
	UpstreamRequestBody  string `json:"upstream_request_body,omitempty" gorm:"type:text"`
	UpstreamResponseBody string `json:"upstream_response_body,omitempty" gorm:"type:text"`
	ClientResponseBody   string `json:"client_response_body,omitempty" gorm:"type:text"`
	DerivedAssistantText string `json:"derived_assistant_text,omitempty" gorm:"type:text"`
	DerivedToolCalls     string `json:"derived_tool_calls,omitempty" gorm:"type:text"`
	Metadata             string `json:"metadata,omitempty" gorm:"type:text"`
	StorageBytes         int64  `json:"storage_bytes" gorm:"bigint;index"`
	ExportedAt           int64  `json:"exported_at" gorm:"bigint;index;default:0"`
	ExportBatchId        string `json:"export_batch_id" gorm:"type:varchar(64);index;default:''"`
	DeletedAfterExport   bool   `json:"deleted_after_export" gorm:"default:false"`
}

type ConversationLogQuery struct {
	StartTime int64
	EndTime   int64
	UserId    int
	Username  string
	TokenName string
	ModelName string
	ChannelId int
	Group     string
	RequestId string
	Exported  *bool
}

type ConversationLogSummary struct {
	StorageBytes      int64 `json:"storage_bytes"`
	RecordCount       int64 `json:"record_count"`
	ExportedCount     int64 `json:"exported_count"`
	EarliestCreatedAt int64 `json:"earliest_created_at"`
	LatestCreatedAt   int64 `json:"latest_created_at"`
}

type ConversationLogExportSummary struct {
	RecordCount       int64 `json:"record_count"`
	StorageBytes      int64 `json:"storage_bytes"`
	EarliestCreatedAt int64 `json:"earliest_created_at"`
	LatestCreatedAt   int64 `json:"latest_created_at"`
}

func applyConversationLogQuery(db *gorm.DB, query ConversationLogQuery) *gorm.DB {
	if query.StartTime > 0 {
		db = db.Where("created_at >= ?", query.StartTime)
	}
	if query.EndTime > 0 {
		db = db.Where("created_at <= ?", query.EndTime)
	}
	if query.UserId > 0 {
		db = db.Where("user_id = ?", query.UserId)
	}
	if query.Username != "" {
		db = db.Where("username = ?", query.Username)
	}
	if query.TokenName != "" {
		db = db.Where("token_name = ?", query.TokenName)
	}
	if query.ModelName != "" {
		db = db.Where("model_name = ?", query.ModelName)
	}
	if query.ChannelId > 0 {
		db = db.Where("channel_id = ?", query.ChannelId)
	}
	if query.Group != "" {
		db = db.Where(logGroupCol+" = ?", query.Group)
	}
	if query.RequestId != "" {
		db = db.Where("request_id = ?", query.RequestId)
	}
	if query.Exported != nil {
		if *query.Exported {
			db = db.Where("exported_at > ?", 0)
		} else {
			db = db.Where("exported_at = ?", 0)
		}
	}
	return db
}

func conversationLogDBWithContext(ctx context.Context) *gorm.DB {
	if ctx == nil {
		return LOG_DB
	}
	return LOG_DB.WithContext(ctx)
}

func CreateConversationLog(log *ConversationLog) error {
	return LOG_DB.Create(log).Error
}

func GetConversationLogByID(id int) (*ConversationLog, error) {
	var log ConversationLog
	if err := LOG_DB.First(&log, id).Error; err != nil {
		return nil, err
	}
	return &log, nil
}

func GetConversationLogsByIDs(ids []int) ([]*ConversationLog, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var logs []*ConversationLog
	err := LOG_DB.Where("id IN ?", ids).Order("id asc").Find(&logs).Error
	return logs, err
}

func GetConversationLogs(query ConversationLogQuery, startIdx int, num int) ([]*ConversationLog, int64, error) {
	var total int64
	base := applyConversationLogQuery(LOG_DB.Model(&ConversationLog{}), query)
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []*ConversationLog
	err := applyConversationLogQuery(LOG_DB.Model(&ConversationLog{}), query).
		Select("id, created_at, request_id, user_id, username, token_id, token_name, channel_id, " + logGroupCol + ", model_name, upstream_model_name, relay_format, final_request_format, request_path, is_stream, status_code, storage_bytes, exported_at, export_batch_id, deleted_after_export").
		Order("created_at desc, id desc").
		Offset(startIdx).
		Limit(num).
		Find(&logs).Error
	return logs, total, err
}

func GetConversationLogSummary() (ConversationLogSummary, error) {
	summary := ConversationLogSummary{}
	var sum sql.NullInt64
	if err := LOG_DB.Model(&ConversationLog{}).Select("COALESCE(SUM(storage_bytes), 0)").Scan(&sum).Error; err != nil {
		return summary, err
	}
	summary.StorageBytes = sum.Int64
	if err := LOG_DB.Model(&ConversationLog{}).Count(&summary.RecordCount).Error; err != nil {
		return summary, err
	}
	if err := LOG_DB.Model(&ConversationLog{}).Where("exported_at > ?", 0).Count(&summary.ExportedCount).Error; err != nil {
		return summary, err
	}
	var bounds struct {
		EarliestCreatedAt sql.NullInt64 `gorm:"column:earliest_created_at"`
		LatestCreatedAt   sql.NullInt64 `gorm:"column:latest_created_at"`
	}
	if err := LOG_DB.Model(&ConversationLog{}).
		Select("COALESCE(MIN(created_at), 0) AS earliest_created_at, COALESCE(MAX(created_at), 0) AS latest_created_at").
		Scan(&bounds).Error; err != nil {
		return summary, err
	}
	summary.EarliestCreatedAt = bounds.EarliestCreatedAt.Int64
	summary.LatestCreatedAt = bounds.LatestCreatedAt.Int64
	return summary, nil
}

func GetConversationLogExportSummary(query ConversationLogQuery) (ConversationLogExportSummary, error) {
	summary := ConversationLogExportSummary{}
	base := applyConversationLogQuery(LOG_DB.Model(&ConversationLog{}), query)
	if err := base.Count(&summary.RecordCount).Error; err != nil {
		return summary, err
	}
	var aggregate struct {
		StorageBytes      sql.NullInt64 `gorm:"column:storage_bytes"`
		EarliestCreatedAt sql.NullInt64 `gorm:"column:earliest_created_at"`
		LatestCreatedAt   sql.NullInt64 `gorm:"column:latest_created_at"`
	}
	err := applyConversationLogQuery(LOG_DB.Model(&ConversationLog{}), query).
		Select("COALESCE(SUM(storage_bytes), 0) AS storage_bytes, COALESCE(MIN(created_at), 0) AS earliest_created_at, COALESCE(MAX(created_at), 0) AS latest_created_at").
		Scan(&aggregate).Error
	if err != nil {
		return summary, err
	}
	summary.StorageBytes = aggregate.StorageBytes.Int64
	summary.EarliestCreatedAt = aggregate.EarliestCreatedAt.Int64
	summary.LatestCreatedAt = aggregate.LatestCreatedAt.Int64
	return summary, nil
}

func ForEachConversationLog(ctx context.Context, query ConversationLogQuery, batchSize int, fn func([]*ConversationLog) error) error {
	if batchSize <= 0 {
		batchSize = 100
	}
	lastID := 0
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		var logs []*ConversationLog
		err := applyConversationLogQuery(conversationLogDBWithContext(ctx).Model(&ConversationLog{}), query).
			Where("id > ?", lastID).
			Order("id asc").
			Limit(batchSize).
			Find(&logs).Error
		if err != nil {
			return err
		}
		if len(logs) == 0 {
			return nil
		}
		if err := fn(logs); err != nil {
			return err
		}
		lastID = logs[len(logs)-1].Id
	}
}

func MarkConversationLogsExported(ids []int, batchID string, exportedAt int64) error {
	if len(ids) == 0 {
		return nil
	}
	return LOG_DB.Model(&ConversationLog{}).
		Where("id IN ?", ids).
		Updates(map[string]interface{}{
			"exported_at":     exportedAt,
			"export_batch_id": batchID,
		}).Error
}

func DeleteConversationLogsByIDs(ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := LOG_DB.Where("id IN ?", ids).Delete(&ConversationLog{})
	return result.RowsAffected, result.Error
}

func DeleteConversationLogsByQuery(ctx context.Context, query ConversationLogQuery, batchSize int) (int64, error) {
	var total int64
	err := ForEachConversationLog(ctx, query, batchSize, func(logs []*ConversationLog) error {
		ids := make([]int, 0, len(logs))
		for _, log := range logs {
			ids = append(ids, log.Id)
		}
		rows, err := DeleteConversationLogsByIDs(ids)
		if err != nil {
			return err
		}
		total += rows
		return nil
	})
	return total, err
}

func DeleteConversationLogsOlderThan(ctx context.Context, cutoffTimestamp int64, batchSize int) (int64, error) {
	if cutoffTimestamp <= 0 {
		return 0, nil
	}
	return DeleteConversationLogsByQuery(ctx, ConversationLogQuery{EndTime: cutoffTimestamp}, batchSize)
}

func TrimConversationLogsByStorageLimit(ctx context.Context, maxBytes int64, batchSize int) (int64, error) {
	if maxBytes <= 0 {
		return 0, nil
	}
	summary, err := GetConversationLogSummary()
	if err != nil {
		return 0, err
	}
	if summary.StorageBytes <= maxBytes {
		return 0, nil
	}
	needFree := summary.StorageBytes - maxBytes
	var deleted int64

	deleteBatch := func(exportedOnly bool) error {
		for needFree > 0 {
			if ctx != nil {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			var logs []*ConversationLog
			db := conversationLogDBWithContext(ctx).Model(&ConversationLog{}).
				Select("id, storage_bytes").
				Limit(batchSize)
			if exportedOnly {
				db = db.Where("exported_at > ?", 0).Order("exported_at asc, created_at asc, id asc")
			} else {
				db = db.Order("created_at asc, id asc")
			}
			if err := db.Find(&logs).Error; err != nil {
				return err
			}
			if len(logs) == 0 {
				return nil
			}
			ids := make([]int, 0, len(logs))
			var freed int64
			for _, log := range logs {
				ids = append(ids, log.Id)
				freed += log.StorageBytes
			}
			rows, err := DeleteConversationLogsByIDs(ids)
			if err != nil {
				return err
			}
			deleted += rows
			if freed > 0 {
				needFree -= freed
			}
		}
		return nil
	}

	if err := deleteBatch(true); err != nil {
		return deleted, err
	}
	if needFree > 0 {
		if err := deleteBatch(false); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}
