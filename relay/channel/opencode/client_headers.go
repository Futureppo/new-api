package opencode

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// OpenCode identifiers contain a 48-bit timestamp/counter and 14 random characters.
// Each independent generated session starts at counter 1, as in the official
// client's first ID in a millisecond. Random suffixes keep concurrent sessions
// unique without a process-wide counter leaking into the timestamp's low bits.
// https://github.com/anomalyco/opencode/blob/81200db2a3a86fc72a60dff996557f59865a6f24/packages/schema/src/identifier.ts
func clientSessionIdentifier(c *gin.Context) (string, error) {
	const key = "opencode.generated.ses"
	if value := c.GetString(key); value != "" {
		return value, nil
	}
	random, err := common.GenerateRandomCharsKey(14)
	if err != nil {
		return "", fmt.Errorf("generate opencode session identifier: %w", err)
	}
	value := ^(uint64(time.Now().UnixMilli())<<12 | 1)
	id := fmt.Sprintf("ses_%012x%s", value&0xffffffffffff, random)
	c.Set(key, id)
	return id, nil
}

func fillClientHeaders(c *gin.Context, header *http.Header) error {
	// Do not allow a downstream SDK's identity to survive into the OpenCode
	// request. The finalization hook repeats this after all header overrides;
	// cached identifiers keep the same identity throughout the request/retries.
	for name := range *header {
		if isClientIdentityHeader(name) {
			delete(*header, name)
		}
	}
	header.Set("User-Agent", defaultUserAgent)
	for _, item := range []struct{ name, value string }{
		{"x-opencode-client", defaultClient},
		{"x-opencode-project", defaultProject},
	} {
		header.Set(item.name, item.value)
	}
	session, err := clientSessionIdentifier(c)
	if err != nil {
		return err
	}
	// OpenCode 2.x no longer sends x-opencode-request. The upstream free-tier
	// gate rejects that legacy header even when the other identifiers are valid.
	for _, name := range []string{"x-opencode-session", "x-opencode-session-id", "x-session-affinity", "X-Session-Id"} {
		header.Set(name, session)
	}
	return nil
}

func isClientIdentityHeader(name string) bool {
	name = strings.ToLower(name)
	return name == "user-agent" || strings.HasPrefix(name, "x-opencode-") ||
		name == "x-parent-session-id" || name == "x-session-id" || name == "x-session-affinity" ||
		strings.HasPrefix(name, "x-stainless-") || strings.HasPrefix(name, "x-cline-") ||
		name == "x-app" || name == "x-title"
}
