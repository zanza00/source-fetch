// Package fetch implements the fetch algorithm and the at-ref fast path (§7).
package fetch

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zanza00/source-fetch/internal/cache"
	"github.com/zanza00/source-fetch/internal/git"
	"github.com/zanza00/source-fetch/internal/manifest"
)

const (
	// DepthDefault is the shallow depth used when no flag is given.
	DepthDefault = 1
	// DepthFull requests complete history (`--deep`).
	DepthFull = -1
)

// Marker records what a cached tree holds.
type Marker struct {
	Ref     string
	Commit  string
	Fetched int64
	// Depth is the recorded history depth: DepthFull for a complete clone,
	// a positive number for a shallow one, and 0 when the marker predates
	// depth tracking or carries an invalid value.
	Depth int
}

// Progress receives human-readable progress messages (always stderr-bound).
type Progress func(format string, args ...any)

// EnsureOpts controls a fetch. The zero value means a shallow clone one commit
// deep without forcing.
type EnsureOpts struct {
	Force    bool
	Depth    int // 0 = DepthDefault; DepthFull or >= 1
	Progress Progress
}

// Ensure makes sure the entry is cached at ref and returns its directory.
//
// Fast path: an existing checkout already at ref with enough history is
// returned without touching the network. A shallower checkout is deepened in
// place; a failed upgrade leaves it intact. force re-clones unconditionally
// (used by `update`).
func Ensure(root string, e *manifest.Entry, ref string, opts EnsureOpts) (string, error) {
	if err := e.Validate(); err != nil {
		return "", err
	}
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("source %q: invalid ref %q (must not start with '-')", e.Name, ref)
	}
	depth := opts.Depth
	if depth == 0 {
		depth = DepthDefault
	}
	if depth < DepthFull {
		return "", fmt.Errorf("source %q: invalid depth %d (want %d, or >= 1)", e.Name, depth, DepthFull)
	}
	progress := opts.Progress
	if progress == nil {
		progress = func(string, ...any) {}
	}
	dir := cache.Dir(root, e.Name)

	if !opts.Force && HasGit(dir) && AtRef(dir, ref) {
		if depthSatisfied(dir, depth) {
			return dir, nil
		}
		if err := deepen(dir, depth, progress); err != nil {
			return "", fmt.Errorf("cannot deepen cached checkout %s: %w", dir, err)
		}
		if err := WriteMarker(dir, ref, depth); err != nil {
			return "", err
		}
		return dir, nil
	}

	if _, err := os.Stat(dir); err == nil {
		progress("refreshing %s", e.Name)
		if err := os.RemoveAll(dir); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	if ref == "" {
		if _, err := git.Run("", cloneArgs(e.URL, dir, "", depth)...); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	} else {
		if _, err := git.Run("", cloneArgs(e.URL, dir, ref, depth)...); err != nil {
			if err := fetchCommit(dir, e.URL, ref, depth, progress); err != nil {
				_ = os.RemoveAll(dir)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", err
				}
				progress("ref '%s' not found, using default branch", ref)
				if _, err := git.Run("", cloneArgs(e.URL, dir, "", depth)...); err != nil {
					_ = os.RemoveAll(dir)
					return "", err
				}
			}
		}
	}

	if err := WriteMarker(dir, ref, depth); err != nil {
		return "", err
	}
	return dir, nil
}

// cloneArgs builds `git clone` arguments, omitting --depth for full history.
func cloneArgs(url, dir, ref string, depth int) []string {
	args := []string{"clone", "-q"}
	args = append(args, depthArgs(depth)...)
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	return append(args, "--", url, dir)
}

// depthArgs returns the shallow-clone arguments for depth, or nil for full.
func depthArgs(depth int) []string {
	if depth == DepthFull {
		return nil
	}
	return []string{"--depth", strconv.Itoa(depth)}
}

// fetchCommit treats ref as a commit SHA: fresh init, fetch the SHA, detach.
func fetchCommit(dir, url, ref string, depth int, progress Progress) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := git.Run(dir, "init", "-q"); err != nil {
		return err
	}
	if _, err := git.Run(dir, "remote", "add", "--", "origin", url); err != nil {
		return err
	}
	args := append([]string{"fetch", "-q"}, depthArgs(depth)...)
	args = append(args, "origin", ref)
	if _, err := git.Run(dir, args...); err != nil {
		return err
	}
	if _, err := git.Run(dir, "checkout", "-q", "--detach", "FETCH_HEAD"); err != nil {
		return err
	}
	return nil
}

