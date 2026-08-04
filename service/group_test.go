package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetRequestGroupCandidatesPrefersExplicitTokenGroups(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyTokenGroups, []string{"vip", "default"})

	require.Equal(t, []string{"vip", "default"}, GetRequestGroupCandidates(ctx, "default", "auto"))
}

func TestGetRequestGroupCandidatesKeepsConfiguredAutoGroups(t *testing.T) {
	originalAutoGroups := setting.AutoGroups2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(originalAutoGroups))
	})
	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["default","vip"]`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	require.Equal(t, []string{"default", "vip"}, GetRequestGroupCandidates(ctx, "default", "auto"))
}
