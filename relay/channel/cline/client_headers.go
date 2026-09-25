package cline

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// Match the official CLI identity at cline/cline@0cbfb91ac75c64b7a63535fde6f7f51e670dfa1b:
// apps/cli/src/main.ts and sdk/packages/llms/src/providers/request-headers.ts.
const (
	ClientVersion = "3.0.65"
	CoreVersion   = "0.0.86"
	UserAgent     = "Cline/" + ClientVersion
)

func SetClientHeaders(header http.Header) {
	header.Set("HTTP-Referer", "https://cline.bot")
	header.Set("X-Title", "Cline")
	header.Set("User-Agent", UserAgent)
	header.Set("X-IS-MULTIROOT", "false")
	header.Set("X-CLIENT-TYPE", "cline-cli")
	header.Set("X-CLIENT-VERSION", ClientVersion)
	header.Set("X-PLATFORM", "cli")
	header.Set("X-PLATFORM-VERSION", ClientVersion)
}

func SetChatHeaders(c *gin.Context, header http.Header) error {
	SetClientHeaders(header)
	header.Set("Content-Type", "application/json")
	header.Set("X-CORE-VERSION", CoreVersion)
	const contextKey = "cline.task_id"
	taskID := c.GetString(contextKey)
	if taskID == "" {
		taskID = strings.TrimSpace(c.GetHeader("X-Task-ID"))
		if taskID == "" {
			// Official session format: Date.now() + "_" + five lowercase
			// alphanumeric characters (sdk/packages/shared/src/session/index.ts).
			suffix, err := common.GenerateRandomCharsKey(5)
			if err != nil {
				return fmt.Errorf("generate Cline task ID: %w", err)
			}
			taskID = fmt.Sprintf("%d_%s", time.Now().UnixMilli(), strings.ToLower(suffix))
		}
		// Keep the same task on relay retries; callers can supply a stable
		// X-Task-ID to group multiple turns of the same conversation.
		c.Set(contextKey, taskID)
	}
	header.Set("X-Task-ID", taskID)
	return nil
}

// Wildcard/regex passthrough must not replace the upstream client identity
// with another application's headers. Explicit channel overrides still win,
// except User-Agent, which is enforced after all overrides.
func FilterHeaderPassthrough(headers map[string]string) {
	for name := range headers {
		switch strings.ToLower(name) {
		case "http-referer", "x-title", "user-agent", "x-is-multiroot",
			"x-client-type", "x-client-version", "x-platform", "x-platform-version",
			"x-core-version", "x-task-id":
			delete(headers, name)
		}
	}
}
