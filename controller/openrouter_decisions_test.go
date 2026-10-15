package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenRouterDecisionsGateway(t *testing.T) {
	response := `{"id":"dec-1","model":"vendor/mapped","answers":{"greeting":{"type":"noul","noul":0}},"usage":{"input_tokens":100,"output_tokens":10,"cost":999},"extension":false}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/proxy/alpha/decisions", r.URL.Path)
		require.Equal(t, "Bearer upstream-key", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "vendor/mapped", gjson.GetBytes(body, "model").String())
		require.True(t, gjson.GetBytes(body, "questions").IsObject())
		require.Equal(t, "false", gjson.GetBytes(body, "provider.allow_fallbacks").Raw)
		require.Equal(t, "0", gjson.GetBytes(body, "extra").Raw)
		require.False(t, gjson.GetBytes(body, "safety_identifier").Exists())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, response)
	}))
	defer upstream.Close()
	db, engine, ch := setupOpenRouterGateway(t, upstream.URL+"/proxy/v1/")
	ch.ParamOverride = common.GetPointer(`{"extra":0,"provider":{"allow_fallbacks":false}}`)
	require.NoError(t, ch.Update())
	body := `{"model":"or-test","state":{"message":"hi"},"questions":{"greeting":{"type":"noul","instructions":"Is this a greeting?"}},"provider":{"allow_fallbacks":true},"user":"private","safety_identifier":"private","stream":false}`
	w := requestMiMo(t, engine, "/v1/decisions", body, mimoGatewayKey)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.JSONEq(t, response, w.Body.String())
	assertMiMoQuota(t, db, 120)
	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeConsume).First(&log).Error)
	require.Equal(t, "or-test", log.ModelName)
	test := testChannel(ch, "or-test", string(constant.EndpointTypeOpenAIDecisions), true)
	require.NoError(t, test.localErr)
	require.Nil(t, test.newAPIError)
	require.Equal(t, 100, test.usage.PromptTokens)
}

func TestOpenRouterDecisionsBillingModes(t *testing.T) {
	for _, expression := range []bool{false, true} {
		t.Run(map[bool]string{false: "fixed", true: "expression"}[expression], func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"model":"vendor/mapped","answers":{"x":{"type":"noul","noul":0}},"usage":{"input_tokens":100,"output_tokens":10,"cost":999}}`)
			}))
			defer upstream.Close()
			db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
			quota := 1000
			if expression {
				settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
				oldMode, oldExpr := settings.BillingMode, settings.BillingExpr
				t.Cleanup(func() { settings.BillingMode, settings.BillingExpr = oldMode, oldExpr })
				settings.BillingMode = map[string]string{"or-test": "tiered_expr"}
				settings.BillingExpr = map[string]string{"or-test": `tier("base", p*6 + c*10)`}
				quota = 350
			} else {
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"or-test":0.002}`))
			}
			w := requestMiMo(t, engine, "/v1/decisions", `{"model":"or-test","state":"hi","questions":{"x":{"type":"noul","instructions":"yes?"}}}`, mimoGatewayKey)
			require.Equal(t, 200, w.Code, w.Body.String())
			assertMiMoQuota(t, db, quota)
		})
	}
}

func TestOpenRouterDecisionsFinalValidation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("invalid Decisions request reached upstream") }))
	defer upstream.Close()
	db, engine, ch := setupOpenRouterGateway(t, upstream.URL)
	for _, override := range []string{`{"stream":true}`, `{"questions":[]}`, `{"state":null}`} {
		ch.ParamOverride = common.GetPointer(override)
		require.NoError(t, ch.Update())
		w := requestMiMo(t, engine, "/v1/decisions", `{"model":"or-test","state":"hi","questions":{"x":{"type":"noul","instructions":"yes?"}}}`, mimoGatewayKey)
		require.Equal(t, 400, w.Code, w.Body.String())
		test := testChannel(ch, "or-test", string(constant.EndpointTypeOpenAIDecisions), false)
		require.Error(t, test.localErr)
	}
	assertMiMoQuota(t, db, 0)
}

func TestOpenRouterDecisionsErrorsAndZeroUsage(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
	}{
		{"zero", `{"model":"vendor/mapped","answers":{"x":{"type":"noul","noul":0}},"usage":{"input_tokens":0,"output_tokens":0}}`, 200},
		{"business error", `{"error":{"message":"bad request","code":400}}`, 400},
		{"missing usage", `{"model":"vendor/mapped","answers":{"x":{"type":"noul","noul":0}}}`, 502},
		{"negative usage", `{"model":"vendor/mapped","answers":{"x":{"type":"noul","noul":0}},"usage":{"input_tokens":-1,"output_tokens":1}}`, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, tc.response) }))
			defer upstream.Close()
			db, engine, _ := setupOpenRouterGateway(t, upstream.URL)
			w := requestMiMo(t, engine, "/v1/decisions", `{"model":"or-test","state":"hi","questions":{"x":{"type":"noul","instructions":"yes?"}}}`, mimoGatewayKey)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			assertMiMoQuota(t, db, 0)
		})
	}
}
