package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/zanza00/source-fetch/internal/cache"
	"github.com/zanza00/source-fetch/internal/fetch"
	"github.com/zanza00/source-fetch/internal/manifest"
)

// captureStdout runs f with os.Stdout redirected and returns what was
// printed. It writes to a temp file instead of an os.Pipe: a pipe with no
// concurrent reader deadlocks on Windows once the output exceeds the small
// pipe buffer (the embedded core SKILL.md does).
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	path := filepath.Join(t.TempDir(), "stdout")
	tmp, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = tmp
	f()
	_ = tmp.Close()
	os.Stdout = old
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestVersionCommand(t *testing.T) {
	var code int
	out := captureStdout(t, func() { code = Run([]string{"version"}) })
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out); got != currentVersion() {
		t.Fatalf("version output = %q, want %q", got, currentVersion())
	}
}

func TestVersionString(t *testing.T) {
	tests := []struct {
		name     string
		injected string
		bi       *debug.BuildInfo
		want     string
	}{{
		name:     "ldflags injected wins",
		injected: "v0.3.0",
		bi:       &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}},
		want:     "v0.3.0",
	}, {
		name:     "fallback to module version when dev",
		injected: "dev",
		bi:       &debug.BuildInfo{Main: debug.Module{Version: "v0.3.0"}},
		want:     "v0.3.0",
	}, {
		name:     "local build stays dev",
		injected: "dev",
		bi:       &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
		want:     "dev",
	}, {
		name:     "no build info stays dev",
		injected: "dev",
		bi:       nil,
		want:     "dev",
	}, {
		name:     "empty module version stays dev",
		injected: "dev",
		bi:       &debug.BuildInfo{},
		want:     "dev",
	}}
	for _, tt := range tests {
		if got := versionString(tt.injected, tt.bi); got != tt.want {
			t.Errorf("%s: versionString(%q, bi) = %q, want %q", tt.name, tt.injected, got, tt.want)
		}
	}
}

func TestUnknownOptionFails(t *testing.T) {
	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	code := Run([]string{"--nope"})
	_ = w.Close()
	os.Stderr = old
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

// silenceStderr runs f with os.Stderr discarded (progress goes there).
func silenceStderr(t *testing.T, f func()) {
	t.Helper()
	old := os.Stderr
	_, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	f()
	_ = w.Close()
	os.Stderr = old
}

func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	tmp, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = tmp
	defer func() {
		os.Stderr = old
		_ = tmp.Close()
	}()
	f()
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

// makeBareRepo builds a local bare repo with one commit tagged v1.2.3 and
// returns its path.
func makeBareRepo(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, work, "init", "-q")
	gitT(t, work, "config", "user.email", "t@example.com")
	gitT(t, work, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, work, "add", ".")
	gitT(t, work, "commit", "-q", "-m", "init")
	gitT(t, work, "tag", "v1.2.3")
	bare := filepath.Join(base, "bare.git")
	gitT(t, base, "clone", "-q", "--bare", work, bare)
	return bare
}

func TestPeekFetchListRm(t *testing.T) {
	url := "file://" + makeBareRepo(t)
	t.Setenv(cache.EnvCache, t.TempDir())

	var code int
	out := captureStdout(t, func() {
		silenceStderr(t, func() { code = Run([]string{"peek", url}) })
	})
	if code != 0 {
		t.Fatalf("peek exit code = %d, want 0", code)
	}
	dir := strings.TrimSpace(out)
	if !fetch.HasGit(dir) {
		t.Fatalf("expected a checkout at %s", dir)
	}

	// Fast path: a second peek prints the same directory.
	out2 := captureStdout(t, func() {
		silenceStderr(t, func() { code = Run([]string{"peek", url}) })
	})
	if code != 0 || strings.TrimSpace(out2) != dir {
		t.Fatalf("second peek: code=%d out=%q, want %q", code, out2, dir)
	}

	// Listing shows the tree with its remote URL.
	var listOut string
	listOut = captureStdout(t, func() {
		silenceStderr(t, func() { code = Run([]string{"peek"}) })
	})
	if code != 0 || !strings.Contains(listOut, url) {
		t.Fatalf("peek list: code=%d out=%q, want URL %q", code, listOut, url)
	}

	// rm by URL drops the ad-hoc tree.
	silenceStderr(t, func() { code = Run([]string{"rm", url}) })
	if code != 0 {
		t.Fatalf("rm exit code = %d, want 0", code)
	}
	if fetch.HasGit(dir) {
		t.Fatalf("rm did not delete %s", dir)
	}
}

func TestPeekWithRef(t *testing.T) {
	url := "file://" + makeBareRepo(t)
	t.Setenv(cache.EnvCache, t.TempDir())

	var code int
	out := captureStdout(t, func() {
		silenceStderr(t, func() { code = Run([]string{"peek", url, "v1.2.3"}) })
	})
	if code != 0 {
		t.Fatalf("peek exit code = %d, want 0", code)
	}
	m, ok := fetch.ReadMarker(strings.TrimSpace(out))
	if !ok || m.Ref != "v1.2.3" {
		t.Fatalf("marker = %+v ok=%v, want ref=v1.2.3", m, ok)
	}
}

