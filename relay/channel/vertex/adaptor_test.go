package vertex

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func adaptorTestCredentialsJSON(t *testing.T) (Credentials, string) {
	t.Helper()
	creds := Credentials{
		ProjectID:    "vertex-project",
		PrivateKeyID: "key-id",
		PrivateKey:   "test-private-key",
		ClientEmail:  "vertex@example.iam.gserviceaccount.com",
		TokenURI:     defaultGoogleTokenURI,
	}
	data, err := common.Marshal(creds)
	require.NoError(t, err)
	return creds, string(data)
}

func vertexRelayInfo(apiKey, modelName, region string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: modelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:         99,
			ApiKey:            apiKey,
			ApiVersion:        region,
			UpstreamModelName: modelName,
			ChannelOtherSettings: dto.ChannelOtherSettings{
				VertexKeyType: dto.VertexKeyTypeJSON,
			},
		},
	}
}

func TestAdaptorBuildsGoogleClaudeAndMaaSURLs(t *testing.T) {
	_, key := adaptorTestCredentialsJSON(t)
	tests := []struct {
		name     string
		model    string
		region   string
		wantURL  string
		isStream bool
	}{
		{
			name:    "google global",
			model:   "gemini-test",
			region:  `{"default":"global"}`,
			wantURL: "https://aiplatform.googleapis.com/v1/projects/vertex-project/locations/global/publishers/google/models/gemini-test:generateContent",
		},
		{
			name:    "claude regional",
			model:   "claude-opus-4-6",
			region:  `{"default":"us-east5"}`,
			wantURL: "https://us-east5-aiplatform.googleapis.com/v1/projects/vertex-project/locations/us-east5/publishers/anthropic/models/claude-opus-4-6:rawPredict",
		},
		{
			name:    "maas preserves global hostname",
			model:   "meta/llama3-405b-instruct-maas",
			region:  `{"default":"us-central1"}`,
			wantURL: "https://aiplatform.googleapis.com/v1beta1/projects/vertex-project/locations/us-central1/endpoints/openapi/chat/completions",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			info := vertexRelayInfo(key, test.model, test.region)
			info.IsStream = test.isStream
			adaptor := &Adaptor{}
			adaptor.Init(info)
			got, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			require.Equal(t, test.wantURL, got)
		})
	}
}

func TestAdaptorSetupHeaderDoesNotForceUserProject(t *testing.T) {
	accessTokenCache = sync.Map{}
	t.Cleanup(func() { accessTokenCache = sync.Map{} })
	creds, key := adaptorTestCredentialsJSON(t)
	info := vertexRelayInfo(key, "gemini-test", `{"default":"global"}`)
	accessTokenCache.Store(accessTokenCacheKey(creds, info), cachedAccessToken{
		Token:     "cached-token",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	ctx.Request.Header.Set("Content-Type", "application/json")
	headers := http.Header{}
	adaptor := &Adaptor{AccountCredentials: creds}
	require.NoError(t, adaptor.SetupRequestHeader(ctx, &headers, info))
	require.Equal(t, "Bearer cached-token", headers.Get("Authorization"))
	require.Empty(t, headers.Get("x-goog-user-project"))
}

func TestAdaptorRejectsClaudeInAPIKeyMode(t *testing.T) {
	info := vertexRelayInfo("api-key", "claude-opus-4-6", `{"default":"global"}`)
	info.ChannelOtherSettings.VertexKeyType = dto.VertexKeyTypeAPIKey
	adaptor := &Adaptor{}
	adaptor.Init(info)
	_, err := adaptor.GetRequestURL(info)
	require.ErrorContains(t, err, "only supports Google publisher models")
}
