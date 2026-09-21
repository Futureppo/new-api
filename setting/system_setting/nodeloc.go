package system_setting

import (
	"errors"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

type NodeLocSettings struct {
	Enabled      bool   `json:"enabled"`
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

var defaultNodeLocSettings = NodeLocSettings{}

func init() {
	config.GlobalConfig.Register("nodeloc", &defaultNodeLocSettings)
}

func GetNodeLocSettings() *NodeLocSettings {
	return &defaultNodeLocSettings
}

// NodeLocRedirectURI is shared by the public status response and token exchange.
func NodeLocRedirectURI(serverAddress string) (string, error) {
	address := strings.TrimRight(strings.TrimSpace(serverAddress), "/")
	u, err := url.Parse(address)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(address, "#") || u.Path != "" {
		return "", errors.New("invalid NodeLoc server address")
	}
	return address + "/oauth/nodeloc", nil
}

func (s *NodeLocSettings) IsConfigured() bool {
	_, err := NodeLocRedirectURI(ServerAddress)
	return err == nil && strings.TrimSpace(s.ClientId) != "" && strings.TrimSpace(s.ClientSecret) != ""
}
