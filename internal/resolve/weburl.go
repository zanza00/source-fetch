package resolve

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// ParsedURL is a web repository link split into the pieces needed to fetch and
// open it.
type ParsedURL struct {
	RepoURL string // clone URL, without query/fragment
	Ref     string // branch/tag/SHA extracted from the URL ("" when absent)
	Subpath string // in-repository path with '/', already cleaned ("" when absent)
}

var (
	githubMarkers = map[string]bool{
		"blob": true, "tree": true, "commit": true,
		"raw": true, "blame": true, "releases": true,
	}
	giteaMarkers = map[string]bool{
		"src": true, "raw": true, "blame": true, "releases": true,
	}
	// gitlabMarkers includes the page kinds GitLab supports but source-fetch
	// does not: seeing one of them lets the parser fail instead of treating
	// the page as a repository path.
	gitlabMarkers = map[string]bool{
		"blob": true, "tree": true, "commit": true, "raw": true, "blame": true,
		"tags": true, "releases": true, "issues": true, "merge_requests": true,
		"pipelines": true,
	}
	gitlabLegacyMarkers = map[string]bool{
		"blob": true, "tree": true, "commit": true, "raw": true, "blame": true,
		"issues": true, "merge_requests": true, "pipelines": true,
		"tags": true, "releases": true,
	}
	gitlabSourceKinds = map[string]bool{
		"blob": true, "tree": true, "commit": true,
		"raw": true, "blame": true, "tags": true, "releases": true,
	}
	webMarkers = map[string]bool{
		"blob": true, "tree": true, "commit": true, "commits": true,
		"compare": true, "src": true, "raw": true, "blame": true,
		"issues": true, "pulls": true, "actions": true, "search": true,
		"settings": true, "merge_requests": true, "releases": true, "tags": true,
	}
)

// ParseWebURL splits a repository URL or a web deep link into a clone URL, a
// ref and an in-repository subpath. It never touches the network.
//
// An input that is not HTTP(S) is returned unchanged in RepoURL. An HTTP(S)
// URL that is recognisably a web page source-fetch does not support returns an
// error, so it never turns into a clone of the wrong page.
//
// cliRef is an explicit ref from the command line. It only takes part in
// prefix matching for refs that contain '/': the caller applies the final
// precedence with cmp.Or(cliRef, parsed.Ref).
func ParseWebURL(rawURL, cliRef string) (ParsedURL, error) {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return ParsedURL{}, fmt.Errorf("missing URL")
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ParsedURL{RepoURL: raw}, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ParsedURL{}, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ParsedURL{}, fmt.Errorf("invalid URL %q: missing host", raw)
	}

	segs := splitPathSegments(u.Path)
	var (
		repo     []string
		ref, sub string
	)
	switch host {
	case "github.com":
		repo, ref, sub, err = parseGitHub(raw, segs, cliRef)
	case "codeberg.org":
		repo, ref, sub, err = parseCodeberg(raw, segs, cliRef)
	case "gitlab.com":
		repo, ref, sub, err = parseGitLab(raw, segs, cliRef)
	default:
		repo, ref, sub, err = parseGeneric(raw, segs, cliRef)
	}
	if err != nil {
		return ParsedURL{}, err
	}

	for _, s := range repo {
		if s == ".." {
			return ParsedURL{}, fmt.Errorf("path %q escapes the repository", strings.Join(repo, "/"))
		}
	}
	sub, err = cleanSubpath(sub)
	if err != nil {
		return ParsedURL{}, err
	}

	u.Path = "/" + strings.Join(repo, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	u.RawFragment = ""
	return ParsedURL{RepoURL: u.String(), Ref: ref, Subpath: sub}, nil
}

// splitPathSegments splits an URL path without a global path.Clean: empty and
// "." segments are dropped, but ".." is preserved so traversal can be
// detected instead of silently rewriting the path.
func splitPathSegments(p string) []string {
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s == "" || s == "." {
			continue
		}
		out = append(out, s)
	}
	return out
}

