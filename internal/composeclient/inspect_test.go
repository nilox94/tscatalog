package composeclient

import "testing"

func TestCommandFromInspect(t *testing.T) {
	if got := commandFromInspect("/docker-entrypoint.sh", []string{"nginx", "-g", "daemon off;"}); got != "/docker-entrypoint.sh nginx -g daemon off;" {
		t.Fatalf("got %q", got)
	}
	if commandFromInspect("", []string{"a"}) != "" {
		t.Fatal("empty path")
	}
	if commandFromInspect("/bin/sh", nil) != "/bin/sh" {
		t.Fatal("path only")
	}
}
