package controller

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

type namedDecisionChannel struct {
	name, model, path string
	typ               int
}

var namedDecisionChannels = []namedDecisionChannel{
	{"OpenRouter", "typesafe/jev-1.14", "/alpha/decisions", constant.ChannelTypeOpenRouter},
	{"TypeSafe", "jev-1.13.0", "/v1/systemone", constant.ChannelTypeTypeSafe},
	{"Qianwen", constant.QianwenDecisionModel, "/compatible-mode/v1/systemone", constant.ChannelTypeQianwenAI},
}

var namedDecisionPaths = []string{"/v1/systemone", "/v1/decisions"}

func setupNamedDecisionsGateway(t *testing.T, tc namedDecisionChannel, base string) (*gorm.DB, *gin.Engine, *model.Channel) {
	t.Helper()
	db, engine, ch := setupTypeSafeGateway(t, base, "decision-key")
	engine.POST("/v1/decisions", func(c *gin.Context) { Relay(c, types.RelayFormatOpenAIDecisions) })
	ch.Type, ch.Models, ch.Name = tc.typ, "decision-alias", tc.name
	ch.ModelMapping = common.GetPointer(fmt.Sprintf(`{"decision-alias":"%s"}`, tc.model))
	require.NoError(t, ch.Update())
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"decision-alias":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"decision-alias":2}`))
	return db, engine, ch
}

func namedDecisionBody(state string) string {
	return `{"model":"decision-alias","state":` + state + `,"questions":` + gjson.Get(typeSafeTestBody, "questions").Raw +
		`,"provider":{"allow_fallbacks":false},"trace":{"generation_name":"client"},"session_id":"session","stream":false,"extra":{"zero":0,"flag":false,"nil":null,"large":9007199254740993}}`
}

func namedDecisionResponse(tc namedDecisionChannel) string {
	body := strings.ReplaceAll(typeSafeTestResponse, "jev-1.13.0", tc.model)
	usage := `{"input_tokens":100,"output_tokens":10,"cost":999}`
	if tc.typ == constant.ChannelTypeQianwenAI {
		usage = `{"input_tokens":100}`
	}
	return strings.Replace(body, gjson.Get(body, "usage").Raw, usage, 1)
}

func TestNamedDecisionsGatewayMatrix(t *testing.T) {
	for _, tc := range namedDecisionChannels {
		for _, path := range namedDecisionPaths {
			for _, state := range []string{`"Help"`, `{"text":"Help","zero":0,"flag":false}`, `["Help",{"large":9007199254740993}]`} {
				t.Run(tc.name+path+state, func(t *testing.T) {
					response := namedDecisionResponse(tc)
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						require.Equal(t, "/prefix"+tc.path, r.URL.Path)
						require.Equal(t, "Bearer decision-key", r.Header.Get("Authorization"))
						body, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						require.Equal(t, tc.model, gjson.GetBytes(body, "model").String())
						require.JSONEq(t, state, gjson.GetBytes(body, "state").Raw)
						require.JSONEq(t, gjson.Get(typeSafeTestBody, "questions").Raw, gjson.GetBytes(body, "questions").Raw)
						require.Equal(t, "9007199254740993", gjson.GetBytes(body, "extra.large").Raw)
						require.Equal(t, "false", gjson.GetBytes(body, "extra.flag").Raw)
						require.Equal(t, "0", gjson.GetBytes(body, "extra.zero").Raw)
						require.Equal(t, "null", gjson.GetBytes(body, "extra.nil").Raw)
						require.Equal(t, "false", gjson.GetBytes(body, "stream").Raw)
						require.Equal(t, "0", gjson.GetBytes(body, "threshold").Raw)
						for _, field := range []string{"provider", "trace", "session_id"} {
							require.Equal(t, tc.typ == constant.ChannelTypeOpenRouter, gjson.GetBytes(body, field).Exists(), field)
						}
						if tc.typ == constant.ChannelTypeOpenRouter {
							require.Equal(t, "false", gjson.GetBytes(body, "provider.allow_fallbacks").Raw)
							require.Equal(t, "override", gjson.GetBytes(body, "trace.generation_name").String())
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, response)
					}))
					defer upstream.Close()
					db, engine, ch := setupNamedDecisionsGateway(t, tc, upstream.URL+"/prefix")
					ch.ParamOverride = common.GetPointer(`{"threshold":0,"trace":{"generation_name":"override"}}`)
					require.NoError(t, ch.Update())
					w := requestTypeSafe(t, engine, path, namedDecisionBody(state), typeSafeGatewayKey)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.JSONEq(t, response, w.Body.String())
					output := 10
					if tc.typ == constant.ChannelTypeQianwenAI {
						output = 0
						require.False(t, gjson.Get(w.Body.String(), "usage.output_tokens").Exists())
					}
					assertTypeSafeQuota(t, db, 100+output*2)
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1)
					require.Equal(t, "decision-alias", logs[0].ModelName)
					require.Equal(t, output, logs[0].CompletionTokens)
					require.Equal(t, path, gjson.Get(logs[0].Other, "request_path").String())
					require.NotContains(t, logs[0].Other, "-openai-decisions")
				})
			}
		}
	}
}

func TestNamedDecisionsBillingModes(t *testing.T) {
	for _, tc := range namedDecisionChannels {
		for _, mode := range []string{"fixed", "expression"} {
			t.Run(tc.name+mode, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, namedDecisionResponse(tc))
				}))
				defer upstream.Close()
				db, engine, _ := setupNamedDecisionsGateway(t, tc, upstream.URL)
				quota := 1000
				if mode == "fixed" {
					require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"decision-alias":0.002}`))
				} else {
					settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
					oldMode, oldExpr := settings.BillingMode, settings.BillingExpr
					t.Cleanup(func() { settings.BillingMode, settings.BillingExpr = oldMode, oldExpr })
					settings.BillingMode = map[string]string{"decision-alias": "tiered_expr"}
					settings.BillingExpr = map[string]string{"decision-alias": `tier("base", p*6+c*10)`}
					quota = 350
					if tc.typ == constant.ChannelTypeQianwenAI {
						quota = 300
					}
				}
				for i, path := range namedDecisionPaths {
					w := requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
					require.Equal(t, 200, w.Code, w.Body.String())
					assertTypeSafeQuota(t, db, quota*(i+1))
				}
			})
		}
	}
}

