// Package git is a thin wrapper around the external git binary.
//
// source-fetch deliberately shells out to git instead of using a pure-Go
// implementation: it preserves the exact shallow-clone/--branch and
// commit-SHA fetch semantics described in the spec (§7).
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Run executes `git args...` with the working directory set to dir (empty dir
// means "inherit the current directory"). It returns the trimmed stdout on
// success and an error carrying the trimmed stderr on failure.
func Run(dir string, args ...string) (string, error) {
	stdout, stderr, code, err := run(dir, args...)
	if err != nil {
		return stdout, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	if code != 0 {
		msg := stderr
		if msg == "" {
			msg = fmt.Sprintf("exit status %d", code)
		}
		return stdout, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout, nil
}

// RunExit executes `git args...` like Run, but reports the exit code instead
// of turning a non-zero status into an error. It is for the git commands whose
// exit status carries meaning (check-ignore: 0 ignored, 1 not ignored).
func RunExit(dir string, args ...string) (string, int, error) {
	stdout, _, code, err := run(dir, args...)
	if err != nil {
		return stdout, -1, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return stdout, code, nil
}

// run executes git and returns its trimmed stdout, trimmed stderr and exit
// code. A non-zero exit status is not an error here; only a failure to start
// the process is.
func run(dir string, args ...string) (stdout, stderr string, code int, err error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	stdout = strings.TrimSpace(outBuf.String())
	stderr = strings.TrimSpace(errBuf.String())
	code = 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
			err = nil
		}
	}
	return stdout, stderr, code, err
}

// TopLevel reports the git toplevel containing dir, if dir is inside a
// work tree.
func TopLevel(dir string) (string, bool) {
	out, err := Run(dir, "rev-parse", "--show-toplevel")
	if err != nil || out == "" {
		return "", false
	}
	return out, true
}

// HeadCommit returns the current HEAD SHA of the repository at dir.
func HeadCommit(dir string) (string, error) {
	return Run(dir, "rev-parse", "HEAD")
}

// RemoteURL returns the origin remote URL of the repository at dir.
func RemoteURL(dir string) (string, error) {
	return Run(dir, "remote", "get-url", "origin")
}

// CheckIgnore reports whether path is ignored by the repository at dir.
//
// It returns false when git says the path is not ignored; a git failure (for
// example a path outside the repository) is an error.
func CheckIgnore(dir, path string) (bool, error) {
	_, code, err := RunExit(dir, "check-ignore", "-q", "--", path)
	if err != nil {
		return false, err
	}
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, fmt.Errorf("git check-ignore: exit status %d", code)
	}
}

// ExcludeFile returns the path of the repository's info/exclude file: the
// per-clone list of ignored paths. The common directory is used, so linked
// worktrees share one exclude file.
func ExcludeFile(dir string) (string, error) {
	common, err := Run(dir, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if common == "" {
		return "", fmt.Errorf("cannot locate the git directory of %s", dir)
	}
	if !filepath.IsAbs(common) {
		top, ok := TopLevel(dir)
		if !ok {
			return "", fmt.Errorf("cannot locate the git directory of %s", dir)
		}
		common = filepath.Join(top, common)
	}
	return filepath.Join(common, "info", "exclude"), nil
}
