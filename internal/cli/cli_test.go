package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/zanza00/source-fetch/internal/cache"
	"github.com/zanza00/source-fetch/internal/fetch"
)

// captureStdout runs f with os.Stdout redirected and returns what was printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = old
	b, err := io.ReadAll(r)
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