func parseGitHub(raw string, segs []string, cliRef string) ([]string, string, string, error) {
	if len(segs) == 2 {
		return segs, "", "", nil
	}
	if len(segs) < 3 || !githubMarkers[segs[2]] {
		return nil, "", "", fmt.Errorf("unsupported GitHub URL %q: only repository, blob, tree, commit, raw, blame and releases/tag links are supported", raw)
	}
	ref, sub, err := extractGitHubLike(segs[2], segs[3:], cliRef, raw)
	if err != nil {
		return nil, "", "", err
	}
	return segs[:2], ref, sub, nil
}

func parseCodeberg(raw string, segs []string, cliRef string) ([]string, string, string, error) {
	if len(segs) == 2 {
		return segs, "", "", nil
	}
	if len(segs) < 3 || !giteaMarkers[segs[2]] {
		return nil, "", "", fmt.Errorf("unsupported Codeberg URL %q: only repository, src, raw, blame and releases/tag links are supported", raw)
	}
	ref, sub, err := extractGitea(segs[2], segs[3:], cliRef, raw)
	if err != nil {
		return nil, "", "", err
	}
	return segs[:2], ref, sub, nil
}

func parseGitLab(raw string, segs []string, cliRef string) ([]string, string, string, error) {
	if i, kind, ok := findGitLabMarker(segs); ok {
		if !gitlabSourceKinds[kind] {
			return nil, "", "", unsupportedGitLab(raw)
		}
		ref, sub, err := extractGitLab(kind, segs[i+2:], cliRef, raw)
		if err != nil {
			return nil, "", "", err
		}
		return segs[:i], ref, sub, nil
	}
	for i := 2; i+1 < len(segs); i++ {
		if gitlabLegacyMarkers[segs[i]] {
			return nil, "", "", fmt.Errorf("unsupported GitLab URL %q: legacy /%s/ links are not supported; use the /-/%s/ form", raw, segs[i], segs[i])
		}
	}
	if len(segs) < 2 {
		return nil, "", "", unsupportedGitLab(raw)
	}
	return segs, "", "", nil
}

func parseGeneric(raw string, segs []string, cliRef string) ([]string, string, string, error) {
	if githubShapeOK(segs) {
		ref, sub, err := extractGitHubLike(segs[2], segs[3:], cliRef, raw)
		if err != nil {
			return nil, "", "", err
		}
		return segs[:2], ref, sub, nil
	}
	if giteaShapeOK(segs) {
		ref, sub, err := extractGitea(segs[2], segs[3:], cliRef, raw)
		if err != nil {
			return nil, "", "", err
		}
		return segs[:2], ref, sub, nil
	}
	if i, kind, ok := findGitLabMarker(segs); ok && gitlabSourceKinds[kind] && len(segs) > i+2 {
		ref, sub, err := extractGitLab(kind, segs[i+2:], cliRef, raw)
		if err != nil {
			return nil, "", "", err
		}
		return segs[:i], ref, sub, nil
	}
	for i := 2; i < len(segs); i++ {
		if webMarkers[segs[i]] {
			return nil, "", "", fmt.Errorf("unsupported URL form %q: base paths and this host are not supported; use the repository URL", raw)
		}
	}
	if len(segs) == 0 {
		return nil, "", "", fmt.Errorf("invalid URL %q: no repository path", raw)
	}
	return segs, "", "", nil
}

func githubShapeOK(segs []string) bool {
	if len(segs) < 3 || !githubMarkers[segs[2]] {
		return false
	}
	rest := segs[3:]
	if segs[2] == "releases" {
		return len(rest) >= 2 && rest[0] == "tag"
	}
	return len(rest) >= 1
}

func giteaShapeOK(segs []string) bool {
	if len(segs) < 5 || !giteaMarkers[segs[2]] {
		return false
	}
	if segs[2] == "releases" {
		return segs[3] == "tag"
	}
	switch segs[3] {
	case "branch", "tag", "commit":
		return true
	}
	return false
}

// findGitLabMarker returns the first "-" at index >= 2 followed by a known
// GitLab kind. Unsupported page kinds are still recognised here so the parser
// can report them instead of treating the URL as a repository.
func findGitLabMarker(segs []string) (int, string, bool) {
	for i := 2; i+1 < len(segs); i++ {
		if segs[i] == "-" && gitlabMarkers[segs[i+1]] {
			return i, segs[i+1], true
		}
	}
	return 0, "", false
}

