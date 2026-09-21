package system_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeLocRedirectURI(t *testing.T) {
	for _, address := range []string{"https://gateway.example", "https://gateway.example/", " https://gateway.example/// "} {
		uri, err := NodeLocRedirectURI(address)
		require.NoError(t, err)
		require.Equal(t, "https://gateway.example/oauth/nodeloc", uri)
	}
	uri, err := NodeLocRedirectURI("http://localhost:3000")
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3000/oauth/nodeloc", uri)
	for _, address := range []string{"", "gateway.example", "//gateway.example", "ftp://gateway.example", "https://", "https://user:pass@gateway.example", "https://gateway.example/path", "https://gateway.example?x=1", "https://gateway.example?", "https://gateway.example/#anchor", "https://gateway.example#"} {
		_, err := NodeLocRedirectURI(address)
		require.Error(t, err, address)
	}
}
