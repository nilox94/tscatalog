package tsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ServiceHost is one advertised host for a VIP Service.
type ServiceHost struct {
	NodeID        string `json:"nodeId"`
	ApprovalLevel string `json:"approvalLevel"`
	Configured    string `json:"configured"`
}

type hostsResponse struct {
	Hosts []ServiceHost `json:"hosts"`
}

// ListHosts returns advertised hosts and their approval for svc:name.
func ListHosts(ctx context.Context, name string) ([]ServiceHost, error) {
	c, err := NewClient()
	if err != nil {
		return nil, err
	}
	if c.APIKey == "" {
		return nil, fmt.Errorf("ListHosts requires TAILSCALE_API_KEY")
	}
	tailnet := c.Tailnet
	if tailnet == "" {
		tailnet = "-"
	}
	u := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/vip-services/%s/devices", tailnet, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("User-Agent", "tscatalog")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list hosts %s: HTTP %d: %s", name, res.StatusCode, strings.TrimSpace(string(body)))
	}
	var out hostsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out.Hosts, nil
}

// ApprovalSummary is a compact host-approval line for show/status.
func ApprovalSummary(hosts []ServiceHost) string {
	if len(hosts) == 0 {
		return "(no host advertised)"
	}
	parts := make([]string, 0, len(hosts))
	for _, h := range hosts {
		level := h.ApprovalLevel
		if level == "" {
			level = "unknown"
		}
		cfg := h.Configured
		if cfg == "" {
			cfg = "unknown"
		}
		parts = append(parts, level+" / "+cfg)
	}
	return strings.Join(parts, "; ")
}
