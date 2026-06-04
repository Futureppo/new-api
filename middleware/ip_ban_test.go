package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIPBanMiddlewareBlocksWithoutAccessLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db
	require.NoError(t, db.AutoMigrate(&model.IPBan{}, &model.Log{}))
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target: "203.0.113.10",
		Reason: "abuse",
	}))
	model.InitIPBanCache()

	var logBuffer bytes.Buffer
	oldWriter := gin.DefaultWriter
	gin.DefaultWriter = &logBuffer
	t.Cleanup(func() {
		gin.DefaultWriter = oldWriter
	})

	router := gin.New()
	router.Use(IPBan())
	SetUpLogger(router)
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "该ip已被封禁，原因：abuse", recorder.Body.String())
	require.Empty(t, logBuffer.String())

	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&count).Error)
	require.EqualValues(t, 0, count)
}
