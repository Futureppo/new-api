package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestLogStatTokenBreakdownAndUserScope(t *testing.T) {
	db := setupAuditLogControllerTestDB(t)
	require.NoError(t, db.Create(&[]model.Log{
		{Username: "alice", CreatedAt: common.GetTimestamp(), Type: model.LogTypeConsume, ModelName: "cache-model", PromptTokens: 100, CompletionTokens: 50, Other: `{"claude":true,"cache_tokens":900}`},
		{Username: "bob", CreatedAt: common.GetTimestamp(), Type: model.LogTypeConsume, ModelName: "cache-model", PromptTokens: 200, CompletionTokens: 20},
	}).Error)
	for _, self := range []bool{false, true} {
		name := "admin"
		if self {
			name = "self"
		}
		t.Run(name, func(t *testing.T) {
			// A supplied username must not override the authenticated self scope.
			c, recorder := newAuditLogTestContext(http.MethodGet, "/api/log/stat?username=bob&model_name=cache-model", "", 1, "alice", "")
			if self {
				GetLogsSelfStat(c)
			} else {
				GetLogsStat(c)
			}
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Input  *int `json:"input_tpm"`
					Output *int `json:"output_tpm"`
					Total  *int `json:"tpm"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			require.NotNil(t, response.Data.Input)
			require.NotNil(t, response.Data.Output)
			require.NotNil(t, response.Data.Total)
			if self {
				require.Equal(t, 1000, *response.Data.Input)
				require.Equal(t, 50, *response.Data.Output)
			} else {
				require.Equal(t, 200, *response.Data.Input)
				require.Equal(t, 20, *response.Data.Output)
			}
			require.Equal(t, *response.Data.Input+*response.Data.Output, *response.Data.Total)
		})
	}
}
