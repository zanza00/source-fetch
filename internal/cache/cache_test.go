package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuardCleanTarget(t *testing.T) {
	home, _ := os.UserHomeDir()

	for _, bad := range []string{"", string(filepath.Separator), ".", home} {
		if err := GuardCleanTarget(bad); err == nil {
			t.Errorf("GuardCleanTarget(%q) should fail", bad)
		}
	}
	if err := GuardCleanTarget(filepath.Join(t.TempDir(), "cache")); err != nil {
		t.Errorf("normal path should pass: %v", err)
	}
}

func TestManifestID(t *testing.T) {
	// Outside a git repo: stable path-<hash>.
	dir := t.TempDir()
	path := filepath.Join(dir, ".source-fetch.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	a := ManifestID(path)
	b := ManifestID(path)
	if a != b {
		t.Fatalf("not stable: %s vs %s", a, b)
	}
	if len(a) < len("path-") || a[:len("path-")] != "path-" {
		t.Fatalf("unexpected id: %s", a)
	}
}

func TestRootHonoursEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "override")
	t.Setenv(EnvCache, override)
	got, err := Root(filepath.Join(t.TempDir(), ".source-fetch.toml"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(override)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Root = %s, want %s", got, want)
	}
}