// IsShallow reports whether dir is a shallow repository.
func IsShallow(dir string) bool {
	out, err := git.Run(dir, "rev-parse", "--is-shallow-repository")
	if err == nil {
		return out == "true"
	}
	_, err = os.Stat(filepath.Join(dir, ".git", "shallow"))
	return err == nil
}

// depthSatisfied reports whether dir already has enough history for want.
//
// A complete cache satisfies every request: history is never reduced. The
// marker is deliberately ignored; the real git state is authoritative.
func depthSatisfied(dir string, want int) bool {
	if want == DepthFull {
		return !IsShallow(dir)
	}
	if want <= DepthDefault {
		return true
	}
	if !IsShallow(dir) {
		return true
	}
	n, err := commitCount(dir)
	return err == nil && n >= want
}

// commitCount returns the number of commits reachable from HEAD.
func commitCount(dir string) (int, error) {
	out, err := git.Run(dir, "rev-list", "--count", "HEAD")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("cannot parse commit count %q: %w", out, err)
	}
	return n, nil
}

// deepen grows the history of an existing checkout in place. It never removes
// dir: on failure the caller keeps the previous checkout.
func deepen(dir string, want int, progress Progress) error {
	target := deepenTarget(dir)
	args := []string{"fetch", "-q"}
	if want == DepthFull {
		args = append(args, "--unshallow")
	} else {
		args = append(args, "--depth", strconv.Itoa(want))
	}
	args = append(args, "origin", target)
	if want == DepthFull {
		progress("deepening %s to full history", dir)
	} else {
		progress("deepening %s to %d commits", dir, want)
	}
	if _, err := git.Run(dir, args...); err != nil {
		return err
	}
	if !depthSatisfied(dir, want) {
		return fmt.Errorf("history is still shallower than requested (want %s)", depthName(want))
	}
	return nil
}

// deepenTarget derives the refspec from the current checkout: an attached
// branch name, else its HEAD commit, else HEAD. The requested ref may be empty
// (default branch) or may not match the cached commit, so it is not used.
func deepenTarget(dir string) string {
	if branch, err := git.Run(dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && branch != "" {
		return branch
	}
	if sha, err := git.HeadCommit(dir); err == nil && sha != "" {
		return sha
	}
	return "HEAD"
}

func depthName(depth int) string {
	if depth == DepthFull {
		return "full"
	}
	return strconv.Itoa(depth)
}

// HasGit reports whether dir is a git checkout.
func HasGit(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && info.IsDir()
}

// AtRef reports whether dir is already at ref.
//
// An empty ref always returns true, so default-branch checkouts are never
// re-refreshed automatically (only `update` forces them).
func AtRef(dir, ref string) bool {
	if ref == "" {
		return true
	}
	if !HasGit(dir) {
		return false
	}
	if m, ok := ReadMarker(dir); ok && m.Ref == ref {
		return true
	}
	head, err := git.HeadCommit(dir)
	if err != nil {
		return false
	}
	target, err := git.Run(dir, "rev-parse", ref+"^{commit}")
	if err != nil {
		return false
	}
	return head == target
}

// WriteMarker records the requested ref, the resolved HEAD, the fetch time and
// the actual history depth.
func WriteMarker(dir, ref string, depth int) error {
	commit, err := git.HeadCommit(dir)
	if err != nil {
		return err
	}
	if depth == 0 {
		depth = DepthDefault
	}
	m := Marker{Ref: ref, Commit: commit, Fetched: time.Now().Unix(), Depth: depth}
	recorded := depthName(m.Depth)
	if !IsShallow(dir) {
		recorded = "full"
		m.Depth = DepthFull
	}
	var b strings.Builder
	fmt.Fprintf(&b, "ref=%s\ncommit=%s\nfetched=%d\ndepth=%s\n", m.Ref, m.Commit, m.Fetched, recorded)
	return os.WriteFile(cache.Marker(dir), []byte(b.String()), 0o644)
}

// ReadMarker parses the marker file for dir. A missing or invalid depth is 0
// (unknown), which keeps markers written before depth tracking readable.
func ReadMarker(dir string) (Marker, bool) {
	f, err := os.Open(cache.Marker(dir))
	if err != nil {
		return Marker{}, false
	}
	defer f.Close()
	var m Marker
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		switch k {
		case "ref":
			m.Ref = v
		case "commit":
			m.Commit = v
		case "fetched":
			if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
				m.Fetched = ts
			}
		case "depth":
			switch {
			case v == "full":
				m.Depth = DepthFull
			default:
				if n, err := strconv.Atoi(v); err == nil && n >= 1 {
					m.Depth = n
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Marker{}, false
	}
	return m, true
}
