package cache

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestIsSCPLike(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"deploy@example.com:o/r", true},
		{"git@github.com:o/r", true},
		{"example.com:o/r", true},
		{"https://example.com/o/r", false},
		{"ssh://git@example.com/o/r", false},
		{"file:///tmp/repo", false},
		{`C:\repo`, false},
		{"C:/repo", false},
		{"/tmp/repo", false},
		{"repo", false},
	}
	for _, tt := range tests {
		if got := IsSCPLike(tt.in); got != tt.want {
			t.Errorf("IsSCPLike(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsWindowsPathOS(t *testing.T) {
	tests := []struct {
		in      string
		windows bool
		want    bool
	}{
		{"C:", true, true},
		{"C:", false, true},
		{`C:\repo`, true, true},
		{`C:\repo`, false, true},
		{"C:/repo", true, true},
		{"C:/repo", false, true},
		{"C:repo", true, true},
		{"C:repo", false, false},
		{"1:repo", true, false},
		{"", true, false},
		{"ab", true, false},
		{"git@example.com:repo", true, false},
	}
	for _, tt := range tests {
		if got := isWindowsPathOS(tt.in, tt.windows); got != tt.want {
			t.Errorf("isWindowsPathOS(%q, windows=%v) = %v, want %v", tt.in, tt.windows, got, tt.want)
		}
	}
}

// TestAdhocKeyDriveRelative pins the platform split for C:relative: a local
// path on Windows, an scp-like URL (host "c") elsewhere.
func TestAdhocKeyDriveRelative(t *testing.T) {
	if runtime.GOOS == "windows" {
		if got, want := AdhocKey("C:repo"), filepath.Clean("C:repo"); got != want {
			t.Errorf("AdhocKey(C:repo) = %q, want %q", got, want)
		}
		return
	}
	if got, want := AdhocKey("C:repo"), "scp://c:repo"; got != want {
		t.Errorf("AdhocKey(C:repo) = %q, want %q", got, want)
	}
}

func TestAdhocKeyCanonicalHTTP(t *testing.T) {
	want := AdhocKey("https://github.com/o/r")
	for _, raw := range []string{
		"https://github.com/o/r.git",
		"https://github.com/o/r/",
		"HTTPS://GitHub.COM:443/o/r?x=1#L2",
		"https://user:pass@github.com/o/r",
		"https://github.com/o/r///",
	} {
		if got := AdhocKey(raw); got != want {
			t.Errorf("AdhocKey(%q) = %q, want %q", raw, got, want)
		}
	}

	// The default port for each scheme is removed; a non-default one is kept.
	if got, want := AdhocKey("http://github.com:80/o/r"), AdhocKey("http://github.com/o/r"); got != want {
		t.Errorf("http default port: %q != %q", got, want)
	}
	if AdhocKey("https://github.com:8443/o/r") == want {
		t.Error("non-default port should change the key")
	}

	// Known hosts treat .git as optional; unknown hosts keep it.
	for _, host := range []string{"github.com", "gitlab.com", "codeberg.org", "gitea.com"} {
		with := "https://" + host + "/o/r.git"
		without := "https://" + host + "/o/r"
		if AdhocKey(with) != AdhocKey(without) {
			t.Errorf("%s: .git should not change the key", host)
		}
	}
	if AdhocKey("https://example.com/o/r.git") == AdhocKey("https://example.com/o/r") {
		t.Error("unknown host: .git must stay part of the key")
	}
}

func TestAdhocKeyTransportAndCaseDistinct(t *testing.T) {
	keys := map[string]string{
		"http": AdhocKey("https://github.com/o/r"),
		"ssh":  AdhocKey("ssh://git@github.com/o/r"),
		"scp":  AdhocKey("git@github.com:o/r"),
	}
	seen := map[string]string{}
	for transport, key := range keys {
		if prev, ok := seen[key]; ok {
			t.Errorf("%s and %s share key %q", transport, prev, key)
		}
		seen[key] = transport
	}
	if !strings.HasPrefix(keys["scp"], "scp://") {
		t.Errorf("scp key = %q, want scp:// prefix", keys["scp"])
	}

	// Path case is significant.
	if AdhocKey("https://github.com/o/R") == AdhocKey("https://github.com/o/r") {
		t.Error("path case should change the key")
	}
	// SSH and scp canonicalise their own spelling variants.
	if AdhocKey("git@github.com:o/r.git") != AdhocKey("git@github.com:o/r/") {
		t.Error("scp .git/trailing slash should canonicalise")
	}
	if AdhocKey("ssh://git@github.com:22/o/r.git") != AdhocKey("ssh://git@github.com/o/r") {
		t.Error("ssh default port/.git should canonicalise")
	}
	// scp paths are relative to the home; ssh paths are absolute.
	if AdhocKey("git@github.com:/o/r") == AdhocKey("git@github.com:o/r") {
		t.Error("absolute and relative scp paths should differ")
	}
}

func TestAdhocKeyFileAndLocal(t *testing.T) {
	// file:// keeps .git in the key but the display name drops it.
	if AdhocKey("file:///tmp/bare.git") == AdhocKey("file:///tmp/bare") {
		t.Fatal("file:// must keep .git in the key")
	}
	if AdhocKey("file:///tmp/bare.git/") != AdhocKey("file:///tmp/bare.git") {
		t.Fatal("file:// trailing slash should canonicalise")
	}
	if got := AdhocName("file:///tmp/bare.git"); !strings.HasPrefix(got, "bare-") {
		t.Fatalf("AdhocName = %q, want bare-<hash>", got)
	}
	if AdhocName("file:///tmp/bare.git") == AdhocName("file:///tmp/bare") {
		t.Fatal("different keys must produce different names")
	}

	// Local paths use filepath.Clean and never drop .git.
	if AdhocKey("./repo") != AdhocKey("repo") {
		t.Error("local paths should be cleaned")
	}
	if AdhocKey("/tmp/repo.git") == AdhocKey("/tmp/repo") {
		t.Error("local .git must stay part of the key")
	}
}

func TestAdhocKeyFallbackIsStable(t *testing.T) {
	raw := "https://[::1/path?x=1#frag"
	key := AdhocKey(raw)
	if key == "" {
		t.Fatal("fallback key should not be empty")
	}
	if key != AdhocKey(raw) {
		t.Fatal("fallback key is not stable")
	}
	if strings.ContainsAny(key, "?#") {
		t.Fatalf("fallback key %q still has query/fragment", key)
	}
	if name := AdhocName(raw); name == "" || !strings.Contains(name, "-") {
		t.Fatalf("fallback name = %q, want <basename>-<hash>", name)
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
	// The basename comes from the canonical key: .git and trailing slash do
	// not create a second entry.
	if other := AdhocName("https://github.com/vercel-labs/agent-browser/"); other != got {
		t.Fatalf("canonical spelling produced %q, want %q", other, got)
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
	if name := AdhocName(""); name == "" {
		t.Fatal("AdhocName(\"\") should never be empty")
	}
}