func TestNamedDecisionsPassthroughAndChannelTests(t *testing.T) {
	for _, tc := range namedDecisionChannels {
		for _, global := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/global=%t", tc.name, global), func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					require.Equal(t, tc.path, r.URL.Path)
					wantModel := tc.model
					if tc.typ == constant.ChannelTypeOpenRouter {
						wantModel = "decision-alias"
						require.False(t, gjson.GetBytes(body, "threshold").Exists(), "OpenRouter passthrough bypasses overrides")
						if gjson.GetBytes(body, "extra").Exists() {
							require.Equal(t, "false", gjson.GetBytes(body, "provider.allow_fallbacks").Raw)
							require.Equal(t, "client", gjson.GetBytes(body, "trace.generation_name").String())
							require.Equal(t, "session", gjson.GetBytes(body, "session_id").String())
						}
					} else {
						require.Equal(t, "0", gjson.GetBytes(body, "threshold").Raw)
						for _, field := range []string{"provider", "trace", "session_id"} {
							require.False(t, gjson.GetBytes(body, field).Exists(), field)
						}
					}
					require.Equal(t, wantModel, gjson.GetBytes(body, "model").String())
					_, _ = io.WriteString(w, namedDecisionResponse(tc))
				}))
				defer upstream.Close()
				_, engine, ch := setupNamedDecisionsGateway(t, tc, upstream.URL)
				settings := model_setting.GetGlobalSettings()
				old := settings.PassThroughRequestEnabled
				t.Cleanup(func() { settings.PassThroughRequestEnabled = old })
				settings.PassThroughRequestEnabled = global
				ch.SetSetting(dto.ChannelSettings{ShowErrorDetails: true, PassThroughBodyEnabled: !global})
				ch.ParamOverride = common.GetPointer(`{"threshold":0,"provider":{"allow_fallbacks":false},"trace":{},"session_id":"override"}`)
				require.NoError(t, ch.Update())
				for _, path := range namedDecisionPaths {
					w := requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
					require.Equal(t, 200, w.Code, w.Body.String())
				}
				for _, endpoint := range []string{"typesafe-systemone", "openai-decisions"} {
					result := testChannel(ch, "decision-alias", endpoint, true)
					require.NoError(t, result.localErr)
					require.Nil(t, result.newAPIError, "%v", result.newAPIError)
					require.Equal(t, 100, result.usage.PromptTokens)
				}
			})
		}
	}
}

