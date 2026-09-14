package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Exercise the actual transaction's SQL generation for the two server dialects.
// SQLite integration tests cover execution; these dry runs need no external DB
// credentials and do not claim to replace a live MySQL/PostgreSQL integration run.
func TestDeferredBillingServerDialectSQL(t *testing.T) {
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	for name, dialect := range map[string]gorm.Dialector{
		"mysql":    mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		"postgres": postgres.New(postgres.Config{Conn: sqlDB}),
	} {
		for _, source := range []string{"wallet", "subscription"} {
			t.Run(name+"/"+source, func(t *testing.T) {
				db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
				require.NoError(t, err)
				var statements []string
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:dry_rows", func(tx *gorm.DB) {
					tx.RowsAffected = 1
					statements = append(statements, tx.Statement.SQL.String())
					switch dest := tx.Statement.Dest.(type) {
					case *int64:
						*dest = 1
					case *UserSubscription:
						*dest = UserSubscription{Id: 1, UserId: 1, AmountUsed: 100, AmountTotal: 1000}
					case *Token:
						*dest = Token{Id: 1, Key: "test-dialect-token"}
					}
				}))
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:dry_update", func(tx *gorm.DB) {
					statements = append(statements, tx.Statement.SQL.String())
					tx.RowsAffected = 1
				}))
				previous := DB
				DB = db
				t.Cleanup(func() { DB = previous })
				task := &Task{ID: 1, UserId: 1, ChannelId: 1, Status: TaskStatusSuccess, Quota: 1,
					PrivateData: TaskPrivateData{BillingSource: source, SubscriptionId: 1, TokenId: 1,
						BillingContext: &TaskBillingContext{DeferredSettlement: true, EstimatedQuota: -10}}}
				won, err := CompleteDeferredTask(task, TaskStatusInProgress, -10)
				require.NoError(t, err)
				require.True(t, won)
				require.Equal(t, -10, task.Quota)
				generated := strings.Join(statements, "\n")
				require.Contains(t, generated, "remain_quota")
				require.Contains(t, generated, "request_count")
				if source == "subscription" {
					require.Contains(t, generated, "FOR UPDATE")
					require.Contains(t, generated, "amount_used")
				}
				if name == "postgres" {
					require.Contains(t, generated, `"tasks"`)
					require.Contains(t, generated, "$1")
				} else {
					require.Contains(t, generated, "`tasks`")
					require.Contains(t, generated, "?")
				}
			})
		}
	}
}
