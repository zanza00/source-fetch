package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zanza00/source-fetch/internal/git"
)

// IgnoreResult reports what EnsureIgnored did.
type IgnoreResult string

const (
	// IgnoreAlready means the path was already ignored by git.
	IgnoreAlready IgnoreResult = "already ignored"
	// IgnoreAdded means the rule was written to the exclude file.
	IgnoreAdded IgnoreResult = "added"
	// IgnoreOutside means the path is not inside a git work tree.
	IgnoreOutside IgnoreResult = "outside a repository"
)

// EnsureIgnored makes sure that root is ignored by the git repository that
// contains it, adding the rule to the repository's exclude file when it is
// not.
//
// The rule goes to .git/info/exclude: it applies to this clone only and
// leaves the tracked files of the repository untouched. A path that is not
// inside a work tree is left alone.
func EnsureIgnored(root string) (IgnoreResult, error) {
	top, ok := git.TopLevel(existingDir(root))
	if !ok {
		return IgnoreOutside, nil
	}
	rel, err := filepath.Rel(top, root)
	if err != nil {
		return IgnoreOutside, err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return IgnoreOutside, nil
	}

	ignored, err := git.CheckIgnore(top, root)
	if err != nil {
		return IgnoreOutside, err
	}
	if ignored {
		return IgnoreAlready, nil
	}

	exclude, err := git.ExcludeFile(top)
	if err != nil {
		return IgnoreOutside, err
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return IgnoreOutside, err
	}
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return IgnoreOutside, err
	}
	defer f.Close()

	rule := fmt.Sprintf("\n# source-fetch cache\n/%s\n", filepath.ToSlash(rel))
	if _, err := f.WriteString(rule); err != nil {
		return IgnoreOutside, err
	}
	return IgnoreAdded, nil
}