func TestNamedDecisionsCrossChannelRetry(t *testing.T) {
	for _, firstTC := range namedDecisionChannels {
		for _, secondTC := range namedDecisionChannels {
			if firstTC.typ == secondTC.typ {
				continue
			}
			for _, path := range namedDecisionPaths {
				t.Run(firstTC.name+"-"+secondTC.name+path, func(t *testing.T) {
					var calls atomic.Int32
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := io.ReadAll(r.Body)
						require.NoError(t, err)
						first := calls.Add(1) == 1
						target := secondTC
						if first {
							target = firstTC
						}
						require.Equal(t, target.path, r.URL.Path)
						require.Equal(t, target.model, gjson.GetBytes(body, "model").String())
						require.Equal(t, first, gjson.GetBytes(body, "first_only").Exists())
						for _, field := range []string{"provider", "trace", "session_id"} {
							require.Equal(t, target.typ == constant.ChannelTypeOpenRouter, gjson.GetBytes(body, field).Exists(), field)
						}
						if first {
							w.WriteHeader(503)
							_, _ = io.WriteString(w, `{"error":{"message":"retry","code":503}}`)
							return
						}
						require.Equal(t, "9007199254740993", gjson.GetBytes(body, "extra.large").Raw)
						_, _ = io.WriteString(w, namedDecisionResponse(secondTC))
					}))
					defer upstream.Close()
					db, engine, first := setupNamedDecisionsGateway(t, firstTC, upstream.URL)
					common.RetryTimes = 1
					first.Priority = common.GetPointer(int64(10))
					first.ParamOverride = common.GetPointer(`{"first_only":false}`)
					require.NoError(t, first.Update())
					second := &model.Channel{Type: secondTC.typ, Name: secondTC.name, Key: "second-key", BaseURL: &upstream.URL, Models: "decision-alias", Group: "default", Status: common.ChannelStatusEnabled, AutoBan: common.GetPointer(0), ModelMapping: common.GetPointer(fmt.Sprintf(`{"decision-alias":"%s"}`, secondTC.model))}
					require.NoError(t, second.Insert())
					w := requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
					require.Equal(t, 200, w.Code, w.Body.String())
					require.EqualValues(t, 2, calls.Load())
					quota := 120
					if secondTC.typ == constant.ChannelTypeQianwenAI {
						quota = 100
					}
					assertTypeSafeQuota(t, db, quota)
					var logs []model.Log
					require.NoError(t, db.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1)
					require.Equal(t, second.Id, logs[0].ChannelId)
				})
			}
		}
	}
}

