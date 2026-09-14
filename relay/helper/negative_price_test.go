package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNegativePricesKeepSignedCostsWithoutNegativeReservations(t *testing.T) {
	prices, ratios := ratio_setting.ModelPrice2JSONString(), ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(prices))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(ratios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"negative-call":-1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"negative-token":-1}`))
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, name := range []string{"negative-call", "negative-token"} {
		info := &relaycommon.RelayInfo{OriginModelName: name, UsingGroup: "default", UserGroup: "default"}
		price, err := ModelPriceHelper(ctx, info, 100, &types.TokenCountMeta{})
		require.NoError(t, err)
		require.Zero(t, price.QuotaToPreConsume)
		require.False(t, price.FreeModel)
		call, err := ModelPriceHelperPerCall(ctx, info)
		require.NoError(t, err)
		if name == "negative-call" {
			require.True(t, price.UsePrice)
			require.Equal(t, float64(-1), price.ModelPrice, "-1 is a valid configured price, not a missing-price sentinel")
			require.Equal(t, -int(common.QuotaPerUnit), call.Quota)
		} else {
			require.False(t, price.UsePrice)
			require.Equal(t, float64(-1), price.ModelRatio)
			require.Negative(t, call.Quota)
		}
	}
}
