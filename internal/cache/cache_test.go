package cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitInit makes dir a git work tree and returns its toplevel.
func gitInit(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	top, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(top))
}

func TestGuardCleanTarget(t *testing.T) {
	home, _ := os.UserHomeDir()

	for _, bad := range []string{"", string(filepath.Separator), ".", home} {
		if err := GuardCleanTarget(bad, ""); err == nil {
			t.Errorf("GuardCleanTarget(%q) should fail", bad)
		}
	}
	if err := GuardCleanTarget(filepath.Join(t.TempDir(), "cache"), ""); err != nil {
		t.Errorf("normal path should pass: %v", err)
	}
}

func TestGuardCleanTargetProtectsTheProject(t *testing.T) {
	repo := gitInit(t, t.TempDir())
	manifestPath := filepath.Join(repo, ".source-fetch.toml")

	// The repository root is never cleaned.
	if err := GuardCleanTarget(repo, manifestPath); err == nil {
		t.Error("cleaning the repository root should fail")
	}
	// A root that holds the manifest is never cleaned.
	root := filepath.Join(repo, "cache")
	if err := GuardCleanTarget(root, filepath.Join(root, ".source-fetch.toml")); err == nil {
		t.Error("cleaning a root that holds the manifest should fail")
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

func TestResolveHonoursEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "override")
	t.Setenv(EnvCache, override)
	got, err := Resolve(filepath.Join(t.TempDir(), ".source-fetch.toml"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(override)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != want {
		t.Fatalf("Resolve = %s, want %s", got.Path, want)
	}
	if got.Source != SourceEnv {
		t.Fatalf("source = %s, want %s", got.Source, SourceEnv)
	}
}

func TestResolveOverrideFlagWins(t *testing.T) {
	t.Setenv(EnvCache, filepath.Join(t.TempDir(), "env"))
	override := filepath.Join(t.TempDir(), "flag")
	got, err := Resolve(filepath.Join(t.TempDir(), ".source-fetch.toml"), ".source-fetch", override)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != override || got.Source != SourceOverride {
		t.Fatalf("Resolve = %+v, want the override %s", got, override)
	}
}

func TestResolveUsesManifestCacheDir(t *testing.T) {
	repo := gitInit(t, t.TempDir())
	t.Chdir(repo)
	got, err := Resolve(filepath.Join(repo, ".source-fetch.toml"), "vendor/src", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repo, "vendor", "src")
	if got.Path != want {
		t.Fatalf("Resolve = %s, want %s", got.Path, want)
	}
	if got.Source != SourceManifest {
		t.Fatalf("source = %s, want %s", got.Source, SourceManifest)
	}
}

func TestResolveDefaultsToProjectLocal(t *testing.T) {
	repo := gitInit(t, t.TempDir())
	t.Chdir(repo)
	got, err := Resolve(filepath.Join(repo, ".source-fetch.toml"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repo, LocalDirName)
	if got.Path != want {
		t.Fatalf("Resolve = %s, want %s", got.Path, want)
	}
	if !got.Local || got.Source != SourceProject {
		t.Fatalf("Resolve = %+v, want a local project cache", got)
	}
}

func TestResolveFollowsTheCurrentDirectory(t *testing.T) {
	// The manifest lives elsewhere (the per-user fallback), the work
	// happens in a repository: the cache belongs to the repository.
	repo := gitInit(t, t.TempDir())
	t.Chdir(repo)
	manifestPath := filepath.Join(t.TempDir(), "manifest.toml")

	got, err := Resolve(manifestPath, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(repo, LocalDirName); got.Path != want {
		t.Fatalf("Resolve = %s, want %s", got.Path, want)
	}
}

func TestResolveOutsideARepoUsesTheUserCache(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	dir := t.TempDir()
	t.Chdir(dir)
	path := filepath.Join(dir, ".source-fetch.toml")
	got, err := Resolve(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "source-fetch", ManifestID(path))
	if got.Path != want {
		t.Fatalf("Resolve = %s, want %s", got.Path, want)
	}
	if got.Local {
		t.Fatalf("Resolve = %+v, want a namespaced user cache", got)
	}
	// The ad-hoc trees are shared by every manifest.
	adhoc, err := ResolveAdhoc(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "source-fetch", "adhoc"); adhoc.Path != want {
		t.Fatalf("ResolveAdhoc = %s, want %s", adhoc.Path, want)
	}
}

func TestResolveAdhocHonoursEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "override")
	t.Setenv(EnvCache, override)
	got, err := ResolveAdhoc(filepath.Join(t.TempDir(), ".source-fetch.toml"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(override)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(abs, "adhoc"); got.Path != want {
		t.Fatalf("ResolveAdhoc = %s, want %s", got.Path, want)
	}
}

func TestEnsureIgnoredAddsTheExcludeRule(t *testing.T) {
	repo := gitInit(t, t.TempDir())
	root := filepath.Join(repo, LocalDirName)

	res, err := EnsureIgnored(root)
	if err != nil {
		t.Fatal(err)
	}
	if res != IgnoreAdded {
		t.Fatalf("EnsureIgnored = %s, want %s", res, IgnoreAdded)
	}

	// The rule lives in .git/info/exclude: the tracked files stay untouched.
	exclude := filepath.Join(repo, ".git", "info", "exclude")
	body, err := os.ReadFile(exclude)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "/"+LocalDirName) {
		t.Fatalf("exclude file has no rule:\n%s", body)
	}

	// The path is ignored now, and the rule is not written twice.
	res, err = EnsureIgnored(root)
	if err != nil {
		t.Fatal(err)
	}
	if res != IgnoreAlready {
		t.Fatalf("EnsureIgnored = %s, want %s", res, IgnoreAlready)
	}
}

func TestEnsureIgnoredOutsideARepo(t *testing.T) {
	res, err := EnsureIgnored(filepath.Join(t.TempDir(), LocalDirName))
	if err != nil {
		t.Fatal(err)
	}
	if res != IgnoreOutside {
		t.Fatalf("EnsureIgnored = %s, want %s", res, IgnoreOutside)
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
