package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDecisionsPricingUsesOneIndependentSnapshot(t *testing.T) {
	oldRatios, oldPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error { saved[key] = value; return nil }))
	clearBilling := map[string]string{"billing_setting.billing_mode": "null", "billing_setting.billing_expr": "null"}
	require.NoError(t, config.GlobalConfig.LoadFromDB(clearBilling))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
		require.NoError(t, config.GlobalConfig.LoadFromDB(clearBilling))
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/decisions", nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeDecisions, ClientModelName: "pricing-client", OriginModelName: "pricing-client", UserGroup: "default", UsingGroup: "default"}
	info.ChannelMeta = &relaycommon.ChannelMeta{UpstreamModelName: "upstream"}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"pricing-client":9,"pricing-client-openai-decisions":0.25}`))
	price, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 250, price.QuotaToPreConsume)
	require.Equal(t, 0.25, price.ModelRatio)
	require.Equal(t, "pricing-client", info.OriginModelName)
	require.Equal(t, "upstream", info.UpstreamModelName)
	info.RelayMode = relayconstant.RelayModeResponses
	ordinary, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 9.0, ordinary.ModelRatio)
	info.RelayMode = relayconstant.RelayModeDecisions
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"pricing-client-openai-decisions":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"pricing-client-openai-decisions":"tier(\"decision\", p * 0.2)"}`,
	}))
	price, err = ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, int(1000*0.2/1e6*common.QuotaPerUnit), price.QuotaToPreConsume)
	require.Equal(t, "pricing-client-openai-decisions", info.TieredBillingSnapshot.ModelName)
	// An administrator changing the price mid-flight must not change settlement.
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_expr": `{"pricing-client-openai-decisions":"tier(\"changed\", p * 20)"}`,
	}))
	ok, quota, result := service.TryTieredSettle(info, service.BuildTieredTokenParams(&dto.Usage{PromptTokens: 2000}, false, nil))
	require.True(t, ok)
	require.NotNil(t, result)
	require.Equal(t, int(2000*0.2/1e6*common.QuotaPerUnit), quota)
}
