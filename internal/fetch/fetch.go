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

// Marker records what a cached tree holds.
type Marker struct {
	Ref     string
	Commit  string
	Fetched int64
}

// Progress receives human-readable progress messages (always stderr-bound).
type Progress func(format string, args ...any)

// Ensure makes sure the entry is cached at ref and returns its directory.
//
// Fast path: an existing checkout already at ref is returned without touching
// the network. force re-clones unconditionally (used by `update`).
func Ensure(root string, e *manifest.Entry, ref string, force bool, progress Progress) (string, error) {
	dir := cache.Dir(root, e.Name)

	if !force && HasGit(dir) && AtRef(dir, ref) {
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
		if _, err := git.Run("", "clone", "-q", "--depth", "1", e.URL, dir); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	} else {
		if _, err := git.Run("", "clone", "-q", "--depth", "1", "--branch", ref, e.URL, dir); err != nil {
			if err := fetchCommit(dir, e.URL, ref, progress); err != nil {
				_ = os.RemoveAll(dir)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return "", err
				}
				progress("ref '%s' not found, using default branch", ref)
				if _, err := git.Run("", "clone", "-q", "--depth", "1", e.URL, dir); err != nil {
					_ = os.RemoveAll(dir)
					return "", err
				}
			}
		}
	}

	if err := WriteMarker(dir, ref); err != nil {
		return "", err
	}
	return dir, nil
}

// fetchCommit treats ref as a commit SHA: fresh init, fetch the SHA, detach.
func fetchCommit(dir, url, ref string, progress Progress) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if _, err := git.Run(dir, "init", "-q"); err != nil {
		return err
	}
	if _, err := git.Run(dir, "remote", "add", "origin", url); err != nil {
		return err
	}
	if _, err := git.Run(dir, "fetch", "-q", "--depth", "1", "origin", ref); err != nil {
		return err
	}
	if _, err := git.Run(dir, "checkout", "-q", "--detach", "FETCH_HEAD"); err != nil {
		return err
	}
	return nil
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

// WriteMarker records the requested ref, the resolved HEAD and the fetch time.
func WriteMarker(dir, ref string) error {
	commit, err := git.HeadCommit(dir)
	if err != nil {
		return err
	}
	m := Marker{Ref: ref, Commit: commit, Fetched: time.Now().Unix()}
	var b strings.Builder
	fmt.Fprintf(&b, "ref=%s\ncommit=%s\nfetched=%d\n", m.Ref, m.Commit, m.Fetched)
	return os.WriteFile(cache.Marker(dir), []byte(b.String()), 0o644)
}

// ReadMarker parses the marker file for dir.
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
			m.Fetched, _ = strconv.ParseInt(v, 10, 64)
		}
	}
	return m, true
}
