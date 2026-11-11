package tsapi

import (
	"fmt"
	"os"

	"tailscale.com/client/tailscale/v2"
)

// oauthScopes is the Tailscale scope this process requests.
//
// kb/1623 lists policy_file for the policy file, and says that scope also
// requires devices:posture_attributes and devices:core:read. The same list
// does not name a scope for /api/v2/tailnet/{tailnet}/vip-services, including
// the devices lookup under that path. `all` is the scope that covers endpoints
// with no narrower one, so it is what a VIP-service write has to request.
// `all:write` is not a scope.
var oauthScopes = []string{"all"}

// NewClient builds a control-plane client.
// Auth: TS_API_KEY, or OAuth (TS_OAUTH_CLIENT_ID + TS_OAUTH_CLIENT_SECRET).
// Tailnet: TS_TAILNET, default "-" (the key's tailnet).
func NewClient() (*tailscale.Client, error) {
	c := &tailscale.Client{
		Tailnet:   os.Getenv("TS_TAILNET"),
		UserAgent: "tscatalog",
	}
	if c.Tailnet == "" {
		c.Tailnet = "-"
	}

	id := os.Getenv("TS_OAUTH_CLIENT_ID")
	secret := os.Getenv("TS_OAUTH_CLIENT_SECRET")
	if id != "" || secret != "" {
		if id == "" || secret == "" {
			return nil, fmt.Errorf("set both TS_OAUTH_CLIENT_ID and TS_OAUTH_CLIENT_SECRET")
		}
		c.Auth = &tailscale.OAuth{
			ClientID:     id,
			ClientSecret: secret,
			Scopes:       append([]string(nil), oauthScopes...),
		}
		return c, nil
	}

	c.APIKey = os.Getenv("TS_API_KEY")
	if c.APIKey == "" {
		return nil, fmt.Errorf("set TS_API_KEY (or OAuth client id/secret)")
	}
	return c, nil
}
