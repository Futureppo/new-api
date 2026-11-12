package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/enhancement"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSelfUsageStatsUsesAuthenticatedIdentity(t *testing.T) {
	db := setupEnhancementOptionControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.QuotaData{}))
	hour := time.Now().Truncate(time.Hour).Unix()
	require.NoError(t, db.Create(&[]model.QuotaData{
		{UserID: 1, Username: "one", ModelName: "a", CreatedAt: hour, Count: 3, Quota: 10},
		{UserID: 2, Username: "two", ModelName: "b", CreatedAt: hour, Count: 900, Quota: 2000},
	}).Error)
	router := gin.New()
	router.GET("/self", func(c *gin.Context) { c.Set("id", 1); GetSelfUsageStats(c) })
	router.GET("/anonymous", GetSelfUsageStats)
	for _, target := range []string{"/self", "/self?user_id=2", "/self?user_id=0", "/self?user_id=-1"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusOK, recorder.Code)
		var body struct {
			Success bool                           `json:"success"`
			Data    enhancement.UsageStatsSnapshot `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
		require.True(t, body.Success)
		require.Equal(t, 1, body.Data.UserID)
		require.EqualValues(t, 3, body.Data.Totals.Requests)
		require.Len(t, body.Data.Users, 1)
		require.Equal(t, 1, body.Data.Users[0].UserID)
		require.Len(t, body.Data.Models, 1)
		require.Equal(t, "a", body.Data.Models[0].ModelName)
	}
	for _, tc := range []struct {
		path string
		code int
	}{{"/anonymous", http.StatusUnauthorized}, {"/self?period=all", http.StatusBadRequest}} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
		require.Equal(t, tc.code, recorder.Code)
	}
}
