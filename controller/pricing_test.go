package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pricingResponse struct {
	Success     bool               `json:"success"`
	Data        []model.Pricing    `json:"data"`
	GroupRatio  map[string]float64 `json:"group_ratio"`
	UsableGroup map[string]string  `json:"usable_group"`
	AutoGroups  []string           `json:"auto_groups"`
}

func configurePricingDisplayGroups(t *testing.T) {
	t.Helper()

	originalGroupRatio := ratio_setting.GroupRatio2JSONString()
	originalGroupDisplay := ratio_setting.GroupDisplay2JSONString()
	originalUserUsableGroups := setting.UserUsableGroups2JSONString()
	originalAutoGroups := setting.AutoGroups2JsonString()

	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatio))
		require.NoError(t, ratio_setting.UpdateGroupDisplayByJSONString(originalGroupDisplay))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(originalUserUsableGroups))
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
		model.InvalidatePricingCache()
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"visible":1,"hidden":2,"missing":3}`))
	require.NoError(t, ratio_setting.UpdateGroupDisplayByJSONString(`{"visible":true,"hidden":false}`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"visible":"Visible group","hidden":"Hidden group","missing":"Missing group"}`))
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["visible","hidden","missing"]`))
	model.InvalidatePricingCache()
}

func decodePricingResponse(t *testing.T, recorder *httptest.ResponseRecorder) pricingResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload pricingResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload
}

func TestGetPricingFiltersGroupsByDisplaySetting(t *testing.T) {
	configurePricingDisplayGroups(t)

	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "visible", Model: "zz-visible-model", ChannelId: 1, Enabled: true},
		{Group: "hidden", Model: "zz-hidden-model", ChannelId: 1, Enabled: true},
		{Group: "missing", Model: "zz-missing-model", ChannelId: 1, Enabled: true},
		{Group: "visible", Model: "zz-mixed-model", ChannelId: 1, Enabled: true},
		{Group: "hidden", Model: "zz-mixed-model", ChannelId: 2, Enabled: true},
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)

	GetPricing(ctx)

	payload := decodePricingResponse(t, recorder)
	require.Equal(t, map[string]string{"visible": "Visible group"}, payload.UsableGroup)
	require.Equal(t, map[string]float64{"visible": 1}, payload.GroupRatio)
	require.Equal(t, []string{"visible"}, payload.AutoGroups)

	pricingByName := pricingByModelName(payload.Data)
	require.Contains(t, pricingByName, "zz-visible-model")
	require.Contains(t, pricingByName, "zz-mixed-model")
	require.NotContains(t, pricingByName, "zz-hidden-model")
	require.NotContains(t, pricingByName, "zz-missing-model")
	require.Equal(t, []string{"visible"}, pricingByName["zz-mixed-model"].EnableGroup)
}