func extractGitHubLike(kind string, parts []string, cliRef, raw string) (string, string, error) {
	switch kind {
	case "blob", "tree", "raw", "blame":
		if len(parts) == 0 {
			return "", "", fmt.Errorf("URL %q has no ref after /%s/", raw, kind)
		}
		ref, sub := splitRefAndPath(parts, cliRef)
		return ref, sub, nil
	case "commit":
		if len(parts) == 0 {
			return "", "", fmt.Errorf("URL %q has no ref after /commit/", raw)
		}
		return parts[0], strings.Join(parts[1:], "/"), nil
	case "releases":
		if len(parts) < 2 || parts[0] != "tag" {
			return "", "", fmt.Errorf("URL %q has no tag after /releases/tag/", raw)
		}
		return parts[1], "", nil
	default:
		return "", "", fmt.Errorf("unsupported URL form %q", raw)
	}
}

func extractGitea(marker string, rest []string, cliRef, raw string) (string, string, error) {
	switch marker {
	case "src", "raw", "blame":
		if len(rest) < 2 {
			return "", "", fmt.Errorf("URL %q has no ref after /%s/", raw, marker)
		}
		switch rest[0] {
		case "branch", "tag":
			ref, sub := splitRefAndPath(rest[1:], cliRef)
			return ref, sub, nil
		case "commit":
			return rest[1], strings.Join(rest[2:], "/"), nil
		default:
			return "", "", fmt.Errorf("unsupported URL form %q: expected branch, tag or commit after /%s/", raw, marker)
		}
	case "releases":
		if len(rest) < 2 || rest[0] != "tag" {
			return "", "", fmt.Errorf("URL %q has no tag after /releases/tag/", raw)
		}
		return rest[1], "", nil
	default:
		return "", "", fmt.Errorf("unsupported URL form %q", raw)
	}
}

func extractGitLab(kind string, parts []string, cliRef, raw string) (string, string, error) {
	switch kind {
	case "blob", "tree", "raw", "blame":
		if len(parts) == 0 {
			return "", "", fmt.Errorf("URL %q has no ref after /%s/", raw, kind)
		}
		ref, sub := splitRefAndPath(parts, cliRef)
		return ref, sub, nil
	case "commit":
		if len(parts) == 0 {
			return "", "", fmt.Errorf("URL %q has no ref after /commit/", raw)
		}
		return parts[0], strings.Join(parts[1:], "/"), nil
	case "tags", "releases":
		if len(parts) == 0 {
			return "", "", fmt.Errorf("URL %q has no tag after /%s/", raw, kind)
		}
		return parts[0], "", nil
	default:
		return "", "", unsupportedGitLab(raw)
	}
}

func unsupportedGitLab(raw string) error {
	return fmt.Errorf("unsupported GitLab URL %q: only repository, blob, tree, commit, raw, blame, tags and releases links are supported", raw)
}

// splitRefAndPath separates a ref from the rest of the path. An explicit
// cliRef only participates through prefix matching, so a ref containing '/'
// (like release/1.0) can be recovered from the URL.
func splitRefAndPath(parts []string, cliRef string) (string, string) {
	if cliRef != "" {
		prefix := strings.Split(cliRef, "/")
		if hasPathPrefix(parts, prefix) {
			return cliRef, strings.Join(parts[len(prefix):], "/")
		}
	}
	if len(parts) == 0 {
		return "", ""
	}
	return parts[0], strings.Join(parts[1:], "/")
}

func hasPathPrefix(parts, prefix []string) bool {
	if len(parts) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if parts[i] != p {
			return false
		}
	}
	return true
}

func cleanSubpath(sub string) (string, error) {
	if sub == "" {
		return "", nil
	}
	cleaned := path.Clean(sub)
	if cleaned == "." {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." ||
		strings.HasPrefix(cleaned, "/") || strings.ContainsRune(cleaned, 0) ||
		!filepath.IsLocal(filepath.FromSlash(cleaned)) {
		return "", fmt.Errorf("path %q escapes the repository", cleaned)
	}
	return cleaned, nil
}
