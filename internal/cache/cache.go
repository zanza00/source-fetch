// Package cache computes the on-disk cache layout and guards destructive
// operations (§6).
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/zanza00/source-fetch/internal/git"
)

// EnvCache overrides the cache root entirely (no namespace); used for tests/CI.
const EnvCache = "SOURCE_FETCH_CACHE"

// LocalDirName is the name of the project-local cache directory.
const LocalDirName = ".source-fetch"

// MarkerName is the per-tree marker file.
const MarkerName = ".source-fetch-ref"

// Source names where a resolved cache root came from.
const (
	SourceOverride = "override" // the --cache flag
	SourceEnv      = "env"      // $SOURCE_FETCH_CACHE
	SourceManifest = "manifest" // the manifest's cache_dir
	SourceProject  = "project"  // the project-local default
	SourceUser     = "user"     // the user cache directory
)

// Location is a resolved cache root.
type Location struct {
	// Path is the namespace root: manifest entries live at Path/<name>.
	Path string
	// Base is the cache root: ad-hoc (peek) trees live at Base/adhoc.
	// It equals Path except in the user-cache mode, where the ad-hoc trees
	// are shared by every manifest.
	Base string
	// Local is true when the cache lives outside the user cache directory:
	// entries are not namespaced per manifest.
	Local bool
	// Source names where the path came from.
	Source string
}

// Dir returns the directory of one entry inside the location.
func (l Location) Dir(name string) string {
	return Dir(l.Path, name)
}

// Resolve computes the cache root for a manifest.
//
// Precedence: the --cache override → $SOURCE_FETCH_CACHE → the manifest's
// cache_dir → the project-local default (<project root>/.source-fetch) → the
// user cache directory (<user cache>/source-fetch/<manifest-id>).
//
// The project root is the git toplevel that contains the current directory;
// when the current directory is not in a work tree, it is the git toplevel
// that owns the manifest, else the manifest's directory. Relative paths from
// the environment or the manifest are resolved against the project root, so
// the cache does not move when the command runs from a subdirectory. The
// --cache override is resolved against the current directory.
func Resolve(manifestPath, cacheDir, override string) (Location, error) {
	if override != "" {
		abs, err := filepath.Abs(override)
		if err != nil {
			return Location{}, err
		}
		return Location{Path: abs, Base: abs, Local: true, Source: SourceOverride}, nil
	}

	root := ProjectRoot(manifestPath)

	if v := os.Getenv(EnvCache); v != "" {
		abs, err := resolveIn(v, root)
		if err != nil {
			return Location{}, err
		}
		return Location{Path: abs, Base: abs, Local: true, Source: SourceEnv}, nil
	}
	if cacheDir != "" {
		abs, err := resolveIn(cacheDir, root)
		if err != nil {
			return Location{}, err
		}
		return Location{Path: abs, Base: abs, Local: true, Source: SourceManifest}, nil
	}
	if local, ok := LocalRoot(root); ok {
		return Location{Path: local, Base: local, Local: true, Source: SourceProject}, nil
	}

	base, err := userBase()
	if err != nil {
		return Location{}, err
	}
	userRoot := filepath.Join(base, "source-fetch")
	return Location{
		Path:   filepath.Join(userRoot, ManifestID(manifestPath)),
		Base:   userRoot,
		Local:  false,
		Source: SourceUser,
	}, nil
}

// ResolveAdhoc computes the ad-hoc cache root used by `peek`.
//
// It follows the same precedence as Resolve, anchored at the manifest in use
// (or the current directory when there is none); ad-hoc trees always live in
// an "adhoc" subdirectory of the cache root.
func ResolveAdhoc(manifestPath, cacheDir, override string) (Location, error) {
	loc, err := Resolve(manifestPath, cacheDir, override)
	if err != nil {
		return Location{}, err
	}
	return Location{
		Path:   filepath.Join(loc.Base, "adhoc"),
		Base:   loc.Base,
		Local:  loc.Local,
		Source: loc.Source,
	}, nil
}

