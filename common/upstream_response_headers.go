package common

import (
	"net/http"
	"strconv"
	"strings"
)

// CopyUpstreamResponseHeaders is the shared policy for every upstream-to-client
// header copy. Only payload metadata and a valid retry delay may cross this
// boundary; provider diagnostics, URLs, credentials, cookies and unknown headers
// must never override the gateway's own response headers.
func CopyUpstreamResponseHeaders(dst, src http.Header) {
	if dst == nil {
		return
	}

	// Even an otherwise allowed header is hop-by-hop when Connection names it.
	hopByHop := make(map[string]bool)
	for key, values := range src {
		if strings.EqualFold(key, "Connection") {
			for _, value := range values {
				for _, token := range strings.Split(value, ",") {
					hopByHop[strings.ToLower(strings.TrimSpace(token))] = true
				}
			}
		}
	}

	for key, values := range src {
		name := strings.ToLower(key)
		if len(values) == 0 || hopByHop[name] {
			continue
		}
		switch name {
		case "content-type", "content-length", "content-encoding",
			"content-disposition", "content-range", "accept-ranges":
			dst[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
		case "retry-after":
			value := strings.TrimSpace(values[0])
			if _, err := strconv.ParseUint(value, 10, 64); err != nil {
				if _, err := http.ParseTime(value); err != nil {
					continue
				}
			}
			dst.Set("Retry-After", value)
		}
	}
}
