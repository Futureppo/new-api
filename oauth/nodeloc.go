package oauth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

const (
	nodeLocTokenEndpoint = "https://www.nodeloc.com/oauth-provider/token"
	nodeLocUserEndpoint  = "https://www.nodeloc.com/oauth-provider/userinfo"
)

func init() {
	Register("nodeloc", &NodeLocProvider{})
}

type NodeLocProvider struct {
	client *http.Client
}

func (p *NodeLocProvider) GetName() string           { return "NodeLoc" }
func (p *NodeLocProvider) GetProviderPrefix() string { return "nodeloc_" }
func (p *NodeLocProvider) IsEnabled() bool {
	return system_setting.GetNodeLocSettings().Enabled
}

// Do not include response bodies or request credentials in errors or logs.
func (p *NodeLocProvider) requestJSON(req *http.Request, target any, failureKey string) error {
	client := p.client
	if client == nil {
		client = &http.Client{
			Timeout:       20 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	res, err := client.Do(req)
	if err != nil {
		return NewOAuthError(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": p.GetName()})
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return NewOAuthError(failureKey, map[string]any{"Provider": p.GetName()})
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1024*1024))
	if err != nil || common.Unmarshal(body, target) != nil {
		return NewOAuthError(failureKey, map[string]any{"Provider": p.GetName()})
	}
	return nil
}

func (p *NodeLocProvider) ExchangeToken(ctx context.Context, code string, _ *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}
	settings := system_setting.GetNodeLocSettings()
	if !settings.IsConfigured() {
		return nil, NewOAuthError(i18n.MsgNodeLocConfigInvalid, nil)
	}
	redirectURI, _ := system_setting.NodeLocRedirectURI(system_setting.ServerAddress)
	values := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {settings.ClientId},
		"client_secret": {settings.ClientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, nodeLocTokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": p.GetName()})
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var response struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := p.requestJSON(req, &response, i18n.MsgOAuthTokenFailed); err != nil {
		return nil, err
	}
	if response.Error != "" || strings.TrimSpace(response.AccessToken) == "" {
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": p.GetName()})
	}
	return &OAuthToken{AccessToken: response.AccessToken, TokenType: "Bearer"}, nil
}

func (p *NodeLocProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": p.GetName()})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nodeLocUserEndpoint, nil)
	if err != nil {
		return nil, NewOAuthError(i18n.MsgOAuthGetUserErr, nil)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/json")
	// Email, avatar and trust level are deliberately not part of the local identity.
	var response struct {
		Id       int64  `json:"id"`
		Username string `json:"username"`
		Name     string `json:"name"`
		Error    string `json:"error"`
	}
	if err := p.requestJSON(req, &response, i18n.MsgOAuthGetUserErr); err != nil {
		return nil, err
	}
	if response.Error != "" || response.Id <= 0 {
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": p.GetName()})
	}
	return &OAuthUser{ProviderUserID: strconv.FormatInt(response.Id, 10), Username: response.Username, DisplayName: response.Name}, nil
}

func (p *NodeLocProvider) IsUserIDTaken(id string) bool { return model.IsNodeLocIdAlreadyTaken(id) }
func (p *NodeLocProvider) FillUserByProviderID(user *model.User, id string) error {
	user.NodeLocId = id
	return user.FillUserByNodeLocId()
}
func (p *NodeLocProvider) SetProviderUserID(user *model.User, id string) { user.NodeLocId = id }
