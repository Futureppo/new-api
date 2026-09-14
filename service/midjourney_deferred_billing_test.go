package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyDeferredBillingPersistenceAndSettlement(t *testing.T) {
	for _, source := range []string{BillingSourceWallet, BillingSourceSubscription} {
		for _, status := range []string{"SUCCESS", "FAILURE"} {
			t.Run(source+status, func(t *testing.T) {
				truncate(t)
				require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
				t.Cleanup(func() { model.DB.Where("user_id = ?", 1).Delete(&model.Midjourney{}) })
				seedUser(t, 1, 1000)
				seedToken(t, 1, 1, "negative-mj", 1000)
				seedChannel(t, 1)
				if source == BillingSourceSubscription {
					seedSubscription(t, 1, 1, 100, 20)
				}
				info := &relaycommon.RelayInfo{UserId: 1, TokenId: 1, BillingSource: source, SubscriptionId: 1, OriginModelName: "mj_imagine", PriceData: types.PriceData{UsePrice: true, ModelPrice: -0.001}}
				task := &model.Midjourney{UserId: 1, ChannelId: 1, MjId: "negative-mj-task", Status: "IN_PROGRESS", Properties: `{"upstream":"preserved"}`}
				AttachDeferredMidjourneyBilling(task, info, -500)
				require.NoError(t, task.Insert())
				require.Equal(t, 1000, getUserQuota(t, 1))
				var loaded, competing model.Midjourney
				require.NoError(t, model.DB.First(&loaded, task.Id).Error)
				require.NoError(t, model.DB.First(&competing, task.Id).Error)
				require.True(t, loaded.HasDeferredBilling())
				require.JSONEq(t, `{"upstream":"preserved"}`, loaded.Properties)
				public, err := common.Marshal(loaded)
				require.NoError(t, err)
				require.NotContains(t, string(public), "deferred_billing")
				// Upstream property replacements must not erase trusted metadata.
				loaded.Properties = `{"upstream":"updated"}`
				require.NoError(t, loaded.Update())
				loaded.Status, competing.Status = status, status
				won, err := CompleteDeferredMidjourney(&loaded, "IN_PROGRESS")
				require.NoError(t, err)
				require.True(t, won)
				won, err = CompleteDeferredMidjourney(&competing, "IN_PROGRESS")
				require.NoError(t, err)
				require.False(t, won)
				want := 1000
				if status == "SUCCESS" {
					require.Equal(t, 1500, getTokenRemainQuota(t, 1))
					if source == BillingSourceWallet {
						want = 1500
					} else {
						require.Zero(t, getSubscriptionUsed(t, 1))
					}
				} else {
					require.Equal(t, 1000, getTokenRemainQuota(t, 1))
				}
				require.Equal(t, want, getUserQuota(t, 1))
				require.Equal(t, int64(1), countLogs(t))
			})
		}
	}
}
