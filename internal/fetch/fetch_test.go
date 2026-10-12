package fetch

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
	sha = gitT(t, work, "rev-parse", "HEAD")
	sha = sha[:len(sha)-1] // strip newline from CombinedOutput
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

func headOf(t *testing.T, dir string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	s := string(out)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s, nil
}
