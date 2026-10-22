package fetch

import (
	"net/url"
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

func noop(string, ...any) {}

// fileURL builds a portable file:// URL, the only way to make `git clone
// --depth` honor the depth for a local repository.
func fileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows: C:/… → /C:/…
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
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

// historyRepo is an offline fixture with enough history to test depth.
type historyRepo struct {
	url     string
	bare    string
	tag     string // tag on an intermediate commit
	tagSHA  string
	tipSHA  string // tip of main
	sideSHA string // tip of a side branch forked from tagSHA
}

// makeHistoryRepo builds a repo with six commits on main, a tag on the third
// commit and a two-commit side branch. The returned URL is file:// so shallow
// clones actually happen.
func makeHistoryRepo(t *testing.T) historyRepo {
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
	var shas []string
	for i := 1; i <= 6; i++ {
		name := filepath.Join(work, "file"+string(rune('0'+i))+".txt")
		if err := os.WriteFile(name, []byte("line\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitT(t, work, "add", ".")
		gitT(t, work, "commit", "-q", "-m", "commit "+string(rune('0'+i)))
		shas = append(shas, strings.TrimSpace(gitT(t, work, "rev-parse", "HEAD")))
	}
	tagSHA := shas[2]
	gitT(t, work, "tag", "v1.0.0", tagSHA)

	// Side branch forked from the tag commit, not reachable from main's tip.
	gitT(t, work, "checkout", "-q", "-b", "side", tagSHA)
	for i := 1; i <= 2; i++ {
		name := filepath.Join(work, "side"+string(rune('0'+i))+".txt")
		if err := os.WriteFile(name, []byte("side\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitT(t, work, "add", ".")
		gitT(t, work, "commit", "-q", "-m", "side "+string(rune('0'+i)))
	}
	sideSHA := strings.TrimSpace(gitT(t, work, "rev-parse", "HEAD"))
	gitT(t, work, "checkout", "-q", "main")

	bare := filepath.Join(base, "bare.git")
	gitT(t, base, "clone", "-q", "--bare", work, bare)
	return historyRepo{
		url:     fileURL(bare),
		bare:    bare,
		tag:     "v1.0.0",
		tagSHA:  tagSHA,
		tipSHA:  shas[5],
		sideSHA: sideSHA,
	}
}

func ensureAt(t *testing.T, root string, e *manifest.Entry, ref string, depth int) string {
	t.Helper()
	dir, err := Ensure(root, e, ref, EnsureOpts{Depth: depth, Progress: noop})
	if err != nil {
		t.Fatalf("Ensure(ref=%q depth=%d): %v", ref, depth, err)
	}
	return dir
}

func assertHistory(t *testing.T, dir string, wantShallow bool, wantCommits int) {
	t.Helper()
	if got := IsShallow(dir); got != wantShallow {
		t.Fatalf("IsShallow(%s) = %v, want %v", dir, got, wantShallow)
	}
	got, err := commitCount(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantCommits {
		t.Fatalf("commit count = %d, want %d", got, wantCommits)
	}
}

func TestEnsureTagAndFastPath(t *testing.T) {
	bare, tag, _ := makeRepo(t)
	e := &manifest.Entry{Name: "tool", URL: bare, Transport: "git"}
	root := t.TempDir()

	dir, err := Ensure(root, e, tag, EnsureOpts{Progress: noop})
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
	dir2, err := Ensure(root, e2, tag, EnsureOpts{Progress: noop})
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

	dir, err := Ensure(root, e, sha, EnsureOpts{Progress: noop})
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
	dir, err := Ensure(root, e, "no-such-ref", EnsureOpts{Progress: func(format string, args ...any) {
		if format == "ref '%s' not found, using default branch" {
			warned = true
		}
	}})
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
	if _, err := Ensure(root, valid, "--upload-pack=evil", EnsureOpts{Progress: noop}); err == nil {
		t.Fatal("expected error for ref starting with '-'")
	}
	badURL := &manifest.Entry{Name: "tool", URL: "--upload-pack=evil", Transport: "git"}
	if _, err := Ensure(root, badURL, "", EnsureOpts{Progress: noop}); err == nil {
		t.Fatal("expected error for url starting with '-'")
	}
}

func TestEnsureRejectsInvalidDepth(t *testing.T) {
	e := &manifest.Entry{Name: "tool", URL: "https://example.com/tool.git", Transport: "git"}
	for _, depth := range []int{-2, -10} {
		if _, err := Ensure(t.TempDir(), e, "", EnsureOpts{Depth: depth, Progress: noop}); err == nil {
			t.Fatalf("expected an error for depth %d", depth)
		}
	}
}

func TestEnsureDepthDefault(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	// The zero value means a shallow clone one commit deep.
	dir := ensureAt(t, root, e, "", 0)
	assertHistory(t, dir, true, 1)
	m, ok := ReadMarker(dir)
	if !ok || m.Depth != DepthDefault {
		t.Fatalf("marker depth = %+v ok=%v, want %d", m, ok, DepthDefault)
	}
}

func TestEnsureDepthThreeInitial(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", 3)
	assertHistory(t, dir, true, 3)
	if m, ok := ReadMarker(dir); !ok || m.Depth != 3 {
		t.Fatalf("marker = %+v ok=%v, want depth 3", m, ok)
	}
}

func TestEnsureDeepInitial(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", DepthFull)
	assertHistory(t, dir, false, 6)
	if m, ok := ReadMarker(dir); !ok || m.Depth != DepthFull {
		t.Fatalf("marker = %+v ok=%v, want depth full", m, ok)
	}
}

func TestEnsureUpgradeOneToThree(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", 1)
	head := gitT(t, dir, "rev-parse", "HEAD")
	upgraded := ensureAt(t, root, e, "", 3)
	if upgraded != dir {
		t.Fatalf("upgrade moved the checkout: %s != %s", upgraded, dir)
	}
	assertHistory(t, upgraded, true, 3)
	if got := gitT(t, upgraded, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD changed during upgrade: %s != %s", strings.TrimSpace(got), strings.TrimSpace(head))
	}
	if m, ok := ReadMarker(upgraded); !ok || m.Depth != 3 {
		t.Fatalf("marker = %+v ok=%v, want depth 3", m, ok)
	}
}

func TestEnsureDepthThreeToTwoNoop(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", 3)
	head := gitT(t, dir, "rev-parse", "HEAD")
	// A shallower request must never reduce history.
	if got := ensureAt(t, root, e, "", 2); got != dir {
		t.Fatalf("no-op moved the checkout")
	}
	assertHistory(t, dir, true, 3)
	if got := gitT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD changed during no-op")
	}
}

func TestEnsureUpgradeOneToFull(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", 1)
	ensureAt(t, root, e, "", DepthFull)
	assertHistory(t, dir, false, 6)
	if m, ok := ReadMarker(dir); !ok || m.Depth != DepthFull {
		t.Fatalf("marker = %+v ok=%v, want depth full", m, ok)
	}
}

func TestEnsureFullToDepthOneNoop(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", DepthFull)
	ensureAt(t, root, e, "", 1)
	assertHistory(t, dir, false, 6)
}

func TestEnsureTagUpgrade(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, repo.tag, 1)
	if got := strings.TrimSpace(gitT(t, dir, "rev-parse", "HEAD")); got != repo.tagSHA {
		t.Fatalf("HEAD = %s, want tag SHA %s", got, repo.tagSHA)
	}
	ensureAt(t, root, e, repo.tag, 3)
	assertHistory(t, dir, true, 3)
	if got := strings.TrimSpace(gitT(t, dir, "rev-parse", "HEAD")); got != repo.tagSHA {
		t.Fatalf("HEAD = %s, want tag SHA %s", got, repo.tagSHA)
	}
}

func TestEnsureSHAUpgrade(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, repo.tipSHA, 1)
	ensureAt(t, root, e, repo.tipSHA, 3)
	assertHistory(t, dir, true, 3)
	if got := strings.TrimSpace(gitT(t, dir, "rev-parse", "HEAD")); got != repo.tipSHA {
		t.Fatalf("HEAD = %s, want %s", got, repo.tipSHA)
	}
}

func TestEnsureDetachedTagEmptyRefUpgrade(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, repo.tag, 1)
	// An empty ref is always "at ref", but the upgrade must follow the
	// detached tag, not the remote default branch.
	ensureAt(t, root, e, "", 3)
	assertHistory(t, dir, true, 3)
	if got := strings.TrimSpace(gitT(t, dir, "rev-parse", "HEAD")); got != repo.tagSHA {
		t.Fatalf("HEAD = %s, want tag SHA %s", got, repo.tagSHA)
	}
}

func TestEnsureDetachedSHAEmptyRefUpgrade(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, repo.sideSHA, 1)
	ensureAt(t, root, e, "", 3)
	// The side branch tip is not the default branch tip; using the default
	// branch would fetch unrelated history or fail to satisfy the depth.
	assertHistory(t, dir, true, 3)
	if got := strings.TrimSpace(gitT(t, dir, "rev-parse", "HEAD")); got != repo.sideSHA {
		t.Fatalf("HEAD = %s, want side SHA %s", got, repo.sideSHA)
	}
}

func TestEnsureAttachedBranchEmptyRefUpgrade(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", 1)
	if branch := strings.TrimSpace(gitT(t, dir, "symbolic-ref", "--short", "HEAD")); branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}
	ensureAt(t, root, e, "", 3)
	assertHistory(t, dir, true, 3)
	if branch := strings.TrimSpace(gitT(t, dir, "symbolic-ref", "--short", "HEAD")); branch != "main" {
		t.Fatalf("branch = %q, want main after upgrade", branch)
	}
}

