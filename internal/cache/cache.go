// Package cache computes the on-disk cache layout, canonicalises ad-hoc URLs
// (AdhocKey/AdhocName) and guards destructive operations (§6).
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

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
	base, err := userBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "source-fetch", ManifestID(manifestPath)), nil
}

// AdhocRoot returns the cache directory for one-off `peek` fetches. It is
// independent of any manifest. Project `clean` leaves it alone unless
// SOURCE_FETCH_CACHE is set, in which case it is nested in the override root.
//
// Precedence mirrors Root: $SOURCE_FETCH_CACHE/adhoc →
// $XDG_CACHE_HOME/source-fetch/adhoc → <user cache>/source-fetch/adhoc.
func AdhocRoot() (string, error) {
	if v := os.Getenv(EnvCache); v != "" {
		abs, err := filepath.Abs(v)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, "adhoc"), nil
	}
	base, err := userBase()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "source-fetch", "adhoc"), nil
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

// AdhocKey returns the stable identity of a one-off URL. It canonicalises
// spelling differences that do not change the repository (trailing slash,
// query/fragment, host case, default port, and .git on known hosts) while
// keeping HTTP, SSH and scp-like transports distinct. The result is not
// necessarily a clone URL.
func AdhocKey(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// scp-like URLs must be recognised before url.Parse: Go rejects
	// "git@host:path" with "first path segment in URL cannot contain colon".
	if user, host, p, ok := splitSCP(s); ok {
		p = trimDotGit(path.Clean(p))
		if user != "" {
			return "scp://" + user + "@" + strings.ToLower(host) + ":" + p
		}
		return "scp://" + strings.ToLower(host) + ":" + p
	}
	if IsWindowsPath(s) {
		return filepath.Clean(s)
	}

	u, err := url.Parse(s)
	if err != nil {
		return fallbackKey(s)
	}
	if u.Scheme == "" {
		return filepath.Clean(s)
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	switch scheme {
	case "http", "https":
		if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
			port = ""
		}
		p := cleanURLPath(u.Path)
		if knownHost(host) {
			p = trimDotGit(p)
		}
		return scheme + "://" + hostPort(host, port) + p
	case "ssh":
		if port == "22" {
			port = ""
		}
		user := ""
		if u.User != nil {
			user = u.User.Username()
		}
		prefix := "ssh://"
		if user != "" {
			prefix += user + "@"
		}
		return prefix + hostPort(host, port) + trimDotGit(cleanSlashPath(u.Path))
	case "file":
		// A local directory may legitimately be named x.git: do not strip it.
		h := strings.ToLower(u.Host)
		return "file://" + h + cleanSlashPath(u.Path)
	default:
		if u.Host == "" {
			return scheme + ":" + u.Opaque + cleanSlashPath(u.Path)
		}
		return scheme + "://" + strings.ToLower(u.Host) + cleanSlashPath(u.Path)
	}
}

// AdhocName derives the cache name for a one-off URL: the canonical repo
// basename (sanitised) plus the first 8 hex chars of sha256(AdhocKey), so
// spelling variants of the same repo share one tree.
func AdhocName(raw string) string {
	key := AdhocKey(raw)
	base := path.Base(key)
	// For display, drop .git even when the key keeps it (file:// and unknown
	// hosts), so file:///tmp/bare.git is still bare-<hash>.
	if strings.HasSuffix(strings.ToLower(base), ".git") {
		base = base[:len(base)-4]
	}
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
	sum := sha256.Sum256([]byte(key))
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}

// splitSCP recognises the scp-like form [user@]host:path, where path is
// relative to the remote user's home unless it starts with '/'.
func splitSCP(s string) (user, host, p string, ok bool) {
	if strings.Contains(s, "://") || strings.ContainsAny(s, " \t\r\n") {
		return "", "", "", false
	}
	at := strings.LastIndex(s, "@")
	rest := s
	if at >= 0 {
		user = s[:at]
		rest = s[at+1:]
	}
	colon := strings.IndexByte(rest, ':')
	if colon <= 0 {
		return "", "", "", false
	}
	host, p = rest[:colon], rest[colon+1:]
	if host == "" || strings.ContainsAny(host, `/\`) {
		return "", "", "", false
	}
	// A bare single-letter host is a Windows drive (C:/x, C:\x, C:relative),
	// not an scp URL. Drive-relative paths are local only on Windows; on Unix
	// the same spelling is a valid scp-like URL.
	if at < 0 && IsWindowsPath(rest) {
		return "", "", "", false
	}
	return user, host, p, true
}

// IsSCPLike reports whether s is an scp-like git URL of the form
// [user@]host:path, the spelling AdhocKey treats as its own transport. Local
// paths and Windows drive paths are not scp-like. Commands use it to decide
// whether an argument can address an ad-hoc cache entry.
func IsSCPLike(s string) bool {
	_, _, p, ok := splitSCP(s)
	return ok && p != ""
}

// IsWindowsPath reports whether s is a Windows drive path: C:, C:/x, C:\x, or
// the drive-relative C:relative. Drive-relative paths are local only on
// Windows; elsewhere the same spelling is a valid scp-like URL, so the
// classification is platform-dependent.
func IsWindowsPath(s string) bool {
	return isWindowsPathOS(s, runtime.GOOS == "windows")
}

func isWindowsPathOS(s string, windows bool) bool {
	if len(s) < 2 || s[1] != ':' || !isASCIIAlpha(s[0]) {
		return false
	}
	if len(s) == 2 || s[2] == '/' || s[2] == '\\' {
		return true
	}
	return windows
}

func isASCIIAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// fallbackKey is the textual cleanup used when url.Parse fails. AdhocKey
// cannot return an error, so it must stay stable and never panic.
func fallbackKey(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, "/")
}

func cleanSlashPath(p string) string {
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

func cleanURLPath(p string) string {
	p = cleanSlashPath(p)
	if p == "/" {
		return ""
	}
	return p
}

func trimDotGit(p string) string {
	if strings.HasSuffix(strings.ToLower(p), ".git") {
		return p[:len(p)-4]
	}
	return p
}

func knownHost(host string) bool {
	switch host {
	case "github.com", "gitlab.com", "codeberg.org", "gitea.com":
		return true
	}
	return false
}

func hostPort(host, port string) string {
	if port != "" {
		return net.JoinHostPort(host, port)
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
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
