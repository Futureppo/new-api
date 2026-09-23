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
	header.Set("User-Agent", openCodeUserAgent(header.Get("User-Agent")))
	for _, item := range []struct{ name, value string }{
		{"x-opencode-client", defaultClient},
		{"x-opencode-project", defaultProject},
	} {
		if header.Get(item.name) == "" {
			header.Set(item.name, item.value)
		}
	}
	for _, item := range []struct{ name, prefix string }{
		{"x-opencode-session", "ses"},
		{"x-opencode-request", "msg"},
	} {
		if header.Get(item.name) != "" {
			continue
		}
		value, err := clientIdentifier(c, item.prefix)
		if err != nil {
			return err
		}
		header.Set(item.name, value)
	}
	return nil
}

func openCodeUserAgent(value string) string {
	if strings.HasPrefix(value, "opencode/") {
		return value
	}
	return defaultUserAgent
}
