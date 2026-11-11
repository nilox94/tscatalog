package tsapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvQuotesAndPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "TS_API_KEY=\"tskey-api-abc\" # note\n" +
		"OTHER=nope\n" +
		"TS_TAILNET='example.com' # tailnet\n" +
		"TS_QUOTED=\"hash # inside\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TS_API_KEY", "")
	t.Setenv("TS_TAILNET", "")
	t.Setenv("TS_QUOTED", "")
	t.Setenv("OTHER", "")
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TS_API_KEY"); got != "tskey-api-abc" {
		t.Fatalf("key %q", got)
	}
	if got := os.Getenv("TS_TAILNET"); got != "example.com" {
		t.Fatalf("tailnet %q", got)
	}
	if got := os.Getenv("TS_QUOTED"); got != "hash # inside" {
		t.Fatalf("quoted %q", got)
	}
	if os.Getenv("OTHER") != "" {
		t.Fatalf("non-TS key was loaded: %q", os.Getenv("OTHER"))
	}
}
