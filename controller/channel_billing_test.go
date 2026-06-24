package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestParseDeepSeekBalanceUSDUsesUSDDirectly(t *testing.T) {
	balance, err := parseDeepSeekBalanceUSD([]DeepSeekBalanceInfo{
		{Currency: "USD", TotalBalance: "12.34"},
	})

	require.NoError(t, err)
	require.Equal(t, 12.34, balance)
}

func TestParseDeepSeekBalanceUSDConvertsCNY(t *testing.T) {
	originalPrice := operation_setting.Price
	operation_setting.Price = 7.3
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
	})

	balance, err := parseDeepSeekBalanceUSD([]DeepSeekBalanceInfo{
		{Currency: "CNY", TotalBalance: "73"},
	})

	require.NoError(t, err)
	require.InDelta(t, 10, balance, 0.000001)
}

func TestParseDeepSeekBalanceUSDPreferUSD(t *testing.T) {
	originalPrice := operation_setting.Price
	operation_setting.Price = 7.3
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
	})

	balance, err := parseDeepSeekBalanceUSD([]DeepSeekBalanceInfo{
		{Currency: "CNY", TotalBalance: "73"},
		{Currency: " usd ", TotalBalance: "5.5"},
	})

	require.NoError(t, err)
	require.Equal(t, 5.5, balance)
}

func TestParseDeepSeekBalanceUSDReturnsErrorWithoutSupportedCurrency(t *testing.T) {
	_, err := parseDeepSeekBalanceUSD([]DeepSeekBalanceInfo{
		{Currency: "EUR", TotalBalance: "10"},
	})

	require.ErrorContains(t, err, "currency USD or CNY not found")
}