// ProjectRoot returns the project root: the git toplevel that contains the
// current directory; when the current directory is not in a work tree, the
// git toplevel that owns the manifest, else the manifest's directory, else
// the current directory.
//
// The current directory comes first on purpose: the cache belongs to the
// project you are working in, not to the manifest that happens to be
// discovered there.
func ProjectRoot(manifestPath string) string {
	wd, wdErr := os.Getwd()
	if wdErr == nil {
		if top, ok := git.TopLevel(existingDir(wd)); ok {
			return top
		}
	}
	if manifestPath != "" {
		dir := filepath.Dir(absPath(manifestPath))
		if top, ok := git.TopLevel(existingDir(dir)); ok {
			return top
		}
		return dir
	}
	if wdErr != nil {
		return "."
	}
	return wd
}

// LocalRoot returns the project-local cache root for a project root
// (<project root>/.source-fetch) and whether that root is a git work tree.
func LocalRoot(root string) (string, bool) {
	top, ok := git.TopLevel(existingDir(root))
	if !ok || !sameDir(top, root) {
		return "", false
	}
	return filepath.Join(top, LocalDirName), true
}

// absPath returns the absolute form of path, or path when it cannot be
// resolved.
func absPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// resolveIn makes path absolute, resolving a relative path against base.
func resolveIn(path, base string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	abs, err := filepath.Abs(filepath.Join(base, path))
	if err != nil {
		return "", err
	}
	return abs, nil
}

// existingDir returns the nearest existing ancestor of dir.
func existingDir(dir string) string {
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}

func userBase() (string, error) {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		uc, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine cache directory: %w", err)
		}
		base = uc
	}
	return base, nil
}

// AdhocName derives the cache name for a one-off URL: the repo basename
// (sanitised) plus the first 8 hex chars of sha256(url), so same-named repos
// from different hosts never collide.
func AdhocName(url string) string {
	u := url
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	u = strings.TrimSuffix(u, ".git")
	base := path.Base(strings.TrimSuffix(u, "/"))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.TrimLeft(b.String(), "-.")
	if name == "" {
		name = "src"
	}
	sum := sha256.Sum256([]byte(url))
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}

// ManifestID is a stable id of the repo owning the manifest: the basename of
// the git toplevel containing it, else path-<hash>.
func ManifestID(manifestPath string) string {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		abs = manifestPath
	}
	if top, ok := git.TopLevel(filepath.Dir(abs)); ok {
		return filepath.Base(top)
	}
	sum := sha256.Sum256([]byte(abs))
	return "path-" + hex.EncodeToString(sum[:])[:12]
}

// Dir is the cache directory for one entry.
func Dir(root, name string) string {
	return filepath.Join(root, name)
}

// Marker is the marker file path for one entry.
func Marker(dir string) string {
	return filepath.Join(dir, MarkerName)
}

// GuardCleanTarget refuses clean when the target is empty, "/" or $HOME. It
// also refuses the roots that would take the project down with them: the
// toplevel of a git repository, a directory that holds the manifest, and a
// directory that holds the current working directory.
func GuardCleanTarget(root, manifestPath string) error {
	if root == "" {
		return fmt.Errorf("refusing to clean an empty cache path")
	}
	cleaned := filepath.Clean(root)
	if cleaned == string(filepath.Separator) || cleaned == "." {
		return fmt.Errorf("refusing to clean %s", cleaned)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if cleaned == filepath.Clean(home) {
			return fmt.Errorf("refusing to clean $HOME (%s)", cleaned)
		}
	}
	if top, ok := git.TopLevel(existingDir(cleaned)); ok && sameDir(cleaned, top) {
		return fmt.Errorf("refusing to clean the repository root (%s)", cleaned)
	}
	if manifestPath != "" {
		abs, err := filepath.Abs(manifestPath)
		if err == nil && contains(cleaned, abs) {
			return fmt.Errorf("refusing to clean %s: it holds the manifest %s", cleaned, abs)
		}
	}
	if wd, err := os.Getwd(); err == nil && contains(cleaned, wd) {
		return fmt.Errorf("refusing to clean %s: it holds the current directory", cleaned)
	}
	return nil
}

// sameDir reports whether a and b are the same directory.
func sameDir(a, b string) bool {
	return resolvePath(a) == resolvePath(b)
}

// contains reports whether path is dir or lives inside it.
func contains(dir, path string) bool {
	dir = resolvePath(dir)
	path = resolvePath(path)
	if dir == path {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// resolvePath returns the canonical form of path with symlinks resolved.
// Paths that do not exist yet are only cleaned.
func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
