// Package git is a thin wrapper around the external git binary.
//
// source-fetch deliberately shells out to git instead of using a pure-Go
// implementation: it preserves the exact shallow-clone/--branch and
// commit-SHA fetch semantics described in the spec (§7).
package git

import (
	"bytes"
	"fmt"
	"strings"

	"os/exec"
)

// Run executes `git args...` with the working directory set to dir (empty dir
// means "inherit the current directory"). It returns the trimmed stdout on
// success and an error carrying the trimmed stderr on failure.
func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
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
