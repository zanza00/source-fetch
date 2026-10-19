// Package manifest loads and discovers source-fetch manifests.
//
// A manifest is flat TOML with one table per dependency, keyed under
// [source.<name>]. Only the canonical name is supported (no legacy [tool.*]).
package manifest

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	Path string
	// CacheDir is the optional top-level cache_dir: the directory that
	// holds the checkouts, relative to the project root or absolute.
	CacheDir string
	Entries  []*Entry
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
	CacheDir string              `toml:"cache_dir"`
	Source   map[string]rawEntry `toml:"source"`
}

// Load reads and validates a manifest file.
func Load(path string) (*Manifest, error) {
	var f rawFile
	if _, err := toml.DecodeFile(path, &f); err != nil {
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	m := &Manifest{Path: path, CacheDir: f.CacheDir}
	for name, r := range f.Source {
		e := &Entry{
			Name:       name,
			URL:        cmp.Or(r.URL, r.Repo), // repo is an accepted alias for the canonical url
			Transport:  cmp.Or(r.Transport, "git"),
			VersionCmd: r.VersionCmd,
			TagPrefix:  r.TagPrefix,
			Ref:        r.Ref,
			DefaultRef: r.DefaultRef,
		}
		if err := e.Validate(); err != nil {
			return nil, err
		}
		m.Entries = append(m.Entries, e)
	}
	slices.SortFunc(m.Entries, func(a, b *Entry) int { return cmp.Compare(a.Name, b.Name) })
	return m, nil
}

func (e *Entry) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("manifest: source with empty name")
	}
	if !filepath.IsLocal(e.Name) || strings.ContainsAny(e.Name, `/\`) {
		return fmt.Errorf("source %q: invalid name (must be a single local path element)", e.Name)
	}
	if e.URL == "" {
		return fmt.Errorf("source %q: url is required", e.Name)
	}
	if strings.HasPrefix(e.URL, "-") {
		return fmt.Errorf("source %q: invalid url %q (must not start with '-')", e.Name, e.URL)
	}
	if strings.HasPrefix(e.Ref, "-") {
		return fmt.Errorf("source %q: invalid ref %q (must not start with '-')", e.Name, e.Ref)
	}
	if strings.HasPrefix(e.DefaultRef, "-") {
		return fmt.Errorf("source %q: invalid default_ref %q (must not start with '-')", e.Name, e.DefaultRef)
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

// UserPath returns the per-user fallback manifest path
// (~/.config/source-fetch/manifest.toml).
func UserPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, userConfigRel), nil
}

// encodeEntry is the canonical write form of an Entry: only `url` is always
// present, optional fields are omitted when empty, and the implied default
// transport ("git") is not written out.
type encodeEntry struct {
	URL        string `toml:"url"`
	Transport  string `toml:"transport,omitempty"`
	VersionCmd string `toml:"version_cmd,omitempty"`
	TagPrefix  string `toml:"tag_prefix,omitempty"`
	Ref        string `toml:"ref,omitempty"`
	DefaultRef string `toml:"default_ref,omitempty"`
}

type encodeFile struct {
	// CacheDir comes first: a top-level key must precede the tables.
	CacheDir string                 `toml:"cache_dir,omitempty"`
	Source   map[string]encodeEntry `toml:"source"`
}

// Save rewrites the manifest file at m.Path from the in-memory entries.
//
// The whole file is re-encoded from the model, so comments and layout of a
// hand-written file are not preserved. The write is atomic: a temp file in
// the same directory is renamed over the target, and the permissions of an
// existing file are kept.
func (m *Manifest) Save() error {
	source := make(map[string]encodeEntry, len(m.Entries))
	for _, e := range m.Entries {
		if err := e.Validate(); err != nil {
			return err
		}
		ee := encodeEntry{URL: e.URL}
		if e.Transport != "" && e.Transport != "git" {
			ee.Transport = e.Transport
		}
		if e.VersionCmd != "" {
			ee.VersionCmd = e.VersionCmd
		}
		if e.TagPrefix != "" {
			ee.TagPrefix = e.TagPrefix
		}
		if e.Ref != "" {
			ee.Ref = e.Ref
		}
		if e.DefaultRef != "" {
			ee.DefaultRef = e.DefaultRef
		}
		source[e.Name] = ee
	}

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(encodeFile{CacheDir: m.CacheDir, Source: source}); err != nil {
		return fmt.Errorf("encode manifest %s: %w", m.Path, err)
	}

	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(m.Path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.Path), "."+CanonicalName+".tmp*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), m.Path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
