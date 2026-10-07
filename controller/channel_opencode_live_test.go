package controller

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

// Opt-in live coverage of the actual dashboard test path, including settings,
// model mapping and the default short "hi" request. Uses an isolated memory DB.
func TestOpenCodeLiveDashboardChannel(t *testing.T) {
	if os.Getenv("OPENCODE_LIVE_CHANNEL_TEST") != "1" {
		t.Skip("set OPENCODE_LIVE_CHANNEL_TEST=1 to test the live dashboard path")
	}
	key := os.Getenv("OPENCODE_LIVE_API_KEY")
	if key == "" {
		key = "public"
	}
	// Initialize the SQL dialect helpers as normal startup does, but never use
	// the host's database DSN or SQLite file for a live probe.
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousPath, previousMaster := common.SQLitePath, common.IsMasterNode
	previousSQLite, previousMySQL, previousPostgres := common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	t.Setenv("SQL_DSN", "local")
	common.SQLitePath = "file:opencode-live-dashboard?mode=memory&cache=shared"
	common.IsMasterNode = false
	common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = true, false, false
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SQLitePath, common.IsMasterNode = previousPath, previousMaster
		common.UsingSQLite, common.UsingMySQL, common.UsingPostgreSQL = previousSQLite, previousMySQL, previousPostgres
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
	})
	require.NoError(t, model.InitDB())
	db := model.DB
	model.LOG_DB = db
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.ConversationLog{}, &model.User{}, &model.Log{}))
	user := model.User{Id: 1, Username: "opencode-live-test", Group: "default", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	user.SetSetting(dto.UserSetting{AcceptUnsetRatioModel: true})
	require.NoError(t, db.Create(&user).Error)
	service.InitHttpClient()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 120
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	settings := model_setting.GetGlobalSettings()
	previous := settings.PassThroughRequestEnabled
	settings.PassThroughRequestEnabled = true
	t.Cleanup(func() { settings.PassThroughRequestEnabled = previous })
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			channel := model.Channel{
				Type: constant.ChannelTypeOpenCode, Name: "opencode-live-test", Status: common.ChannelStatusEnabled,
				Key: "unused-test-key", Models: "mimo-v2.6-flash", Group: "default",
				ModelMapping:   common.GetPointer(`{"mimo-v2.6-flash":"mimo-v2.6-flash-free"}`),
				HeaderOverride: common.GetPointer(`{"User-Agent":"Go-http-client/2.0","x-opencode-client":"other-agent","x-opencode-session":"invalid-session"}`),
			}
			channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true})
			require.NoError(t, db.Create(&channel).Error)
			channel.Key = key // Keep the supplied key out of database/SQL diagnostics.
			result := testChannel(&channel, "mimo-v2.6-flash", "", stream)
			if result.localErr != nil {
				t.Fatal(strings.ReplaceAll(result.localErr.Error(), key, "[REDACTED]"))
			}
			if result.newAPIError != nil {
				t.Fatal(strings.ReplaceAll(result.newAPIError.Error(), key, "[REDACTED]"))
			}
			t.Logf("dashboard test passed: model=mimo-v2.6-flash-free stream=%t", stream)
		})
	}
}
