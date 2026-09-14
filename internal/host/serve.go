package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"tscatalog/internal/catalog"
)

// CLI talks to the local tailscaled via the tailscale CLI.
// Serve mappings are host-local; they are not in the control-plane API.
type CLI struct {
	Bin string
}

func (c CLI) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "tailscale"
}

func (c CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("tailscale %s: %w\n%s", strings.Join(args, " "), err, bytes.TrimSpace(out))
	}
	return out, nil
}

func (c CLI) GetConfig(ctx context.Context) (*catalog.ServeFile, error) {
	out, err := c.run(ctx, "serve", "get-config", "--all")
	if err != nil {
		return &catalog.ServeFile{
			Version:  catalog.ServeVersion,
			Services: map[string]catalog.ServeService{},
		}, nil
	}
	var cfg catalog.ServeFile
	if err := json.Unmarshal(out, &cfg); err != nil {
		return nil, fmt.Errorf("parse serve config: %w", err)
	}
	if cfg.Services == nil {
		cfg.Services = map[string]catalog.ServeService{}
	}
	return &cfg, nil
}

// Serve publishes one VIP Service over HTTPS, proxying to a local HTTP target.
//
// `tailscale serve set-config` treats the target scheme as the *public*
// protocol. get-config dumps the backend as http://, so --all would try to
// replace an existing HTTPS:443 mapping with HTTP:443 and fail. The CLI
// form (--https=443 http://127.0.0.1:port) keeps public HTTPS.
func (c CLI) Serve(ctx context.Context, name, endpoint, local string) error {
	port, ok := strings.CutPrefix(endpoint, "tcp:")
	if !ok || port == "" {
		return fmt.Errorf("endpoint %q: want tcp:<port>", endpoint)
	}
	_, err := c.run(ctx, "serve",
		"--service="+name,
		"--https="+port,
		"--bg",
		"--yes",
		local,
	)
	return err
}

func (c CLI) Advertise(ctx context.Context, svc string) error {
	_, err := c.run(ctx, "serve", "advertise", svc)
	return err
}

type MagicDNS struct {
	Suffix string
}

func (c CLI) MagicDNS(ctx context.Context) (MagicDNS, error) {
	out, err := c.run(ctx, "status", "--json")
	if err != nil {
		return MagicDNS{}, err
	}
	var st struct {
		MagicDNSSuffix string `json:"MagicDNSSuffix"`
		CurrentTailnet struct {
			MagicDNSSuffix string `json:"MagicDNSSuffix"`
		} `json:"CurrentTailnet"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return MagicDNS{}, err
	}
	suf := st.MagicDNSSuffix
	if suf == "" {
		suf = st.CurrentTailnet.MagicDNSSuffix
	}
	return MagicDNS{Suffix: suf}, nil
}
