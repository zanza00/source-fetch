package fetch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanza00/source-fetch/internal/cache"
	"github.com/zanza00/source-fetch/internal/manifest"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// makeRepo builds a local bare repo with one commit tagged v1.2.3 and returns
// its path plus the commit SHA.
func makeRepo(t *testing.T) (bare, tag, sha string) {
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
	sha = strings.TrimSpace(gitT(t, work, "rev-parse", "HEAD"))
	bare = filepath.Join(base, "bare.git")
	gitT(t, base, "clone", "-q", "--bare", work, bare)
	return bare, "v1.2.3", sha
}

func noop(string, ...any) {}

func TestEnsureTagAndFastPath(t *testing.T) {
	bare, tag, _ := makeRepo(t)
	e := &manifest.Entry{Name: "tool", URL: bare, Transport: "git"}
	root := t.TempDir()

	dir, err := Ensure(root, e, tag, false, noop)
	if err != nil {
		t.Fatal(err)
	}
	if !HasGit(dir) {
		t.Fatalf("expected a git checkout at %s", dir)
	}
	m, ok := ReadMarker(dir)
	if !ok || m.Ref != tag {
		t.Fatalf("marker = %+v ok=%v, want ref=%s", m, ok, tag)
	}
	if !AtRef(dir, tag) {
		t.Fatal("AtRef should be true after fetch")
	}

	// Fast path: an unreachable URL must not matter when already cached.
	e2 := &manifest.Entry{Name: "tool", URL: filepath.Join(root, "does-not-exist"), Transport: "git"}
	dir2, err := Ensure(root, e2, tag, false, noop)
	if err != nil {
		t.Fatalf("fast path should not touch the network: %v", err)
	}
	if dir2 != dir {
		t.Fatalf("dir2 = %s, want %s", dir2, dir)
	}
}

func TestEnsureByCommitSHA(t *testing.T) {
	bare, _, sha := makeRepo(t)
	e := &manifest.Entry{Name: "tool", URL: bare, Transport: "git"}
	root := t.TempDir()

	dir, err := Ensure(root, e, sha, false, noop)
	if err != nil {
		t.Fatal(err)
	}
	got, err := headOf(t, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != sha {
		t.Fatalf("HEAD = %s, want %s", got, sha)
	}
}

func TestEnsureUnknownRefFallsBack(t *testing.T) {
	bare, _, _ := makeRepo(t)
	e := &manifest.Entry{Name: "tool", URL: bare, Transport: "git"}
	root := t.TempDir()

	var warned bool
	dir, err := Ensure(root, e, "no-such-ref", false, func(format string, args ...any) {
		if format == "ref '%s' not found, using default branch" {
			warned = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !HasGit(dir) {
		t.Fatal("expected fallback checkout")
	}
	if !warned {
		t.Fatal("expected a fallback warning")
	}
}

func TestAtRefEmptyAlwaysTrue(t *testing.T) {
	if !AtRef(t.TempDir(), "") {
		t.Fatal("empty ref should always be at-ref")
	}
}

func TestEnsureRejectsDashRefAndURL(t *testing.T) {
	root := t.TempDir()
	valid := &manifest.Entry{Name: "tool", URL: "https://example.com/tool.git", Transport: "git"}
	if _, err := Ensure(root, valid, "--upload-pack=evil", false, noop); err == nil {
		t.Fatal("expected error for ref starting with '-'")
	}
	badURL := &manifest.Entry{Name: "tool", URL: "--upload-pack=evil", Transport: "git"}
	if _, err := Ensure(root, badURL, "", false, noop); err == nil {
		t.Fatal("expected error for url starting with '-'")
	}
}

func TestReadMarkerInvalidTimestamp(t *testing.T) {
	dir := t.TempDir()
	body := "ref=v1.0.0\ncommit=abc123\nfetched=not-a-number\n"
	if err := os.WriteFile(cache.Marker(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, ok := ReadMarker(dir)
	if !ok {
		t.Fatal("expected ReadMarker to succeed")
	}
	if m.Ref != "v1.0.0" || m.Commit != "abc123" || m.Fetched != 0 {
		t.Fatalf("Marker = %+v, want Ref=v1.0.0 Commit=abc123 Fetched=0", m)
	}
}

func headOf(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
}
