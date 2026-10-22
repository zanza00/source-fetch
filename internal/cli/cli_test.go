package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		t.Run(tt.name, func(t *testing.T) {
			if got := versionString(tt.injected, tt.bi); got != tt.want {
				t.Errorf("versionString(%q, bi) = %q, want %q", tt.injected, got, tt.want)
			}
		})
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
	gitT(t, work, "symbolic-ref", "HEAD", "refs/heads/main")
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
	if _, err := os.Stat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("rm created %s", missing)
	}
}

// runCLI runs the CLI with stdout and stderr captured.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	stdout = captureStdout(t, func() {
		stderr = captureStderr(t, func() { code = Run(args) })
	})
	return code, stdout, stderr
}

// fileURL builds a portable file:// URL for a local path.
func fileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows: C:/… → /C:/…
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// useInsteadOf rewrites an HTTPS repository URL to a local bare repo through a
// temporary global git config. It keeps deep-link CLI tests fully offline.
func useInsteadOf(t *testing.T, bare, base string) {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	body := fmt.Sprintf("[url %q]\n\tinsteadOf = %s\n\tinsteadOf = %s.git\n",
		fileURL(bare), base, base)
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func TestPeekDeepLink(t *testing.T) {
	bare := makeBareRepo(t)
	base := "https://github.com/acme/tool"
	useInsteadOf(t, bare, base)
	t.Setenv(cache.EnvCache, t.TempDir())

	code, out, stderr := runCLI(t, "peek", base+"/blob/main/file.txt")
	if code != 0 {
		t.Fatalf("peek exit code = %d: %s", code, stderr)
	}
	file := strings.TrimSpace(out)
	if got := filepath.Base(file); got != "file.txt" {
		t.Fatalf("peek printed %q, want a file.txt path", file)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "hello\n" {
		t.Fatalf("linked file content = %q, err = %v", data, err)
	}

	// Second peek of the same deep link reuses the checkout (offline fast path).
	code, out2, stderr := runCLI(t, "peek", base+"/blob/main/file.txt")
	if code != 0 {
		t.Fatalf("second peek exit code = %d: %s", code, stderr)
	}
	if strings.TrimSpace(out2) != file {
		t.Fatalf("second peek = %q, want %q", strings.TrimSpace(out2), file)
	}

	// A tree link prints the checkout directory.
	code, out3, stderr := runCLI(t, "peek", base+"/tree/main")
	if code != 0 {
		t.Fatalf("tree peek exit code = %d: %s", code, stderr)
	}
	dir := strings.TrimSpace(out3)
	if !fetch.HasGit(dir) {
		t.Fatalf("tree peek printed %q, want a checkout", dir)
	}
	if filepath.Dir(file) != dir {
		t.Fatalf("file dir = %q, want %q", filepath.Dir(file), dir)
	}
}

func TestRmCanonicalizesDeepLink(t *testing.T) {
	bare := makeBareRepo(t)
	base := "https://github.com/acme/tool"
	useInsteadOf(t, bare, base)
	t.Setenv(cache.EnvCache, t.TempDir())

	code, out, stderr := runCLI(t, "peek", base+".git")
	if code != 0 {
		t.Fatalf("peek exit code = %d: %s", code, stderr)
	}
	dir := strings.TrimSpace(out)
	if !fetch.HasGit(dir) {
		t.Fatalf("peek printed %q, want a checkout", dir)
	}

	code, _, stderr = runCLI(t, "rm", base+"/tree/main")
	if code != 0 {
		t.Fatalf("rm exit code = %d: %s", code, stderr)
	}
	if fetch.HasGit(dir) {
		t.Fatalf("rm of a deep link did not remove %s", dir)
	}
}

func TestPeekDeepLinkMissingPath(t *testing.T) {
	bare := makeBareRepo(t)
	base := "https://github.com/acme/tool"
	useInsteadOf(t, bare, base)
	t.Setenv(cache.EnvCache, t.TempDir())

	code, out, stderr := runCLI(t, "peek", base+"/blob/main/nope")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stdout = %q, want no phantom path", out)
	}
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("stderr = %q, want a not-found error", stderr)
	}
}

func TestPeekDeepLinkSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require extra privileges on Windows")
	}
	bare := makeBareRepo(t)
	base := "https://github.com/acme/tool"
	useInsteadOf(t, bare, base)
	t.Setenv(cache.EnvCache, t.TempDir())

	code, out, stderr := runCLI(t, "peek", base+"/tree/main")
	if code != 0 {
		t.Fatalf("peek exit code = %d: %s", code, stderr)
	}
	dir := strings.TrimSpace(out)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	code, out, stderr = runCLI(t, "peek", base+"/blob/main/escape")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("stdout = %q, want no path", out)
	}
	if !strings.Contains(stderr, "resolves outside the checkout") {
		t.Fatalf("stderr = %q, want a symlink-escape error", stderr)
	}
}

func TestPeekRejectsUnsupportedWebURLs(t *testing.T) {
	bare := makeBareRepo(t)
	base := "https://github.com/acme/tool"
	useInsteadOf(t, bare, base)
	cacheRoot := t.TempDir()
	t.Setenv(cache.EnvCache, cacheRoot)

	for _, link := range []string{base + "/issues/1", base + "/pulls/2"} {
		code, out, stderr := runCLI(t, "peek", link)
		if code != 1 {
			t.Fatalf("peek %s exit code = %d, want 1", link, code)
		}
		if strings.TrimSpace(out) != "" {
			t.Fatalf("peek %s printed %q", link, out)
		}
		if !strings.Contains(stderr, "unsupported GitHub URL") {
			t.Fatalf("peek %s stderr = %q", link, stderr)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(cacheRoot, "adhoc")); err == nil && len(entries) > 0 {
		t.Fatalf("unsupported URLs created cache entries: %v", entries)
	}
}

func TestPathSubpathValidation(t *testing.T) {
	bare := makeBareRepo(t)
	manifestPath := filepath.Join(t.TempDir(), manifest.CanonicalName)
	t.Setenv(manifest.EnvManifest, manifestPath)
	t.Setenv(cache.EnvCache, t.TempDir())

	code, _, stderr := runCLI(t, "manifest", "add", "tool", bare)
	if code != 0 {
		t.Fatalf("manifest add exit code = %d: %s", code, stderr)
	}

	code, out, stderr := runCLI(t, "path", "tool", "file.txt")
	if code != 0 {
		t.Fatalf("path exit code = %d: %s", code, stderr)
	}
	if got := filepath.Base(strings.TrimSpace(out)); got != "file.txt" {
		t.Fatalf("path printed %q, want a file.txt path", out)
	}

	code, out, stderr = runCLI(t, "path", "tool", "nope")
	if code != 1 {
		t.Fatalf("missing subpath exit code = %d, want 1", code)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("missing subpath printed %q", out)
	}
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("missing subpath stderr = %q", stderr)
	}

	for _, sub := range []string{"../escape", "/etc/passwd"} {
		code, out, stderr = runCLI(t, "path", "tool", sub)
		if code != 1 {
			t.Fatalf("path %q exit code = %d, want 1", sub, code)
		}
		if strings.TrimSpace(out) != "" {
			t.Fatalf("path %q printed %q", sub, out)
		}
		if !strings.Contains(stderr, "escapes the repository") {
			t.Fatalf("path %q stderr = %q", sub, stderr)
		}
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
