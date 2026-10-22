package resolve

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/zanza00/source-fetch/internal/git"
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
		"src": true, "raw": true, "blame": true, "commit": true, "releases": true,
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

	segs, err := splitPathSegments(u.Path)
	if err != nil {
		return ParsedURL{}, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	var (
		repo     []string
		ref, sub string
	)
	switch {
	case host == "github.com":
		repo, ref, sub, err = parseGitHub(raw, segs, cliRef)
	case host == "codeberg.org":
		repo, ref, sub, err = parseGitea(raw, segs, "Codeberg", cliRef)
	case host == "gitea.com":
		repo, ref, sub, err = parseGitea(raw, segs, "Gitea", cliRef)
	case host == "gitlab.com":
		repo, ref, sub, err = parseGitLab(raw, segs, cliRef)
	case host == "raw.githubusercontent.com":
		// raw.githubusercontent.com is a raw-file CDN, never a repository
		// host: /{owner}/{repo}/{ref}/{path} would otherwise look exactly like
		// a GitHub-like repo path and be cloned.
		return ParsedURL{}, fmt.Errorf("unsupported host %q: raw file pages are not supported; use the repository URL", host)
	case host == "bitbucket.org":
		repo, ref, sub, err = parseBitbucket(raw, segs)
	case isAzureDevOpsHost(host):
		repo, ref, sub, err = parseAzureDevOps(raw, u, segs)
	default:
		repo, ref, sub, err = parseGeneric(raw, segs, cliRef)
	}
	if err != nil {
		return ParsedURL{}, err
	}

	// A ref taken from the URL is attacker-controlled. Reject it here instead
	// of letting clone/fetch fail and silently falling back to the default
	// branch, which would serve a file from the wrong commit.
	if ref != "" {
		if err := git.ValidateRef(ref); err != nil {
			return ParsedURL{}, fmt.Errorf("invalid ref %q in URL %q: %w", ref, raw, err)
		}
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

// splitPathSegments splits an URL path without a global path.Clean. Leading and
// trailing slashes are ignored and "." segments are dropped (RFC 3986 treats
// them as relative-path noise), but ".." is preserved so traversal can be
// detected instead of silently rewriting the path.
//
// An empty segment between non-empty ones (as in /blob//file) is malformed: it
// would shift the ref and the subpath, so it is rejected instead of being
// dropped. A trailing slash is not an empty segment.
func splitPathSegments(p string) ([]string, error) {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil, nil
	}
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		switch s {
		case "":
			return nil, fmt.Errorf("empty path segment")
		case ".":
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func parseGitHub(raw string, segs []string, cliRef string) ([]string, string, string, error) {
	if len(segs) == 2 {
		return segs, "", "", nil
	}
	if len(segs) < 3 || !githubMarkers[segs[2]] {
		return nil, "", "", fmt.Errorf("unsupported GitHub URL %q: only repository, blob, tree, commit, raw, blame and releases/tag links are supported", raw)
	}
	ref, sub, err := extractRef(styleGitHub, segs[2], segs[3:], cliRef, raw)
	if err != nil {
		return nil, "", "", err
	}
	return segs[:2], ref, sub, nil
}

// parseGitea parses Codeberg, Gitea and Forgejo links. site names the host in
// error messages ("Codeberg", "Gitea").
func parseGitea(raw string, segs []string, site, cliRef string) ([]string, string, string, error) {
	if len(segs) == 2 {
		return segs, "", "", nil
	}
	if len(segs) < 3 || !giteaMarkers[segs[2]] {
		return nil, "", "", fmt.Errorf("unsupported %s URL %q: only repository, src, commit, raw, blame and releases/tag links are supported", site, raw)
	}
	ref, sub, err := extractRef(styleGitea, segs[2], segs[3:], cliRef, raw)
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
		if i < 2 {
			return nil, "", "", fmt.Errorf("unsupported GitLab URL %q: missing repository path before /-/%s/ (expected namespace/project)", raw, kind)
		}
		ref, sub, err := extractRef(styleGitLab, kind, segs[i+2:], cliRef, raw)
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

// parseBitbucket accepts a Bitbucket repository URL only. Bitbucket serves
// source, raw, commit, pull-request and wiki pages below the same
// /{workspace}/{repo} prefix, so any deeper path would be cloned as though it
// were the repository.
func parseBitbucket(raw string, segs []string) ([]string, string, string, error) {
	if len(segs) != 2 {
		return nil, "", "", fmt.Errorf("unsupported Bitbucket URL %q: only repository links (/{workspace}/{repo}) are supported; source, raw, commits, pull request and wiki pages are not", raw)
	}
	return segs, "", "", nil
}

// isAzureDevOpsHost reports whether host belongs to Azure DevOps. The legacy
// form uses a per-organisation {org}.visualstudio.com host.
func isAzureDevOpsHost(host string) bool {
	return host == "dev.azure.com" || strings.HasSuffix(host, ".visualstudio.com")
}

// parseAzureDevOps accepts an Azure DevOps repository URL such as
// dev.azure.com/{org}/{project}/_git/{repo} as a plain clone URL. Azure DevOps
// deep links carry the file path in the ?path= query parameter, which
// ParseWebURL always drops; accepting those would print the repository root
// instead of the file, so they are rejected.
func parseAzureDevOps(raw string, u *url.URL, segs []string) ([]string, string, string, error) {
	git := -1
	for i, s := range segs {
		if s == "_git" {
			git = i
			break
		}
	}
	if git < 0 || git+1 >= len(segs) {
		return nil, "", "", fmt.Errorf("unsupported Azure DevOps URL %q: expected a repository URL containing /_git/{repo}", raw)
	}
	if _, ok := u.Query()["path"]; ok {
		return nil, "", "", fmt.Errorf("unsupported Azure DevOps URL %q: deep links with ?path= are not supported; use the repository URL", raw)
	}
	return segs, "", "", nil
}

func parseGeneric(raw string, segs []string, cliRef string) ([]string, string, string, error) {
	// A generic host can serve both /{owner}/{repo} and a single-segment
	// /{repo}. A URL whose second segment is a web marker (for example
	// /repo/blob/main/file) is therefore ambiguous: it is either a deep link
	// into a single-segment repository or a two-segment repository whose name
	// happens to be "blob". Only the single-segment repository URL is
	// supported, so reject the shape instead of cloning the wrong repository.
	// This deliberately rejects an ambiguous two-segment path such as
	// /o/blob/main as well; that is safer than cloning the wrong thing.
	if singleSegmentRepoDeepLink(segs) {
		return nil, "", "", fmt.Errorf("unsupported URL form %q: single-segment repository deep links are not supported; use the repository URL", raw)
	}
	// Gitea's explicit raw/blame selector wins over GitHub's raw/<ref>/... shape,
	// so a GitHub Enterprise ref literally named branch, tag or commit is
	// ambiguous and parses as Gitea. See giteaRawShapeOK.
	if giteaRawShapeOK(segs) {
		ref, sub, err := extractRef(styleGitea, segs[2], segs[3:], cliRef, raw)
		if err != nil {
			return nil, "", "", err
		}
		return segs[:2], ref, sub, nil
	}
	if githubShapeOK(segs) {
		ref, sub, err := extractRef(styleGitHub, segs[2], segs[3:], cliRef, raw)
		if err != nil {
			return nil, "", "", err
		}
		return segs[:2], ref, sub, nil
	}
	if giteaShapeOK(segs) {
		ref, sub, err := extractRef(styleGitea, segs[2], segs[3:], cliRef, raw)
		if err != nil {
			return nil, "", "", err
		}
		return segs[:2], ref, sub, nil
	}
	if i, kind, ok := findGitLabMarker(segs); ok {
		if !gitlabSourceKinds[kind] {
			return nil, "", "", fmt.Errorf("unsupported URL form %q: /-/%s/ pages are not supported; use the repository URL", raw, kind)
		}
		if i == 0 {
			return nil, "", "", fmt.Errorf("unsupported URL form %q: missing repository path before /-/%s/", raw, kind)
		}
		ref, sub, err := extractRef(styleGitLab, kind, segs[i+2:], cliRef, raw)
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

// singleSegmentRepoDeepLink reports whether segs has the shape of a web deep
// link into a single-segment repository: /{repo}/{marker}/... . A valid
// multi-segment shape (GitHub-like, Gitea, or a GitLab "/-/" marker after two
// or more segments) means the marker-looking second segment is actually part
// of a two-segment repository name, so those links are left to the normal
// parser (for example /o/blob/blob/main/file and /o/blob/-/tree/main).
func singleSegmentRepoDeepLink(segs []string) bool {
	if len(segs) < 3 || !isSingleSegmentRepoMarker(segs[1]) {
		return false
	}
	// A GitLab "/-/" marker at index 1 is the single-segment form and stays
	// rejected, even though the following segment can look like a GitHub
	// marker (e.g. /repo/-/tree/main). Only a marker at index >= 2 proves a
	// multi-segment repository.
	if i, _, ok := findGitLabMarker(segs); ok {
		return i < 2
	}
	if githubShapeOK(segs) || giteaShapeOK(segs) || giteaRawShapeOK(segs) {
		return false
	}
	return true
}

// isSingleSegmentRepoMarker reports whether s is a known web marker that can
// follow a repository in a deep link. webMarkers covers the GitHub-like and
// Gitea pages; "-" is GitLab's reserved separator and pipelines is GitLab's
// pipelines page.
func isSingleSegmentRepoMarker(s string) bool {
	return webMarkers[s] || s == "-" || s == "pipelines"
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

// giteaRawShapeOK reports whether a generic-host URL uses Gitea's
// /(raw|blame)/(branch|tag|commit)/... form, where the selector names how the
// ref is interpreted. It needs a segment after the selector (the ref for
// branch/tag, the SHA for commit) plus a path for raw/blame.
//
// A GitHub Enterprise URL using raw/<ref>/<path> is indistinguishable from this
// form when the ref is literally named branch, tag or commit; the Gitea
// selector wins. Any other ref still parses as GitHub.
func giteaRawShapeOK(segs []string) bool {
	if len(segs) < 5 || (segs[2] != "raw" && segs[2] != "blame") {
		return false
	}
	switch segs[3] {
	case "branch", "tag", "commit":
		return true
	}
	return false
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

// findGitLabMarker returns the index and kind of the first GitLab "/-/" marker.
// On GitLab the "/-/" sequence is reserved: a "-" segment followed by a kind
// segment always opens a page, whether or not source-fetch supports that page.
// A "-" followed by another "-" belongs to the repository path (a repository
// can be named "-"), so it is skipped; the second "-" then opens the marker.
func findGitLabMarker(segs []string) (int, string, bool) {
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "-" && segs[i+1] != "-" {
			return i, segs[i+1], true
		}
	}
	return 0, "", false
}

// linkStyle selects the site-specific rules for the segments after a web
// marker.
type linkStyle int

const (
	styleGitHub linkStyle = iota
	styleGitLab
	styleGitea
)

// extractRef parses the segments after a web marker (kind) according to the
// site's link style. It is the single decoder for all three site families; the
// differences are confined to the marker-specific cases below.
func extractRef(style linkStyle, kind string, parts []string, cliRef, raw string) (string, string, error) {
	switch kind {
	case "blob", "tree", "raw", "blame":
		// Gitea's raw/blame links carry an explicit branch/tag/commit
		// selector; the other sites treat the next segment as the ref.
		if style == styleGitea {
			return giteaSelectorRef(kind, parts, cliRef, raw)
		}
		if len(parts) == 0 {
			return "", "", fmt.Errorf("URL %q has no ref after /%s/", raw, kind)
		}
		ref, sub := splitRefAndPath(parts, cliRef)
		return ref, sub, nil
	case "src":
		if style != styleGitea {
			break
		}
		return giteaSelectorRef(kind, parts, cliRef, raw)
	case "commit":
		if len(parts) == 0 {
			return "", "", fmt.Errorf("URL %q has no ref after /commit/", raw)
		}
		return parts[0], strings.Join(parts[1:], "/"), nil
	case "releases":
		if style == styleGitLab {
			// GitLab's /-/releases/{tag} has no "tag" segment.
			return tagRef(kind, parts, raw)
		}
		if len(parts) < 2 || parts[0] != "tag" {
			return "", "", fmt.Errorf("URL %q has no tag after /releases/tag/", raw)
		}
		// A release/tag page has no subpath, so everything after the marker
		// is the tag itself: tags may contain '/'.
		return strings.Join(parts[1:], "/"), "", nil
	case "tags":
		if style != styleGitLab {
			break
		}
		return tagRef(kind, parts, raw)
	}
	if style == styleGitLab {
		return "", "", unsupportedGitLab(raw)
	}
	return "", "", fmt.Errorf("unsupported URL form %q", raw)
}

// tagRef joins the segments after a tag/release marker. The whole remainder is
// the tag, so tags may contain '/'.
func tagRef(kind string, parts []string, raw string) (string, string, error) {
	if len(parts) == 0 {
		return "", "", fmt.Errorf("URL %q has no tag after /%s/", raw, kind)
	}
	return strings.Join(parts, "/"), "", nil
}

// giteaSelectorRef parses Gitea's explicit branch/tag/commit selector after
// src, raw or blame.
func giteaSelectorRef(marker string, parts []string, cliRef, raw string) (string, string, error) {
	if len(parts) < 2 {
		return "", "", fmt.Errorf("URL %q has no ref after /%s/", raw, marker)
	}
	switch parts[0] {
	case "branch", "tag":
		ref, sub := splitRefAndPath(parts[1:], cliRef)
		return ref, sub, nil
	case "commit":
		return parts[1], strings.Join(parts[2:], "/"), nil
	default:
		return "", "", fmt.Errorf("unsupported URL form %q: expected branch, tag or commit after /%s/", raw, marker)
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
