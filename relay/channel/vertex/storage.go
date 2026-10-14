package vertex

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/service"
)

var storageBucket = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]$`)

// Build an exact Google Storage API URL. A service-account token is never sent
// to a caller-supplied HTTP host or forwarded across a redirect.
func StorageObjectURL(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "gs" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !storageBucket.MatchString(u.Host) || strings.TrimPrefix(u.Path, "/") == "" {
		return "", errors.New("invalid GCS object URI")
	}
	return "https://storage.googleapis.com/storage/v1/b/" + url.PathEscape(u.Host) + "/o/" + url.PathEscape(strings.TrimPrefix(u.Path, "/")) + "?alt=media", nil
}

func FetchStorageObject(ctx context.Context, uri, key, proxy string) (*http.Response, error) {
	target, err := StorageObjectURL(uri)
	if err != nil {
		return nil, err
	}
	creds, err := ParseCredentials(key)
	if err != nil {
		return nil, err
	}
	token, err := AcquireAccessTokenForRelayContext(ctx, creds, proxy, nil)
	if err != nil {
		return nil, err
	}
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	safeClient := *client
	safeClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return safeClient.Do(req)
}
