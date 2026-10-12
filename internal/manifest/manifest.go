// Package manifest loads and discovers source-fetch manifests.
//
// A manifest is flat TOML with one table per dependency, keyed under
// [source.<name>]. Only the canonical name is supported (no legacy [tool.*]).
package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// EnvManifest is the explicit manifest override.
const EnvManifest = "SOURCE_FETCH_MANIFEST"

// CanonicalName is the discovery filename looked up while walking upward.
const CanonicalName = ".source-fetch.toml"

// userConfigRel is the optional per-user fallback manifest.
var userConfigRel = filepath.Join(".config", "source-fetch", "manifest.toml")

// Entry is one dependency to be fetched.
type Entry struct {
	Name       string
	URL        string
	Transport  string
	VersionCmd string
	TagPrefix  string
	Ref        string
	DefaultRef string
}

// Manifest is a loaded set of entries plus the path it came from.
type Manifest struct {
	Path    string
	Entries []*Entry
}

type rawEntry struct {
	URL        string `toml:"url"`
	Repo       string `toml:"repo"`
	Transport  string `toml:"transport"`
	VersionCmd string `toml:"version_cmd"`
	TagPrefix  string `toml:"tag_prefix"`
	Ref        string `toml:"ref"`
	DefaultRef string `toml:"default_ref"`
}

type rawFile struct {
	Source map[string]rawEntry `toml:"source"`
}

// Load reads and validates a manifest file.
func Load(path string) (*Manifest, error) {
	var f rawFile
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	m := &Manifest{Path: path}
	for name, r := range f.Source {
		e := &Entry{
			Name:       name,
			URL:        r.URL,
			Transport:  r.Transport,
			VersionCmd: r.VersionCmd,
			TagPrefix:  r.TagPrefix,
			Ref:        r.Ref,
			DefaultRef: r.DefaultRef,
		}
		if e.URL == "" {
			e.URL = r.Repo // accepted alias for the canonical `url`
		}
		if e.Transport == "" {
			e.Transport = "git"
		}
		if err := e.validate(); err != nil {
			return nil, err
		}
		m.Entries = append(m.Entries, e)
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Name < m.Entries[j].Name })
	return m, nil
}

func (e *Entry) validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("manifest: source with empty name")
	}
	if e.URL == "" {
		return fmt.Errorf("source %q: url is required", e.Name)
	}
	if e.Transport != "git" {
		return fmt.Errorf("source %q: unsupported transport %q (only \"git\" is supported)", e.Name, e.Transport)
	}
	return nil
}

// Lookup returns the entry named name, or false.
func (m *Manifest) Lookup(name string) (*Entry, bool) {
	for _, e := range m.Entries {
		if e.Name == name {
			return e, true
		}
	}
	return nil, false
}

// Discover resolves the manifest path: $SOURCE_FETCH_MANIFEST, then a walk
// upward from startDir for CanonicalName, then the per-user config file.
// The returned error is non-nil when nothing is found.
func Discover(startDir string) (string, error) {
	if v := os.Getenv(EnvManifest); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", err
		}
		if fileExists(abs) {
			return abs, nil
		}
		return "", fmt.Errorf("manifest not found: %s (from %s)", abs, EnvManifest)
	}

	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, CanonicalName)
		if fileExists(candidate) {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidate := filepath.Join(home, userConfigRel)
		if fileExists(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("manifest not found (searched for %s from %s; set %s)",
		CanonicalName, startDir, EnvManifest)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
