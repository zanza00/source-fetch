package cache

import (
	"os"
	"path/filepath"
	"strings"
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
	if !strings.HasPrefix(a, "path-") {
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

func TestAdhocRootHonoursEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "override")
	t.Setenv(EnvCache, override)
	got, err := AdhocRoot()
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(override)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(abs, "adhoc"); got != want {
		t.Fatalf("AdhocRoot = %s, want %s", got, want)
	}
}

func TestAdhocName(t *testing.T) {
	url := "https://github.com/vercel-labs/agent-browser.git"
	got := AdhocName(url)
	if !strings.HasPrefix(got, "agent-browser-") {
		t.Fatalf("AdhocName = %q, want agent-browser-<hash> prefix", got)
	}
	if got != AdhocName(url) {
		t.Fatalf("AdhocName not stable: %s vs %s", got, AdhocName(url))
	}
	// Same basename from a different host must not collide.
	if other := AdhocName("https://example.com/agent-browser.git"); other == got {
		t.Fatalf("same-named repos collide: %s", got)
	}
	// scp-style and query/fragment URLs sanitise to a filesystem-safe name.
	weird := AdhocName("git@example.com:org/we ird.git?x=1")
	if !strings.HasPrefix(weird, "we-ird-") {
		t.Fatalf("AdhocName = %q, want sanitised we-ird-<hash> prefix", weird)
	}
	if AdhocName("https://example.com/") == "" {
		t.Fatal("AdhocName should never be empty")
	}
}
