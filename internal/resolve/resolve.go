// Package resolve implements version detection and ref resolution (§5).
package resolve

import (
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/zanza00/source-fetch/internal/manifest"
)

var versionRe = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

// InstalledVersion runs the entry's version_cmd (if any) and returns the first
// X.Y.Z it prints, or "" when there is no version. The command is skipped
// entirely when its first word is not on PATH.
func InstalledVersion(e *manifest.Entry) string {
	cmd := strings.TrimSpace(e.VersionCmd)
	if cmd == "" {
		return ""
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	if _, err := exec.LookPath(fields[0]); err != nil {
		return ""
	}
	out, _ := runShell(cmd)
	return versionRe.FindString(out)
}

// Ref resolves the ref to use for an entry following the §5 precedence:
// CLI ref → manifest ref → version tag → default_ref → "" (default branch).
func Ref(e *manifest.Entry, cliRef string) string {
	if cliRef != "" {
		return cliRef
	}
	if e.Ref != "" {
		return e.Ref
	}
	if v := InstalledVersion(e); v != "" {
		return e.TagPrefix + v
	}
	return e.DefaultRef
}

// runShell executes a shell command using the platform shell.
func runShell(cmd string) (string, error) {
	var name string
	var args []string
	if runtime.GOOS == "windows" {
		name, args = "cmd", []string{"/C", cmd}
	} else {
		name, args = "sh", []string{"-c", cmd}
	}
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}
