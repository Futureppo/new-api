package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type userModelsResponse struct {
	Success bool     `json:"success"`
	Message string   `json:"message"`
	Data    []string `json:"data"`
}

func configureUserModelGroups(t *testing.T) {
	t.Helper()
	require.NoError(t, i18n.Init())

	originalUsableGroups := setting.UserUsableGroups2JSONString()
	originalAutoGroups := setting.AutoGroups2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
	})

	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","auto":"Auto"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))
}

func seedUserModels(t *testing.T) int {
	t.Helper()

	return seedUserModelsWithAbilities(t, []model.Ability{
		{Group: "default", Model: "default-only", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "shared", ChannelId: 2, Enabled: true},
		{Group: "vip", Model: "vip-only", ChannelId: 3, Enabled: true},
		{Group: "vip", Model: "shared", ChannelId: 4, Enabled: true},
		{Group: "hidden", Model: "hidden-only", ChannelId: 5, Enabled: true},
		{Group: "default", Model: "disabled", ChannelId: 6, Enabled: false},
	})
}

func seedUserModelsWithAbilities(t *testing.T, abilities []model.Ability) int {
	t.Helper()

	db := setupModelListControllerTestDB(t)
	user := model.User{Username: "playground-user", Group: "default", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	if len(abilities) > 0 {
		require.NoError(t, db.Create(&abilities).Error)
	}
	return user.Id
}

func requestUserModels(t *testing.T, userID int, target string) (*httptest.ResponseRecorder, userModelsResponse) {
	t.Helper()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	ctx.Set("id", userID)

	GetUserModels(ctx)

	var payload userModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	sort.Strings(payload.Data)
	return recorder, payload
}

func TestGetUserModelsFiltersByRequestedGroup(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModels(t)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?group=vip")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, payload.Success)
	require.Equal(t, []string{"shared", "vip-only"}, payload.Data)
}

func TestGetUserModelsWithoutGroupReturnsUsableGroupUnion(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModels(t)

	recorder, payload := requestUserModels(t, userID, "/api/user/models")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, payload.Success)
	require.Equal(t, []string{"default-only", "shared", "vip-only"}, payload.Data)
}

func TestGetUserModelsAutoReturnsAutoGroupUnion(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModels(t)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?group=auto")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, payload.Success)
	require.Equal(t, []string{"default-only", "shared", "vip-only"}, payload.Data)
}

func TestGetUserModelsRejectsUnavailableGroup(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModels(t)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?group=hidden")

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.False(t, payload.Success)
	require.Empty(t, payload.Data)
}

func TestGetUserModelsReturnsSelectedGroupUnion(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModels(t)
	groups := url.QueryEscape(`["vip","default","vip"]`)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?groups="+groups)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, payload.Success)
	require.Equal(t, []string{"default-only", "shared", "vip-only"}, payload.Data)
}

func TestGetUserModelsReturnsEmptyArrayForGroupWithoutModels(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModelsWithAbilities(t, nil)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?group=default")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, payload.Success)
	require.NotNil(t, payload.Data)
	require.Empty(t, payload.Data)
}

func TestGetUserModelsReturnsSelectedGroupUnionWhenOneGroupHasNoModels(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModelsWithAbilities(t, []model.Ability{
		{Group: "vip", Model: "vip-only", ChannelId: 1, Enabled: true},
	})
	groups := url.QueryEscape(`["default","vip"]`)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?groups="+groups)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, payload.Success)
	require.Equal(t, []string{"vip-only"}, payload.Data)
}

func TestGetUserModelsExplicitEmptyGroupsUsesUserGroup(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModels(t)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?groups=%5B%5D")

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, payload.Success)
	require.Equal(t, []string{"default-only", "shared"}, payload.Data)
}

func TestGetUserModelsRejectsAutoMixedWithOtherGroups(t *testing.T) {
	configureUserModelGroups(t)
	userID := seedUserModels(t)
	groups := url.QueryEscape(`["auto","vip"]`)

	recorder, payload := requestUserModels(t, userID, "/api/user/models?groups="+groups)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.False(t, payload.Success)
}
