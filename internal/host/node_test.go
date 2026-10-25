package host

import (
	"testing"
)

func TestExtractSuggestedUp(t *testing.T) {
	msg := `Error: changing settings via 'tailscale up' requires mentioning all
non-default flags. To proceed, either re-run your command with --reset or
use the command below to explicitly mention the current value of
all non-default settings:

	tailscale up --advertise-tags=tag:macos --accept-routes --advertise-exit-node --exit-node-allow-lan-access
`
	got := extractSuggestedUp(msg)
	want := "tailscale up --advertise-tags=tag:macos --accept-routes --advertise-exit-node --exit-node-allow-lan-access"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestPrefsUpFlagsExitNode(t *testing.T) {
	args := prefsUpFlags(debugPrefs{
		RouteAll:               true,
		AdvertiseRoutes:        []string{"0.0.0.0/0", "::/0"},
		ExitNodeAllowLANAccess: true,
	})
	if len(args) != 2 {
		t.Fatalf("lan-access without exit-node should be omitted: %v", args)
	}
	args = prefsUpFlags(debugPrefs{
		RouteAll:               true,
		ExitNodeAllowLANAccess: true,
		ExitNodeIP:             "100.1.2.3",
	})
	if len(args) != 2 {
		t.Fatalf("%v", args)
	}
}

func TestUnionStringsAddsTagPrefix(t *testing.T) {
	got := unionStrings(nil, []string{"macos"})
	if len(got) != 1 || got[0] != "tag:macos" {
		t.Fatalf("%v", got)
	}
}