func TestNamedDecisionsValidation(t *testing.T) {
	for _, tc := range namedDecisionChannels {
		for _, path := range namedDecisionPaths {
			t.Run(tc.name+path, func(t *testing.T) {
				upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid request reached upstream") }))
				defer upstream.Close()
				db, engine, ch := setupNamedDecisionsGateway(t, tc, upstream.URL)
				body := namedDecisionBody(`"Help"`)
				invalid := []string{
					strings.Replace(body, `"stream":false`, `"stream":true`, 1),
					strings.Replace(body, `"stream":false`, `"stream_options":{}`, 1),
					`{"model":"decision-alias","input":"Help","questions":[{"type":"predicate","instructions":"Urgent?"}]}`,
				}
				for _, request := range invalid {
					w := requestTypeSafe(t, engine, path, request, typeSafeGatewayKey)
					require.Equal(t, 400, w.Code, w.Body.String())
				}
				for _, override := range []string{`{"stream":true}`, `{"stream_options":{}}`, `{"state":null}`, `{"questions":[]}`} {
					ch.ParamOverride = &override
					require.NoError(t, ch.Update())
					w := requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
					require.Equal(t, 400, w.Code, w.Body.String())
					result := testChannel(ch, "decision-alias", map[string]string{"/v1/systemone": "typesafe-systemone", "/v1/decisions": "openai-decisions"}[path], false)
					require.Error(t, result.localErr)
				}
				if tc.typ == constant.ChannelTypeOpenRouter {
					ch.ParamOverride = nil
					ch.SetOtherSettings(dto.ChannelOtherSettings{OpenRouterEnterprise: common.GetPointer(true)})
					require.NoError(t, ch.Update())
					w := requestTypeSafe(t, engine, path, body, typeSafeGatewayKey)
					require.Equal(t, 400, w.Code, w.Body.String())
					require.Contains(t, w.Body.String(), "standard channel")
				}
				assertTypeSafeQuota(t, db, 0)
			})
		}
	}
}

func TestNamedDecisionsErrorsAndZeroUsage(t *testing.T) {
	for _, tc := range namedDecisionChannels {
		for _, path := range namedDecisionPaths {
			t.Run(tc.name+path, func(t *testing.T) {
				response, contentType, status := "", "application/json", 200
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", contentType)
					w.Header().Set("Retry-After", "3")
					w.WriteHeader(status)
					_, _ = io.WriteString(w, response)
				}))
				defer upstream.Close()
				db, engine, ch := setupNamedDecisionsGateway(t, tc, upstream.URL)
				valid := namedDecisionResponse(tc)
				for _, failure := range []string{
					`not-json`,
					`{"model":"m","answers":{"a":{"type":"noul","noul":0}}}`,
					strings.Replace(valid, `"input_tokens":100`, `"input_tokens":-1`, 1),
					`{"error":{"message":"upstream failed"}}`,
				} {
					response = failure
					w := requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
					require.Equal(t, 502, w.Code, w.Body.String())
					assertTypeSafeQuota(t, db, 0)
				}
				response, contentType = valid, "text/event-stream"
				w := requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
				require.Equal(t, 502, w.Code, w.Body.String())
				assertTypeSafeQuota(t, db, 0)
				response, contentType = `{"error":{"message":"decision unavailable","code":429}}`, "application/json"
				for _, httpStatus := range []int{429, 503} {
					status = httpStatus
					w = requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
					require.Equal(t, status, w.Code, w.Body.String())
					if tc.typ != constant.ChannelTypeOpenRouter {
						require.JSONEq(t, response, w.Body.String())
						require.Equal(t, "3", w.Header().Get("Retry-After"))
					}
					assertTypeSafeQuota(t, db, 0)
				}
				status = 200
				response = strings.Replace(valid, gjson.Get(valid, "usage").Raw, `{"input_tokens":0,"output_tokens":0}`, 1)
				w = requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.JSONEq(t, response, w.Body.String())
				assertTypeSafeQuota(t, db, 0)
				ch.SetSetting(dto.ChannelSettings{ShowErrorDetails: true, ModelMappingFullEnabled: true})
				require.NoError(t, ch.Update())
				w = requestTypeSafe(t, engine, path, namedDecisionBody(`"Help"`), typeSafeGatewayKey)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, "decision-alias", gjson.Get(w.Body.String(), "model").String())
				assertTypeSafeQuota(t, db, 0)
			})
		}
	}
}
