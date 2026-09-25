package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// An error after partial SSE must reach the normal error handler without
// committing streaming headers, including through passthrough and overrides.
func TestClineNonStreamPipeline(t *testing.T) {
	service.InitHttpClient()
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	for _, tc := range []struct {
		name, stream string
		passthrough  bool
		override     map[string]any
	}{
		{name: "omitted stream"},
		{name: "explicit false", stream: `,"stream":false`},
		{name: "passthrough", stream: `,"stream":false`, passthrough: true},
		{name: "override", override: map[string]any{"stream": false, "stream_options": map[string]any{"include_usage": false}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				captured <- data
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: "+`{"choices":[{"index":0,"delta":{"content":"partial"}}]}`+"\n\ndata: "+`{"error":{"message":"upstream failed"}}`+"\n\ndata: [DONE]\n\n")
			}))
			defer upstream.Close()
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			body := `{"model":"cline-free/a","messages":[{"role":"user","content":"hi"}]` + tc.stream + `}`
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			defer common.CleanupBodyStorage(c)
			common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeCline)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, upstream.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, "cline-free/a")
			common.SetContextKey(c, constant.ContextKeyChannelSetting, dto.ChannelSettings{PassThroughBodyEnabled: tc.passthrough})
			common.SetContextKey(c, constant.ContextKeyChannelParamOverride, tc.override)
			request, err := helper.GetAndValidateRequest(c, types.RelayFormatOpenAI)
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{Request: request, OriginModelName: "cline-free/a", RequestURLPath: "/v1/chat/completions", RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, StartTime: time.Now()}
			apiErr := TextHelper(c, info)
			require.NotNil(t, apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			require.Contains(t, apiErr.Error(), "upstream failed")
			require.False(t, info.IsStream)
			require.False(t, c.Writer.Written())
			require.Empty(t, w.Header().Get("Content-Type"))
			select {
			case body := <-captured:
				var got map[string]any
				require.NoError(t, common.Unmarshal(body, &got))
				require.Equal(t, true, got["stream"])
				require.Equal(t, map[string]any{"include_usage": true}, got["stream_options"])
			default:
				t.Fatal("request did not reach upstream")
			}
		})
	}
}
