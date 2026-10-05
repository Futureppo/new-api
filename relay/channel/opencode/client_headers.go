package opencode

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var clientIdentifierCounter atomic.Uint64

// OpenCode identifiers contain a 48-bit timestamp/counter and 14 random characters.
// Sessions sort descending; request message identifiers sort ascending.
// Format: https://github.com/anomalyco/opencode/blob/v1.18.32/packages/schema/src/identifier.ts
func clientIdentifier(c *gin.Context, prefix string) (string, error) {
	key := "opencode.generated." + prefix
	if value := c.GetString(key); value != "" {
		return value, nil
	}
	random, err := common.GenerateRandomCharsKey(14)
	if err != nil {
		return "", fmt.Errorf("generate opencode %s identifier: %w", prefix, err)
	}
	value := uint64(time.Now().UnixMilli())<<12 | (clientIdentifierCounter.Add(1) & 0xfff)
	if prefix == "ses" {
		value = ^value
	}
	id := fmt.Sprintf("%s_%012x%s", prefix, value&0xffffffffffff, random)
	c.Set(key, id)
	return id, nil
}

func fillClientHeaders(c *gin.Context, header *http.Header) error {
	// Do not allow a downstream SDK's identity to survive into the OpenCode
	// request, including through wildcard/regex passthrough. Explicit channel
	// header overrides are still applied afterwards by DoApiRequest.
	for name := range *header {
		if isClientIdentityHeader(name) {
			header.Del(name)
		}
	}
	header.Set("User-Agent", defaultUserAgent)
	for _, item := range []struct{ name, value string }{
		{"x-opencode-client", defaultClient},
		{"x-opencode-project", defaultProject},
	} {
		header.Set(item.name, item.value)
	}
	for _, item := range []struct{ name, prefix string }{
		{"x-opencode-session", "ses"},
		{"x-opencode-request", "msg"},
	} {
		value, err := clientIdentifier(c, item.prefix)
		if err != nil {
			return err
		}
		header.Set(item.name, value)
	}
	// Official request.ts sends both session header names with the same value.
	header.Set("x-opencode-session-id", header.Get("x-opencode-session"))
	return nil
}

func isClientIdentityHeader(name string) bool {
	name = strings.ToLower(name)
	return name == "user-agent" || strings.HasPrefix(name, "x-opencode-") ||
		name == "x-parent-session-id" || name == "x-session-id" || name == "x-session-affinity" ||
		strings.HasPrefix(name, "x-stainless-") || strings.HasPrefix(name, "x-cline-") ||
		name == "x-app" || name == "x-title"
}
