package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIPBanMiddlewareBlocksWithoutAccessLog(t *testing.T) {
	setupIPBanMiddlewareTestDB(t)
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

func TestIPBanMiddlewareRendersBrowserPage(t *testing.T) {
	setupIPBanMiddlewareTestDB(t)
	require.NoError(t, appI18n.Init())
	expiresAt := common.GetTimestamp() + 3600
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target:    "203.0.113.10",
		Reason:    `abuse <script>alert("xss")</script>`,
		ExpiresAt: expiresAt,
	}))
	model.InitIPBanCache()

	router := gin.New()
	router.Use(I18n())
	router.Use(IPBan())
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/html")
	require.Equal(t, "zh-CN", recorder.Header().Get("Content-Language"))
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
	require.Contains(t, recorder.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'")
	require.Contains(t, recorder.Body.String(), "访问已被限制")
	require.Contains(t, recorder.Body.String(), "203.0.113.10")
	require.Contains(t, recorder.Body.String(), "解除时间")
	require.Contains(t, recorder.Body.String(), fmt.Sprintf(`data-expires-at="%d"`, expiresAt))
	require.NotContains(t, recorder.Body.String(), `<script>alert("xss")</script>`)
	require.Contains(t, recorder.Body.String(), `&lt;script&gt;alert(&#34;xss&#34;)&lt;/script&gt;`)
}

func TestIPBanMiddlewareRendersPermanentRestriction(t *testing.T) {
	setupIPBanMiddlewareTestDB(t)
	require.NoError(t, appI18n.Init())
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target: "203.0.113.10",
		Reason: "policy violation",
	}))
	model.InitIPBanCache()

	router := gin.New()
	router.Use(I18n())
	router.Use(IPBan())
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "en")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "No automatic unblocking time is set")
	require.NotContains(t, recorder.Body.String(), "data-expires-at=")
}

func TestIPBanMiddlewareAllowsConfiguredSameOriginBackground(t *testing.T) {
	setupIPBanMiddlewareTestDB(t)
	require.NoError(t, appI18n.Init())
	restoreSiteBackgroundSettings(t)

	backgroundSettings := system_setting.SiteBackgroundSettings{
		Enabled:        true,
		Fit:            system_setting.SiteBackgroundFitCover,
		OverlayOpacity: 25,
		GlassEnabled:   true,
		GlassOpacity:   72,
		Sources: []system_setting.SiteBackgroundSource{
			{
				Type:    system_setting.SiteBackgroundSourceImageURL,
				URL:     "/wallpaper.jpg",
				Enabled: true,
				Weight:  1,
			},
		},
	}
	settingsJSON, err := common.Marshal(backgroundSettings)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"site_background.config": string(settingsJSON),
	}))
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target: "203.0.113.10",
		Reason: "abuse",
	}))
	model.InitIPBanCache()

	router := gin.New()
	router.Use(I18n())
	router.Use(IPBan())
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	router.GET("/wallpaper.jpg", func(c *gin.Context) {
		c.Data(http.StatusOK, "image/jpeg", []byte("image"))
	})
	router.GET("/private", func(c *gin.Context) {
		c.String(http.StatusOK, "private")
	})

	pageRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	pageRequest.RemoteAddr = "203.0.113.10:1234"
	pageRequest.Header.Set("Accept", "text/html")
	pageRecorder := httptest.NewRecorder()
	router.ServeHTTP(pageRecorder, pageRequest)
	require.Equal(t, http.StatusForbidden, pageRecorder.Code)
	require.Contains(t, pageRecorder.Body.String(), `/wallpaper.jpg?_ip_ban_background=1`)

	backgroundRequest := httptest.NewRequest(http.MethodGet, "/wallpaper.jpg?_ip_ban_background=1", nil)
	backgroundRequest.RemoteAddr = "203.0.113.10:1234"
	backgroundRequest.Header.Set("Accept", "image/avif,image/webp,image/*,*/*")
	backgroundRecorder := httptest.NewRecorder()
	router.ServeHTTP(backgroundRecorder, backgroundRequest)
	require.Equal(t, http.StatusOK, backgroundRecorder.Code)
	require.Equal(t, "image", backgroundRecorder.Body.String())

	blockedRequest := httptest.NewRequest(http.MethodGet, "/wallpaper.jpg", nil)
	blockedRequest.RemoteAddr = "203.0.113.10:1234"
	blockedRequest.Header.Set("Accept", "image/*")
	blockedRecorder := httptest.NewRecorder()
	router.ServeHTTP(blockedRecorder, blockedRequest)
	require.Equal(t, http.StatusForbidden, blockedRecorder.Code)

	privateRequest := httptest.NewRequest(http.MethodGet, "/private?_ip_ban_background=1", nil)
	privateRequest.RemoteAddr = "203.0.113.10:1234"
	privateRequest.Header.Set("Accept", "application/json")
	privateRecorder := httptest.NewRecorder()
	router.ServeHTTP(privateRecorder, privateRequest)
	require.Equal(t, http.StatusForbidden, privateRecorder.Code)
}