func TestPeekListEmpty(t *testing.T) {
	t.Setenv(cache.EnvCache, t.TempDir())
	var code int
	out := captureStdout(t, func() {
		silenceStderr(t, func() { code = Run([]string{"peek"}) })
	})
	if code != 0 {
		t.Fatalf("peek exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "NAME") {
		t.Fatalf("empty peek list should still print a header, got %q", out)
	}
}

func TestManifestAddSetRm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, manifest.CanonicalName)
	t.Setenv(manifest.EnvManifest, path)

	run := func(wantCode int, args ...string) {
		t.Helper()
		var code int
		silenceStderr(t, func() { code = Run(args) })
		if code != wantCode {
			t.Fatalf("Run(%v) exit code = %d, want %d", args, code, wantCode)
		}
	}

	// add creates the manifest and the entry.
	run(0, "manifest", "add", "chezmoi", "https://example.com/chezmoi.git",
		"--version-cmd", "chezmoi --version", "--tag-prefix", "v")
	m, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := m.Lookup("chezmoi")
	if !ok || e.URL != "https://example.com/chezmoi.git" || e.VersionCmd != "chezmoi --version" || e.TagPrefix != "v" {
		t.Fatalf("add wrote wrong entry: %+v", e)
	}

	// a second add without --force fails; with --force it overwrites.
	run(1, "manifest", "add", "chezmoi", "https://example.com/other.git")
	run(0, "manifest", "add", "chezmoi", "https://example.com/other.git", "--force", "--ref", "v2.0.0")
	m, _ = manifest.Load(path)
	e, _ = m.Lookup("chezmoi")
	if e.URL != "https://example.com/other.git" || e.Ref != "v2.0.0" || e.VersionCmd != "" {
		t.Fatalf("force add did not overwrite: %+v", e)
	}

	// set updates single fields; an empty value clears.
	run(0, "manifest", "set", "chezmoi", "version_cmd=chezmoi --version", "ref=v2.73.0")
	m, _ = manifest.Load(path)
	e, _ = m.Lookup("chezmoi")
	if e.VersionCmd != "chezmoi --version" || e.Ref != "v2.73.0" {
		t.Fatalf("set wrote wrong entry: %+v", e)
	}
	run(0, "manifest", "set", "chezmoi", "ref=")
	m, _ = manifest.Load(path)
	e, _ = m.Lookup("chezmoi")
	if e.Ref != "" {
		t.Fatalf("set with empty value did not clear: %+v", e)
	}

	// failures: unknown key, unknown source, unknown flag.
	run(1, "manifest", "set", "chezmoi", "nope=x")
	run(1, "manifest", "set", "ghost", "ref=v1")
	run(1, "manifest", "add", "x", "https://example.com/x.git", "--nope")

	// bare manifest still prints the path.
	var code int
	out := captureStdout(t, func() {
		silenceStderr(t, func() { code = Run([]string{"manifest"}) })
	})
	if code != 0 || strings.TrimSpace(out) != path {
		t.Fatalf("manifest: code=%d out=%q, want %q", code, out, path)
	}
	run(1, "manifest", "bogus")

	// rm drops the entry, leaving a valid manifest.
	run(0, "manifest", "rm", "chezmoi")
	m, err = manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Lookup("chezmoi"); ok {
		t.Fatal("manifest rm did not remove the entry")
	}
	run(1, "manifest", "rm", "chezmoi")

	// rm on a manifest that does not exist fails without creating it.
	missing := filepath.Join(t.TempDir(), "none.toml")
	t.Setenv(manifest.EnvManifest, missing)
	run(1, "manifest", "rm", "x")
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("rm created %s", missing)
	}
}

func TestManifestRemovalWorkflows(t *testing.T) {
	bare := makeBareRepo(t)
	for _, removeCache := range []bool{false, true} {
		name := "keep checkout"
		if removeCache {
			name = "remove checkout first"
		}
		t.Run(name, func(t *testing.T) {
			manifestPath := filepath.Join(t.TempDir(), manifest.CanonicalName)
			root := t.TempDir()
			t.Setenv(manifest.EnvManifest, manifestPath)
			t.Setenv(cache.EnvCache, root)
			run := func(args ...string) string {
				t.Helper()
				var code int
				var stderr string
				captureStdout(t, func() {
					stderr = captureStderr(t, func() { code = Run(args) })
				})
				if code != 0 {
					t.Fatalf("Run(%v) = %d: %s", args, code, stderr)
				}
				return stderr
			}
			run("manifest", "add", "tool", bare)
			run("path", "tool")
			if removeCache {
				run("rm", "tool")
			}
			stderr := run("manifest", "rm", "tool")
			if !strings.Contains(stderr, "cache untouched") || strings.Contains(stderr, "use rm") {
				t.Fatalf("manifest rm gives misleading cache advice: %q", stderr)
			}
			m, err := manifest.Load(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := m.Lookup("tool"); ok {
				t.Fatal("manifest entry remains after removal")
			}
			if got := fetch.HasGit(cache.Dir(root, "tool")); got != !removeCache {
				t.Fatalf("checkout exists = %v, want %v", got, !removeCache)
			}
		})
	}
}
