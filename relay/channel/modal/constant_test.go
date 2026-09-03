package modal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://example--app.modal.direct":                     "https://example--app.modal.direct",
		"https://example--app.modal.direct/":                    "https://example--app.modal.direct",
		"https://example--app.modal.direct/v1":                  "https://example--app.modal.direct",
		"https://example--app.modal.direct/v1/chat/completions": "https://example--app.modal.direct",
		"https://example--app.modal.direct/chat/completions":    "https://example--app.modal.direct",
	}

	for input, expected := range tests {
		require.Equal(t, expected, NormalizeBaseURL(input))
	}
}