func restoreSiteBackgroundSettings(t *testing.T) {
	t.Helper()
	original, err := common.Marshal(system_setting.GetSiteBackgroundSettings())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
			"site_background.config": string(original),
		}))
	})
}

func TestShouldRenderIPBanPage(t *testing.T) {
	tests := []struct {
		name   string
		method string
		accept string
		want   bool
	}{
		{name: "browser get", method: http.MethodGet, accept: "text/html,application/xhtml+xml", want: true},
		{name: "browser head", method: http.MethodHead, accept: "text/html", want: true},
		{name: "api get", method: http.MethodGet, accept: "application/json", want: false},
		{name: "api post", method: http.MethodPost, accept: "text/html", want: false},
		{name: "empty accept", method: http.MethodGet, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(test.method, "/", nil)
			c.Request.Header.Set("Accept", test.accept)
			require.Equal(t, test.want, shouldRenderIPBanPage(c))
		})
	}
}

func TestIPBanMiddlewareAutoBansCommonUserFromToken(t *testing.T) {
	db := setupIPBanMiddlewareTestDB(t)
	user := createIPBanMiddlewareUser(t, common.RoleCommonUser)
	createIPBanMiddlewareToken(t, user.Id, "autobantoken")
	require.NoError(t, model.CreateIPBan(&model.IPBan{
		Target:      "203.0.113.10",
		Reason:      "abuse from banned ip",
		AutoBanUser: true,
	}))
	model.InitIPBanCache()

	router := gin.New()
	router.Use(IPBan())
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("Authorization", "Bearer sk-autobantoken")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	var updated model.User
	require.NoError(t, db.First(&updated, "id = ?", user.Id).Error)
	require.Equal(t, common.UserStatusDisabled, updated.Status)
	require.Equal(t, "abuse from banned ip", updated.DisableReason)
}

func TestIPBanMiddlewareDoesNotAutoBanPrivilegedUsers(t *testing.T) {
	for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			db := setupIPBanMiddlewareTestDB(t)
			user := createIPBanMiddlewareUser(t, role)
			createIPBanMiddlewareToken(t, user.Id, "privilegedtoken")
			require.NoError(t, model.CreateIPBan(&model.IPBan{
				Target:      "203.0.113.10",
				Reason:      "privileged request",
				AutoBanUser: true,
			}))
			model.InitIPBanCache()

			router := gin.New()
			router.Use(IPBan())
			router.GET("/", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "203.0.113.10:1234"
			req.Header.Set("Authorization", "Bearer sk-privilegedtoken")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusForbidden, recorder.Code)
			var updated model.User
			require.NoError(t, db.First(&updated, "id = ?", user.Id).Error)
			require.Equal(t, common.UserStatusEnabled, updated.Status)
			require.Empty(t, updated.DisableReason)
		})
	}
}

func setupIPBanMiddlewareTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
	})
	require.NoError(t, db.AutoMigrate(&model.IPBan{}, &model.Log{}, &model.User{}, &model.Token{}))
	return db
}

func createIPBanMiddlewareUser(t *testing.T, role int) model.User {
	t.Helper()
	user := model.User{
		Username: "user-" + strconv.Itoa(role),
		Password: "password",
		Role:     role,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	return user
}

func createIPBanMiddlewareToken(t *testing.T, userId int, key string) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         userId,
		Key:            key,
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)
}