func TestEnsureFailedUpgradePreservesCheckout(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", 1)
	head := gitT(t, dir, "rev-parse", "HEAD")

	// deepen uses the origin remote, so break that instead of e.URL.
	if err := os.Rename(repo.bare, repo.bare+"-gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(root, e, "", EnsureOpts{Depth: 3, Progress: noop}); err == nil {
		t.Fatal("expected the upgrade to fail with an unreachable origin")
	}
	if !HasGit(dir) {
		t.Fatal("failed upgrade destroyed the checkout")
	}
	assertHistory(t, dir, true, 1)
	if got := gitT(t, dir, "rev-parse", "HEAD"); got != head {
		t.Fatalf("HEAD changed after a failed upgrade: %s != %s", strings.TrimSpace(got), strings.TrimSpace(head))
	}
	// A subsequent forced update still works and is the documented recovery.
	if err := os.Rename(repo.bare+"-gone", repo.bare); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(root, e, "", EnsureOpts{Force: true, Depth: 3, Progress: noop}); err != nil {
		t.Fatalf("forced re-clone after failure: %v", err)
	}
	assertHistory(t, dir, true, 3)
}

func TestReadMarkerOldWithoutDepth(t *testing.T) {
	dir := t.TempDir()
	body := "ref=v1.0.0\ncommit=abc123\nfetched=1700000000\n"
	if err := os.WriteFile(cache.Marker(dir), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, ok := ReadMarker(dir)
	if !ok {
		t.Fatal("expected ReadMarker to succeed")
	}
	if m.Ref != "v1.0.0" || m.Commit != "abc123" || m.Fetched != 1700000000 || m.Depth != 0 {
		t.Fatalf("Marker = %+v, want old fields and Depth=0", m)
	}
}

func TestReadMarkerDepth(t *testing.T) {
	tests := []struct {
		line string
		want int
	}{
		{"depth=full", DepthFull},
		{"depth=7", 7},
		{"depth=bad", 0},
		{"depth=0", 0},
		{"depth=-3", 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			dir := t.TempDir()
			body := "ref=v1.0.0\ncommit=abc123\nfetched=1\n" + tt.line + "\n"
			if err := os.WriteFile(cache.Marker(dir), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			m, ok := ReadMarker(dir)
			if !ok {
				t.Fatal("expected ReadMarker to succeed")
			}
			if m.Depth != tt.want {
				t.Fatalf("Depth = %d, want %d", m.Depth, tt.want)
			}
		})
	}
}

func TestDepthSatisfiedUsesGitState(t *testing.T) {
	repo := makeHistoryRepo(t)
	e := &manifest.Entry{Name: "tool", URL: repo.url, Transport: "git"}
	root := t.TempDir()

	dir := ensureAt(t, root, e, "", 3)
	// A stale marker must not make a shallow checkout look full.
	if !depthSatisfied(dir, 3) || depthSatisfied(dir, 4) {
		t.Fatalf("depthSatisfied disagrees with a shallow depth-3 checkout")
	}
	ensureAt(t, root, e, "", DepthFull)
	if err := os.WriteFile(cache.Marker(dir), []byte("ref=\ncommit=x\nfetched=1\ndepth=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !depthSatisfied(dir, DepthFull) || !depthSatisfied(dir, 10) {
		t.Fatal("a complete checkout must satisfy every depth regardless of the marker")
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
