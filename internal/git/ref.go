package git

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateRef reports whether ref is a syntactically valid Git ref name.
//
// An empty ref is valid: it means the remote default branch. Otherwise the
// rules are a conservative subset of git-check-ref-format(1): they reject
// anything that git would parse as an option, a revision expression, a path or
// an invalid loose ref, while accepting ordinary branch and tag names such as
// v1.0.0, release/1.0, refs/tags/v1 and feature/foo+bar.
//
// This is deliberately not full refspec parsing: it validates the shape of a
// single ref, which is all that `git clone --branch`, `git fetch` and
// `git rev-parse` need.
func ValidateRef(ref string) error {
	if ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("ref %q must not start with '-'", ref)
	}
	if ref == "@" {
		return fmt.Errorf("ref %q must not be exactly '@'", ref)
	}
	if strings.Contains(ref, "..") {
		return fmt.Errorf("ref %q must not contain '..'", ref)
	}
	if strings.Contains(ref, "@{") {
		return fmt.Errorf("ref %q must not contain '@{'", ref)
	}
	if strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") {
		return fmt.Errorf("ref %q must not start or end with '/'", ref)
	}
	if strings.Contains(ref, "//") {
		return fmt.Errorf("ref %q must not contain '//'", ref)
	}
	if strings.HasSuffix(ref, ".") {
		return fmt.Errorf("ref %q must not end with '.'", ref)
	}
	for _, r := range ref {
		switch {
		case unicode.IsControl(r):
			return fmt.Errorf("ref %q must not contain control characters", ref)
		case r == ' ':
			return fmt.Errorf("ref %q must not contain spaces", ref)
		case strings.ContainsRune("~^:?*[\\", r):
			return fmt.Errorf("ref %q must not contain %q", ref, r)
		}
	}
	for _, part := range strings.Split(ref, "/") {
		if strings.HasPrefix(part, ".") {
			return fmt.Errorf("ref %q must not have a path component starting with '.'", ref)
		}
		if strings.HasSuffix(part, ".lock") {
			return fmt.Errorf("ref %q must not have a path component ending with '.lock'", ref)
		}
	}
	return nil
}
