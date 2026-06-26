package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/stretchr/testify/require"
)

func TestValidateOAuthAccountAgeForNewAssociation(t *testing.T) {
	oldMinimumAgeSeconds := common.GitHubMinimumAccountAgeSeconds
	t.Cleanup(func() {
		common.GitHubMinimumAccountAgeSeconds = oldMinimumAgeSeconds
	})

	now := time.Unix(2_000_000_000, 0)
	githubProvider := &oauth.GitHubProvider{}
	requireAccountAgeTooLowError := func(t *testing.T, err error) {
		t.Helper()
		var ageErr *OAuthAccountAgeTooLowError
		require.ErrorAs(t, err, &ageErr)
	}

	t.Run("disabled with zero seconds", func(t *testing.T) {
		common.GitHubMinimumAccountAgeSeconds = 0
		require.NoError(t, validateOAuthAccountAgeForNewAssociation(githubProvider, nil, now))
	})

	t.Run("rejects younger account", func(t *testing.T) {
		common.GitHubMinimumAccountAgeSeconds = 10
		createdAt := now.Add(-9 * time.Second)
		err := validateOAuthAccountAgeForNewAssociation(githubProvider, &oauth.OAuthUser{
			AccountCreatedAt: &createdAt,
		}, now)
		requireAccountAgeTooLowError(t, err)
	})

	t.Run("rejects exact threshold", func(t *testing.T) {
		common.GitHubMinimumAccountAgeSeconds = 10
		createdAt := now.Add(-10 * time.Second)
		err := validateOAuthAccountAgeForNewAssociation(githubProvider, &oauth.OAuthUser{
			AccountCreatedAt: &createdAt,
		}, now)
		requireAccountAgeTooLowError(t, err)
	})

	t.Run("allows older account", func(t *testing.T) {
		common.GitHubMinimumAccountAgeSeconds = 10
		createdAt := now.Add(-11 * time.Second)
		err := validateOAuthAccountAgeForNewAssociation(githubProvider, &oauth.OAuthUser{
			AccountCreatedAt: &createdAt,
		}, now)
		require.NoError(t, err)
	})

	t.Run("rejects missing creation time", func(t *testing.T) {
		common.GitHubMinimumAccountAgeSeconds = 10
		err := validateOAuthAccountAgeForNewAssociation(githubProvider, &oauth.OAuthUser{}, now)
		requireAccountAgeTooLowError(t, err)
	})

	t.Run("ignores non GitHub providers", func(t *testing.T) {
		common.GitHubMinimumAccountAgeSeconds = 10
		err := validateOAuthAccountAgeForNewAssociation(&oauth.DiscordProvider{}, &oauth.OAuthUser{}, now)
		require.NoError(t, err)
	})
}
