// Package cache computes the on-disk cache layout and guards destructive
// operations (§6).
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zanza00/source-fetch/internal/git"
)

// EnvCache overrides the cache root entirely (no namespace); used for tests/CI.
const EnvCache = "SOURCE_FETCH_CACHE"

// MarkerName is the per-tree marker file.
const MarkerName = ".source-fetch-ref"

// Root returns the cache directory for the given manifest path.
//
// Precedence: $SOURCE_FETCH_CACHE (used as-is, no namespace) →
// $XDG_CACHE_HOME → os.UserCacheDir(), then /source-fetch/<manifest-id>.
func Root(manifestPath string) (string, error) {
	if v := os.Getenv(EnvCache); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		uc, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine cache directory: %w", err)
		}
		base = uc
	}
	return filepath.Join(base, "source-fetch", ManifestID(manifestPath)), nil
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

// GuardCleanTarget refuses clean when the target is empty, "/" or $HOME.
func GuardCleanTarget(root string) error {
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
	return nil
}
