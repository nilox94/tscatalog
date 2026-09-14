package tsapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvSetsMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("CATALOG_TEST_KEY=from-file\n# comment\nexport CATALOG_TEST_EXPORT=yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CATALOG_TEST_KEY", "")
	t.Setenv("CATALOG_TEST_EXPORT", "")
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CATALOG_TEST_KEY") != "from-file" {
		t.Fatalf("got %q", os.Getenv("CATALOG_TEST_KEY"))
	}
	if os.Getenv("CATALOG_TEST_EXPORT") != "yes" {
		t.Fatalf("export got %q", os.Getenv("CATALOG_TEST_EXPORT"))
	}
}

func TestLoadDotEnvDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("CATALOG_TEST_KEEP=file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CATALOG_TEST_KEEP", "env")
	if err := LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("CATALOG_TEST_KEEP") != "env" {
		t.Fatalf("overwrote env: %q", os.Getenv("CATALOG_TEST_KEEP"))
	}
}

func TestLoadDotEnvMissingOK(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), ".env")); err != nil {
		t.Fatal(err)
	}
}
