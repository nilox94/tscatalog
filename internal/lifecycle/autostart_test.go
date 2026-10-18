package lifecycle

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlistOmitsKeepAlive(t *testing.T) {
	raw, err := LoginAgent{
		Executable: "/usr/local/bin/tscatalog",
		Catalog:    "/Users/me/.config/tscatalog/catalog.yaml",
		Path:       "/opt/homebrew/bin:/usr/bin:/bin",
		Log:        "/tmp/autostart.log",
	}.Plist()
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if strings.Contains(s, "KeepAlive") {
		t.Fatal("KeepAlive would respawn after stop; omit it so a stop stays down until the next login or up")
	}
	if !strings.Contains(s, "<key>RunAtLoad</key>\n  <true/>") {
		t.Fatalf("missing RunAtLoad:\n%s", s)
	}
	if !strings.Contains(s, "<string>autostart</string>") {
		t.Fatal("missing autostart argv")
	}
	if !strings.Contains(s, "<key>TSCATALOG_CATALOG</key>") || !strings.Contains(s, "<key>PATH</key>") {
		t.Fatalf("missing env snapshot:\n%s", s)
	}
	if !strings.Contains(s, AgentLabel) {
		t.Fatal("missing label")
	}
}

func TestPlistEscapesPATH(t *testing.T) {
	raw, err := LoginAgent{
		Executable: "/bin/tscatalog",
		Catalog:    "/tmp/catalog.yaml",
		Path:       "/opt/a&b/bin:/usr/bin",
		Log:        "/tmp/autostart.log",
	}.Plist()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "a&amp;b") {
		t.Fatalf("PATH not escaped:\n%s", raw)
	}
}

func TestRejectEphemeralExecutable(t *testing.T) {
	exe := filepath.Join("/var/folders/xx/go-build123", "tscatalog")
	if err := rejectEphemeralExecutable(exe); err == nil {
		t.Fatal("expected reject")
	}
	if err := rejectEphemeralExecutable("/usr/local/bin/tscatalog"); err != nil {
		t.Fatal(err)
	}
}

func TestPlistRequiresPATH(t *testing.T) {
	_, err := LoginAgent{
		Executable: "/bin/tscatalog",
		Catalog:    "/tmp/catalog.yaml",
		Log:        "/tmp/autostart.log",
	}.Plist()
	if err == nil {
		t.Fatal("expected empty PATH error")
	}
}
