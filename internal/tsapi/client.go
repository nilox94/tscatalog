package tsapi

import (
	"fmt"
	"os"

	"tailscale.com/client/tailscale/v2"
)

// NewClient builds a control-plane client.
// Auth: TAILSCALE_API_KEY, or TS_API_KEY, or OAuth (TAILSCALE_OAUTH_CLIENT_ID + SECRET).
// Tailnet: TAILSCALE_TAILNET, default "-" (the key's tailnet).
func NewClient() (*tailscale.Client, error) {
	c := &tailscale.Client{
		Tailnet:   envFirst("TAILSCALE_TAILNET", "TS_TAILNET"),
		UserAgent: "tscatalog",
	}
	if c.Tailnet == "" {
		c.Tailnet = "-"
	}

	id := envFirst("TAILSCALE_OAUTH_CLIENT_ID", "TS_OAUTH_CLIENT_ID")
	secret := envFirst("TAILSCALE_OAUTH_CLIENT_SECRET", "TS_OAUTH_CLIENT_SECRET")
	if id != "" || secret != "" {
		if id == "" || secret == "" {
			return nil, fmt.Errorf("set both TAILSCALE_OAUTH_CLIENT_ID and TAILSCALE_OAUTH_CLIENT_SECRET")
		}
		c.Auth = &tailscale.OAuth{
			ClientID:     id,
			ClientSecret: secret,
			Scopes:       []string{"all:write"},
		}
		return c, nil
	}

	c.APIKey = envFirst("TAILSCALE_API_KEY", "TS_API_KEY")
	if c.APIKey == "" {
		return nil, fmt.Errorf("set TAILSCALE_API_KEY (or OAuth client id/secret)")
	}
	return c, nil
}

func envFirst(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}
