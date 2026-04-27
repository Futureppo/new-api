package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type siteAPIResponse[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type groupTransferResponseData struct {
	SourceGroup string `json:"source_group"`
	TargetGroup string `json:"target_group"`
	Affected    int64  `json:"affected"`
}

type groupTransferOptionsResponseData struct {
	SourceGroups []model.UserGroupCount `json:"source_groups"`
	TargetGroups []string               `json:"target_groups"`
}

type groupBalanceResponseData struct {
	Group      string `json:"group"`
	Mode       string `json:"mode"`
	Quota      int    `json:"quota"`
	Affected   int64  `json:"affected"`
	TotalDelta int64  `json:"total_delta"`
}

func setupSiteControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	initSiteColumnNames(t)

	gin.SetMode(gin.TestMode)
	common.UsingSQLite = true
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}))

	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))

	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func initSiteColumnNames(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalUsingSQLite := common.UsingSQLite
	originalUsingMySQL := common.UsingMySQL
	originalUsingPostgreSQL := common.UsingPostgreSQL
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	defer func() {
		common.IsMasterNode = originalIsMasterNode
		common.SQLitePath = originalSQLitePath
		common.UsingSQLite = originalUsingSQLite
		common.UsingMySQL = originalUsingMySQL
		common.UsingPostgreSQL = originalUsingPostgreSQL
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	}()

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.UsingSQLite = false
	common.UsingMySQL = false
	common.UsingPostgreSQL = false
	require.NoError(t, os.Setenv("SQL_DSN", "local"))

	require.NoError(t, model.InitDB())
	if model.DB != nil {
		sqlDB, err := model.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func seedSiteUser(t *testing.T, db *gorm.DB, id int, username string, group string, role int) *model.User {
	t.Helper()

	return seedSiteUserWithQuota(t, db, id, username, group, role, 0, common.UserStatusEnabled)
}

func seedSiteUserWithQuota(t *testing.T, db *gorm.DB, id int, username string, group string, role int, quota int, status int) *model.User {
	t.Helper()

	user := &model.User{
		Id:          id,
		Username:    username,
		Password:    "password123",
		DisplayName: username,
		Role:        role,
		Status:      status,
		Quota:       quota,
		Group:       group,
		AffCode:     fmt.Sprintf("aff%d", id),
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func newSiteControllerContext(t *testing.T, method string, target string, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	var requestBody *bytes.Reader
	if body != nil {
		payload, err := common.Marshal(body)
		require.NoError(t, err)
		requestBody = bytes.NewReader(payload)
	} else {
		requestBody = bytes.NewReader(nil)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, requestBody)
	if body != nil {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	ctx.Set("id", 100)
	ctx.Set("username", "root")
	return ctx, recorder
}

func decodeSiteAPIResponse[T any](t *testing.T, recorder *httptest.ResponseRecorder) siteAPIResponse[T] {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var response siteAPIResponse[T]
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestTransferGroupUsersMovesActiveUsersOnly(t *testing.T) {
	db := setupSiteControllerTestDB(t)
	seedSiteUser(t, db, 100, "root", "default", common.RoleRootUser)
	seedSiteUser(t, db, 1, "legacy-a", "legacy", common.RoleCommonUser)
	seedSiteUser(t, db, 2, "legacy-b", "legacy", common.RoleCommonUser)
	seedSiteUser(t, db, 3, "default-user", "default", common.RoleCommonUser)
	deletedUser := seedSiteUser(t, db, 4, "legacy-deleted", "legacy", common.RoleCommonUser)
	require.NoError(t, db.Delete(deletedUser).Error)

	ctx, recorder := newSiteControllerContext(t, http.MethodPost, "/api/site/group-transfer", gin.H{
		"source_group": "legacy",
		"target_group": "vip",
	})
	TransferGroupUsers(ctx)

	response := decodeSiteAPIResponse[groupTransferResponseData](t, recorder)
	require.True(t, response.Success)
	require.EqualValues(t, 2, response.Data.Affected)

	var activeLegacyCount int64
	require.NoError(t, db.Model(&model.User{}).Where("`group` = ?", "legacy").Count(&activeLegacyCount).Error)
	require.EqualValues(t, 0, activeLegacyCount)

	var activeVipCount int64
	require.NoError(t, db.Model(&model.User{}).Where("`group` = ?", "vip").Count(&activeVipCount).Error)
	require.EqualValues(t, 2, activeVipCount)

	var deleted model.User
	require.NoError(t, db.Unscoped().First(&deleted, 4).Error)
	require.Equal(t, "legacy", deleted.Group)

	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).First(&log).Error)
	require.Contains(t, log.Content, "legacy")
	require.Contains(t, log.Content, "vip")
	require.Contains(t, log.Other, "admin_info")
}

func TestGetGroupTransferOptionsReturnsActiveGroups(t *testing.T) {
	db := setupSiteControllerTestDB(t)
	seedSiteUser(t, db, 1, "legacy-a", "legacy", common.RoleCommonUser)
	seedSiteUser(t, db, 2, "legacy-b", "legacy", common.RoleCommonUser)
	seedSiteUser(t, db, 3, "vip-user", "vip", common.RoleCommonUser)
	deletedUser := seedSiteUser(t, db, 4, "legacy-deleted", "legacy", common.RoleCommonUser)
	require.NoError(t, db.Delete(deletedUser).Error)

	ctx, recorder := newSiteControllerContext(t, http.MethodGet, "/api/site/group-transfer/options", nil)
	GetGroupTransferOptions(ctx)

	response := decodeSiteAPIResponse[groupTransferOptionsResponseData](t, recorder)
	require.True(t, response.Success)
	require.ElementsMatch(t, []string{"default", "vip"}, response.Data.TargetGroups)

	counts := map[string]int64{}
	for _, sourceGroup := range response.Data.SourceGroups {
		counts[sourceGroup.Group] = sourceGroup.Count
	}
	require.EqualValues(t, 2, counts["legacy"])
	require.EqualValues(t, 1, counts["vip"])
}

func TestTransferGroupUsersRejectsSameGroup(t *testing.T) {
	_ = setupSiteControllerTestDB(t)

	ctx, recorder := newSiteControllerContext(t, http.MethodPost, "/api/site/group-transfer", gin.H{
		"source_group": "default",
		"target_group": "default",
	})
	TransferGroupUsers(ctx)

	response := decodeSiteAPIResponse[struct{}](t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "不能相同")
}

func TestTransferGroupUsersRejectsUnknownTargetGroup(t *testing.T) {
	_ = setupSiteControllerTestDB(t)

	ctx, recorder := newSiteControllerContext(t, http.MethodPost, "/api/site/group-transfer", gin.H{
		"source_group": "default",
		"target_group": "missing",
	})
	TransferGroupUsers(ctx)

	response := decodeSiteAPIResponse[struct{}](t, recorder)
	require.False(t, response.Success)
	require.Contains(t, response.Message, "目标分组不存在")
}

func TestPreviewGroupTransferCountsActiveUsersOnly(t *testing.T) {
	db := setupSiteControllerTestDB(t)
	seedSiteUser(t, db, 1, "legacy-a", "legacy", common.RoleCommonUser)
	seedSiteUser(t, db, 2, "legacy-b", "legacy", common.RoleCommonUser)
	deletedUser := seedSiteUser(t, db, 3, "legacy-deleted", "legacy", common.RoleCommonUser)
	require.NoError(t, db.Delete(deletedUser).Error)

	ctx, recorder := newSiteControllerContext(t, http.MethodGet, "/api/site/group-transfer/preview?source_group=legacy&target_group=vip", nil)
	PreviewGroupTransfer(ctx)

	response := decodeSiteAPIResponse[groupTransferResponseData](t, recorder)
	require.True(t, response.Success)
	require.EqualValues(t, 2, response.Data.Affected)
}

func TestUpdateGroupBalanceAddsActiveUsersOnly(t *testing.T) {
	db := setupSiteControllerTestDB(t)
	seedSiteUser(t, db, 100, "root", "default", common.RoleRootUser)
	seedSiteUserWithQuota(t, db, 1, "legacy-a", "legacy", common.RoleCommonUser, 100, common.UserStatusEnabled)
	seedSiteUserWithQuota(t, db, 2, "legacy-b", "legacy", common.RoleCommonUser, 200, common.UserStatusEnabled)
	deletedUser := seedSiteUserWithQuota(t, db, 3, "legacy-deleted", "legacy", common.RoleCommonUser, 300, common.UserStatusEnabled)
	require.NoError(t, db.Delete(deletedUser).Error)

	ctx, recorder := newSiteControllerContext(t, http.MethodPost, "/api/site/group-balance", gin.H{
		"group": "legacy",
		"mode":  "add",
		"quota": 50,
	})
	UpdateGroupBalance(ctx)

	response := decodeSiteAPIResponse[groupBalanceResponseData](t, recorder)
	require.True(t, response.Success)
	require.EqualValues(t, 2, response.Data.Affected)
	require.EqualValues(t, 100, response.Data.TotalDelta)
	require.Equal(t, 150, getSiteUserQuota(t, db, 1, false))
	require.Equal(t, 250, getSiteUserQuota(t, db, 2, false))
	require.Equal(t, 300, getSiteUserQuota(t, db, 3, true))

	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).First(&log).Error)
	require.Contains(t, log.Content, "legacy")
	require.Contains(t, log.Other, "total_delta")
}

func TestUpdateGroupBalanceSubtractFloorsAtZeroAndIncludesDisabled(t *testing.T) {
	db := setupSiteControllerTestDB(t)
	seedSiteUserWithQuota(t, db, 1, "legacy-a", "legacy", common.RoleCommonUser, 30, common.UserStatusEnabled)
	seedSiteUserWithQuota(t, db, 2, "legacy-disabled", "legacy", common.RoleCommonUser, 100, common.UserStatusDisabled)

	ctx, recorder := newSiteControllerContext(t, http.MethodPost, "/api/site/group-balance", gin.H{
		"group": "legacy",
		"mode":  "subtract",
		"quota": 50,
	})
	UpdateGroupBalance(ctx)

	response := decodeSiteAPIResponse[groupBalanceResponseData](t, recorder)
	require.True(t, response.Success)
	require.EqualValues(t, 2, response.Data.Affected)
	require.EqualValues(t, -80, response.Data.TotalDelta)
	require.Equal(t, 0, getSiteUserQuota(t, db, 1, false))
	require.Equal(t, 50, getSiteUserQuota(t, db, 2, false))
}

func TestUpdateGroupBalanceOverride(t *testing.T) {
	db := setupSiteControllerTestDB(t)
	seedSiteUserWithQuota(t, db, 1, "legacy-a", "legacy", common.RoleCommonUser, 20, common.UserStatusEnabled)
	seedSiteUserWithQuota(t, db, 2, "legacy-b", "legacy", common.RoleCommonUser, 60, common.UserStatusEnabled)

	ctx, recorder := newSiteControllerContext(t, http.MethodPost, "/api/site/group-balance", gin.H{
		"group": "legacy",
		"mode":  "override",
		"quota": 100,
	})
	UpdateGroupBalance(ctx)

	response := decodeSiteAPIResponse[groupBalanceResponseData](t, recorder)
	require.True(t, response.Success)
	require.EqualValues(t, 2, response.Data.Affected)
	require.EqualValues(t, 120, response.Data.TotalDelta)
	require.Equal(t, 100, getSiteUserQuota(t, db, 1, false))
	require.Equal(t, 100, getSiteUserQuota(t, db, 2, false))
}

func TestPreviewGroupBalanceMatchesExecute(t *testing.T) {
	db := setupSiteControllerTestDB(t)
	seedSiteUserWithQuota(t, db, 1, "legacy-a", "legacy", common.RoleCommonUser, 100, common.UserStatusEnabled)
	seedSiteUserWithQuota(t, db, 2, "legacy-b", "legacy", common.RoleCommonUser, 20, common.UserStatusEnabled)

	ctx, recorder := newSiteControllerContext(t, http.MethodGet, "/api/site/group-balance/preview?group=legacy&mode=subtract&quota=50", nil)
	PreviewGroupBalance(ctx)
	preview := decodeSiteAPIResponse[groupBalanceResponseData](t, recorder)
	require.True(t, preview.Success)
	require.EqualValues(t, 2, preview.Data.Affected)
	require.EqualValues(t, -70, preview.Data.TotalDelta)

	ctx, recorder = newSiteControllerContext(t, http.MethodPost, "/api/site/group-balance", gin.H{
		"group": "legacy",
		"mode":  "subtract",
		"quota": 50,
	})
	UpdateGroupBalance(ctx)
	executed := decodeSiteAPIResponse[groupBalanceResponseData](t, recorder)
	require.True(t, executed.Success)
	require.Equal(t, preview.Data, executed.Data)
}

func TestGroupBalanceRejectsInvalidParams(t *testing.T) {
	_ = setupSiteControllerTestDB(t)

	tests := []gin.H{
		{"group": "", "mode": "add", "quota": 100},
		{"group": "legacy", "mode": "invalid", "quota": 100},
		{"group": "legacy", "mode": "add", "quota": 0},
	}

	for _, body := range tests {
		ctx, recorder := newSiteControllerContext(t, http.MethodPost, "/api/site/group-balance", body)
		UpdateGroupBalance(ctx)

		response := decodeSiteAPIResponse[struct{}](t, recorder)
		require.False(t, response.Success)
	}
}

func getSiteUserQuota(t *testing.T, db *gorm.DB, userID int, unscoped bool) int {
	t.Helper()

	var user model.User
	query := db
	if unscoped {
		query = query.Unscoped()
	}
	require.NoError(t, query.First(&user, userID).Error)
	return user.Quota
}
