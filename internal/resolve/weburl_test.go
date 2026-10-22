package resolve

import (
	"cmp"
	"strings"
	"testing"
)

func TestParseWebURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		cliRef  string
		want    ParsedURL // Ref is the effective ref: cmp.Or(cliRef, parsed.Ref)
		wantErr string
	}{{
		name: "github blob with query and fragment",
		url:  "https://github.com/o/r/blob/main/dir/file.go?plain=1#L42",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "main", Subpath: "dir/file.go"},
	}, {
		name:    "github blob with an empty ref segment",
		url:     "https://github.com/o/r/blob//file.go",
		wantErr: "empty path segment",
	}, {
		name:    "github tree with an empty ref segment",
		url:     "https://github.com/o/r/tree//dir",
		wantErr: "empty path segment",
	}, {
		name: "github blob with a dot ref segment",
		url:  "https://github.com/o/r/blob/./main/dir/file.go",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "main", Subpath: "dir/file.go"},
	}, {
		name: "github tree",
		url:  "https://github.com/o/r/tree/main/dir",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "main", Subpath: "dir"},
	}, {
		name: "github commit",
		url:  "https://github.com/o/r/commit/abc123/dir/file",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "abc123", Subpath: "dir/file"},
	}, {
		name: "github raw",
		url:  "https://github.com/o/r/raw/main/file",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "github blame",
		url:  "https://github.com/o/r/blame/main/file",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "github releases tag",
		url:  "https://github.com/o/r/releases/tag/v1.2.3",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "v1.2.3"},
	}, {
		name: "github releases tag with slash",
		url:  "https://github.com/o/r/releases/tag/release/1.0",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "release/1.0"},
	}, {
		name: "github releases tag with slash and prerelease suffix",
		url:  "https://github.com/o/r/releases/tag/release/1.0.0-rc.1",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "release/1.0.0-rc.1"},
	}, {
		name: "github repo with .git and trailing slash",
		url:  "https://github.com/o/r.git/",
		want: ParsedURL{RepoURL: "https://github.com/o/r.git"},
	}, {
		name: "github uppercase scheme and host",
		url:  "HTTPS://GitHub.COM/o/r",
		want: ParsedURL{RepoURL: "https://github.com/o/r"},
	}, {
		name: "github path containing a dash directory",
		url:  "https://github.com/o/r/blob/main/dir/-/file",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "main", Subpath: "dir/-/file"},
	}, {
		name: "github repo named releases",
		url:  "https://github.com/o/releases/blob/main/x",
		want: ParsedURL{RepoURL: "https://github.com/o/releases", Ref: "main", Subpath: "x"},
	}, {
		name: "gitlab subgroups blob",
		url:  "https://gitlab.com/group/sub/repo/-/blob/main/dir/file",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "main", Subpath: "dir/file"},
	}, {
		name: "gitlab tree",
		url:  "https://gitlab.com/group/sub/repo/-/tree/main/dir",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "main", Subpath: "dir"},
	}, {
		name: "gitlab commit",
		url:  "https://gitlab.com/group/sub/repo/-/commit/abc123/dir",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "abc123", Subpath: "dir"},
	}, {
		name: "gitlab tags",
		url:  "https://gitlab.com/group/sub/repo/-/tags/v1.0.0",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "v1.0.0"},
	}, {
		name: "gitlab tags with slash",
		url:  "https://gitlab.com/group/sub/repo/-/tags/release/1.0",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "release/1.0"},
	}, {
		name: "gitlab tags with slash and prerelease suffix",
		url:  "https://gitlab.com/group/sub/repo/-/tags/release/1.0.0-rc.1",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "release/1.0.0-rc.1"},
	}, {
		name: "gitlab releases",
		url:  "https://gitlab.com/group/sub/repo/-/releases/v1.0.0",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "v1.0.0"},
	}, {
		name: "gitlab releases with slash",
		url:  "https://gitlab.com/group/sub/repo/-/releases/release/1.0",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "release/1.0"},
	}, {
		name: "gitlab repo named dash",
		url:  "https://gitlab.com/group/sub/-/-/blob/main/foo",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/-", Ref: "main", Subpath: "foo"},
	}, {
		name:    "gitlab legacy blob link",
		url:     "https://gitlab.com/group/repo/blob/main/x",
		wantErr: "legacy /blob/ links are not supported",
	}, {
		name:    "gitlab legacy tree link",
		url:     "https://gitlab.com/group/repo/tree/main",
		wantErr: "legacy /tree/ links are not supported",
	}, {
		name:    "gitlab unsupported page",
		url:     "https://gitlab.com/group/repo/-/issues/1",
		wantErr: "unsupported GitLab URL",
	}, {
		name:    "gitlab unsupported commits page",
		url:     "https://gitlab.com/group/repo/-/commits/main",
		wantErr: "unsupported GitLab URL",
	}, {
		name:    "gitlab unsupported compare page",
		url:     "https://gitlab.com/group/repo/-/compare/main...dev",
		wantErr: "unsupported GitLab URL",
	}, {
		name:    "gitlab unsupported branches page",
		url:     "https://gitlab.com/group/repo/-/branches",
		wantErr: "unsupported GitLab URL",
	}, {
		name:    "gitlab unsupported pipelines page",
		url:     "https://gitlab.com/group/repo/-/pipelines/123",
		wantErr: "unsupported GitLab URL",
	}, {
		name:    "gitlab root-level unsupported page",
		url:     "https://gitlab.com/-/snippets/1",
		wantErr: "unsupported GitLab URL",
	}, {
		name:    "gitlab marker without repository path",
		url:     "https://gitlab.com/-/blob/main/x",
		wantErr: "missing repository path",
	}, {
		name:    "gitlab marker with only a namespace",
		url:     "https://gitlab.com/group/-/blob/main/x",
		wantErr: "missing repository path",
	}, {
		name: "gitlab repo named dash tree",
		url:  "https://gitlab.com/group/sub/-/-/tree/main/dir",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/-", Ref: "main", Subpath: "dir"},
	}, {
		name: "codeberg src branch",
		url:  "https://codeberg.org/o/r/src/branch/main/dir/file",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "main", Subpath: "dir/file"},
	}, {
		name: "codeberg src tag",
		url:  "https://codeberg.org/o/r/src/tag/v1.0/dir",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "v1.0", Subpath: "dir"},
	}, {
		name: "codeberg src commit",
		url:  "https://codeberg.org/o/r/src/commit/abc123/dir",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "abc123", Subpath: "dir"},
	}, {
		name: "codeberg commit",
		url:  "https://codeberg.org/o/r/commit/abc123/dir/file",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "abc123", Subpath: "dir/file"},
	}, {
		name: "codeberg commit without path",
		url:  "https://codeberg.org/o/r/commit/abc123",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "abc123"},
	}, {
		name: "codeberg repo named commit",
		url:  "https://codeberg.org/o/commit/src/branch/main/file",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/commit", Ref: "main", Subpath: "file"},
	}, {
		name: "codeberg raw branch",
		url:  "https://codeberg.org/o/r/raw/branch/main/file",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "codeberg blame branch",
		url:  "https://codeberg.org/o/r/blame/branch/main/file",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "codeberg releases tag",
		url:  "https://codeberg.org/o/r/releases/tag/v1.0",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "v1.0"},
	}, {
		name: "codeberg releases tag with slash",
		url:  "https://codeberg.org/o/r/releases/tag/release/1.0",
		want: ParsedURL{RepoURL: "https://codeberg.org/o/r", Ref: "release/1.0"},
	}, {
		name:    "codeberg unsupported page",
		url:     "https://codeberg.org/o/r/issues/1",
		wantErr: "unsupported Codeberg URL",
	}, {
		name: "gitea raw branch",
		url:  "https://gitea.com/o/r/raw/branch/main/file",
		want: ParsedURL{RepoURL: "https://gitea.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "gitea commit",
		url:  "https://gitea.com/o/r/commit/abc123/dir",
		want: ParsedURL{RepoURL: "https://gitea.com/o/r", Ref: "abc123", Subpath: "dir"},
	}, {
		name:    "gitea unsupported page",
		url:     "https://gitea.com/o/r/issues/1",
		wantErr: "unsupported Gitea URL",
	}, {
		name: "generic host gitlab marker",
		url:  "https://git.example.com/group/sub/repo/-/tree/main/dir",
		want: ParsedURL{RepoURL: "https://git.example.com/group/sub/repo", Ref: "main", Subpath: "dir"},
	}, {
		name: "generic host gitlab tags with slash",
		url:  "https://git.example.com/group/sub/repo/-/tags/release/1.0",
		want: ParsedURL{RepoURL: "https://git.example.com/group/sub/repo", Ref: "release/1.0"},
	}, {
		name: "generic host gitlab releases with slash",
		url:  "https://git.example.com/group/sub/repo/-/releases/release/1.0",
		want: ParsedURL{RepoURL: "https://git.example.com/group/sub/repo", Ref: "release/1.0"},
	}, {
		name: "generic host gitea src",
		url:  "https://gitea.example.com/o/r/src/branch/main/file",
		want: ParsedURL{RepoURL: "https://gitea.example.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "generic host gitea raw branch",
		url:  "https://gitea.example.com/o/r/raw/branch/main/file",
		want: ParsedURL{RepoURL: "https://gitea.example.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "generic host gitea blame branch",
		url:  "https://gitea.example.com/o/r/blame/branch/main/file",
		want: ParsedURL{RepoURL: "https://gitea.example.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "generic host gitea raw commit",
		url:  "https://gitea.example.com/o/r/raw/commit/abc123/file",
		want: ParsedURL{RepoURL: "https://gitea.example.com/o/r", Ref: "abc123", Subpath: "file"},
	}, {
		name: "generic host github-like raw",
		url:  "https://ghe.example.com/o/r/raw/main/file",
		want: ParsedURL{RepoURL: "https://ghe.example.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "generic host github-like blob",
		url:  "https://ghe.example.com/o/r/blob/main/file",
		want: ParsedURL{RepoURL: "https://ghe.example.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "generic host github-like commit",
		url:  "https://ghe.example.com/o/r/commit/abc123/file",
		want: ParsedURL{RepoURL: "https://ghe.example.com/o/r", Ref: "abc123", Subpath: "file"},
	}, {
		name: "generic host single segment repo",
		url:  "https://example.com/repo",
		want: ParsedURL{RepoURL: "https://example.com/repo"},
	}, {
		name:    "generic host single segment repo blob deep link",
		url:     "https://example.com/repo/blob/main/file",
		wantErr: "single-segment repository deep links are not supported",
	}, {
		name:    "generic host single segment repo gitea deep link",
		url:     "https://example.com/repo/src/branch/main",
		wantErr: "single-segment repository deep links are not supported",
	}, {
		name:    "generic host single segment repo commit deep link",
		url:     "https://example.com/repo/commit/abc123",
		wantErr: "single-segment repository deep links are not supported",
	}, {
		name:    "generic host single segment repo gitlab deep link",
		url:     "https://example.com/repo/-/tree/main",
		wantErr: "single-segment repository deep links are not supported",
	}, {
		name: "generic host two segment repo",
		url:  "https://example.com/o/r",
		want: ParsedURL{RepoURL: "https://example.com/o/r"},
	}, {
		name: "generic host two segment repo named like a marker",
		url:  "https://example.com/o/blob",
		want: ParsedURL{RepoURL: "https://example.com/o/blob"},
	}, {
		name:    "generic host ambiguous repo named like a marker with deep link",
		url:     "https://example.com/o/blob/main",
		wantErr: "single-segment repository deep links are not supported",
	}, {
		name: "generic host two segment repo named like a marker github-like deep link",
		url:  "https://example.com/o/blob/blob/main/file",
		want: ParsedURL{RepoURL: "https://example.com/o/blob", Ref: "main", Subpath: "file"},
	}, {
		name: "generic host two segment repo named like a marker gitea deep link",
		url:  "https://example.com/o/blob/src/branch/main",
		want: ParsedURL{RepoURL: "https://example.com/o/blob", Ref: "main"},
	}, {
		name: "generic host two segment repo named like a marker gitlab deep link",
		url:  "https://example.com/o/blob/-/tree/main",
		want: ParsedURL{RepoURL: "https://example.com/o/blob", Ref: "main"},
	}, {
		name: "generic host two segment github-like deep link",
		url:  "https://example.com/o/r/blob/main/file",
		want: ParsedURL{RepoURL: "https://example.com/o/r", Ref: "main", Subpath: "file"},
	}, {
		name: "generic host dash repo with tree",
		url:  "https://example.com/group/blob/-/tree/main",
		want: ParsedURL{RepoURL: "https://example.com/group/blob", Ref: "main"},
	}, {
		name:    "generic host base path",
		url:     "https://ghe.example.com/git/o/r/blob/main/file",
		wantErr: "unsupported URL form",
	}, {
		name: "bitbucket repo",
		url:  "https://bitbucket.org/o/r",
		want: ParsedURL{RepoURL: "https://bitbucket.org/o/r"},
	}, {
		name: "bitbucket repo with .git and trailing slash",
		url:  "https://bitbucket.org/o/r.git/",
		want: ParsedURL{RepoURL: "https://bitbucket.org/o/r.git"},
	}, {
		name:    "bitbucket src form",
		url:     "https://bitbucket.org/o/r/src/main/file",
		wantErr: "unsupported Bitbucket URL",
	}, {
		name:    "bitbucket raw form",
		url:     "https://bitbucket.org/o/r/raw/abc123/file",
		wantErr: "unsupported Bitbucket URL",
	}, {
		name:    "bitbucket commits page",
		url:     "https://bitbucket.org/o/r/commits/main",
		wantErr: "unsupported Bitbucket URL",
	}, {
		name:    "bitbucket workspace page",
		url:     "https://bitbucket.org/o",
		wantErr: "unsupported Bitbucket URL",
	}, {
		name:    "raw githubusercontent file page",
		url:     "https://raw.githubusercontent.com/o/r/main/file",
		wantErr: "raw file pages are not supported",
	}, {
		name: "azure devops repo",
		url:  "https://dev.azure.com/org/project/_git/repo",
		want: ParsedURL{RepoURL: "https://dev.azure.com/org/project/_git/repo"},
	}, {
		name: "azure devops legacy visualstudio repo",
		url:  "https://org.visualstudio.com/project/_git/repo",
		want: ParsedURL{RepoURL: "https://org.visualstudio.com/project/_git/repo"},
	}, {
		name:    "azure devops deep link",
		url:     "https://dev.azure.com/org/project/_git/repo?path=/foo",
		wantErr: "unsupported Azure DevOps URL",
	}, {
		name:    "azure devops legacy visualstudio deep link",
		url:     "https://org.visualstudio.com/project/_git/repo?path=/foo",
		wantErr: "unsupported Azure DevOps URL",
	}, {
		name:    "azure devops project page",
		url:     "https://dev.azure.com/org/project",
		wantErr: "unsupported Azure DevOps URL",
	}, {
		name:    "generic host unsupported page",
		url:     "https://git.example.com/group/repo/-/issues/1",
		wantErr: "unsupported URL form",
	}, {
		name:    "generic host unsupported gitlab pipelines",
		url:     "https://git.example.com/group/repo/-/pipelines/1",
		wantErr: "unsupported URL form",
	}, {
		name:    "generic host unsupported gitlab branches",
		url:     "https://git.example.com/group/repo/-/branches",
		wantErr: "unsupported URL form",
	}, {
		name:    "generic host root-level unsupported gitlab page",
		url:     "https://git.example.com/-/snippets/1",
		wantErr: "unsupported URL form",
	}, {
		name: "generic host repo named dash",
		url:  "https://git.example.com/group/-/-/blob/main/foo",
		want: ParsedURL{RepoURL: "https://git.example.com/group/-", Ref: "main", Subpath: "foo"},
	}, {
		name:    "github issues",
		url:     "https://github.com/o/r/issues/1",
		wantErr: "unsupported GitHub URL",
	}, {
		name:    "github pulls",
		url:     "https://github.com/o/r/pulls/1",
		wantErr: "unsupported GitHub URL",
	}, {
		name:    "github search",
		url:     "https://github.com/search?q=x",
		wantErr: "unsupported GitHub URL",
	}, {
		name:    "github blob without ref",
		url:     "https://github.com/o/r/blob",
		wantErr: `has no ref after /blob/`,
	}, {
		name:    "github releases without tag",
		url:     "https://github.com/o/r/releases",
		wantErr: `has no tag after /releases/tag/`,
	}, {
		name:    "codeberg releases without tag",
		url:     "https://codeberg.org/o/r/releases/tag",
		wantErr: `has no tag after /releases/tag/`,
	}, {
		name:    "gitlab tags without tag",
		url:     "https://gitlab.com/group/repo/-/tags/",
		wantErr: `has no tag after /tags/`,
	}, {
		name:    "gitlab releases without tag",
		url:     "https://gitlab.com/group/repo/-/releases/",
		wantErr: `has no tag after /releases/`,
	}, {
		name:   "cli ref overrides url ref",
		url:    "https://github.com/o/r/blob/main/foo.go",
		cliRef: "v2",
		want:   ParsedURL{RepoURL: "https://github.com/o/r", Ref: "v2", Subpath: "foo.go"},
	}, {
		name:   "cli ref with slash",
		url:    "https://github.com/o/r/blob/release/1.0/foo.go",
		cliRef: "release/1.0",
		want:   ParsedURL{RepoURL: "https://github.com/o/r", Ref: "release/1.0", Subpath: "foo.go"},
	}, {
		name: "url ref with slash without cli ref",
		url:  "https://github.com/o/r/blob/release/1.0/foo.go",
		want: ParsedURL{RepoURL: "https://github.com/o/r", Ref: "release", Subpath: "1.0/foo.go"},
	}, {
		name:   "cli ref with slash in generic gitlab",
		url:    "https://git.example.com/group/repo/-/tree/release/1.0/dir",
		cliRef: "release/1.0",
		want:   ParsedURL{RepoURL: "https://git.example.com/group/repo", Ref: "release/1.0", Subpath: "dir"},
	}, {
		name:    "dotdot traversal",
		url:     "https://github.com/o/r/blob/main/../../etc/passwd",
		wantErr: "escapes the repository",
	}, {
		name:    "encoded dotdot traversal",
		url:     "https://github.com/o/r/blob/main/%2e%2e/etc/passwd",
		wantErr: "escapes the repository",
	}, {
		name:    "traversal in repository path",
		url:     "https://github.com/../etc",
		wantErr: "escapes the repository",
	}, {
		name:    "encoded dotdot ref in blob",
		url:     "https://github.com/acme/tool/blob/%2e%2e/file.txt",
		wantErr: "invalid ref",
	}, {
		name:    "dotdot ref in tree",
		url:     "https://github.com/o/r/tree/..",
		wantErr: "invalid ref",
	}, {
		name:    "dotdot ref in commit",
		url:     "https://github.com/o/r/commit/..",
		wantErr: "invalid ref",
	}, {
		name:    "nul byte ref",
		url:     "https://github.com/o/r/blob/main%00/file.txt",
		wantErr: "invalid ref",
	}, {
		name:    "newline ref",
		url:     "https://github.com/o/r/blob/main%0A/file.txt",
		wantErr: "invalid ref",
	}, {
		name:    "space ref",
		url:     "https://github.com/o/r/blob/main%20foo/file.txt",
		wantErr: "invalid ref",
	}, {
		name:    "colon ref",
		url:     "https://github.com/o/r/blob/main:foo/file.txt",
		wantErr: "invalid ref",
	}, {
		name:    "malformed URL",
		url:     "https://[::1",
		wantErr: "invalid URL",
	}, {
		name:    "missing host",
		url:     "https:///o/r",
		wantErr: "missing host",
	}, {
		name:    "empty URL",
		url:     "   ",
		wantErr: "missing URL",
	}, {
		name: "local absolute path passthrough",
		url:  "/tmp/repo",
		want: ParsedURL{RepoURL: "/tmp/repo"},
	}, {
		name: "local relative path passthrough",
		url:  "./repo",
		want: ParsedURL{RepoURL: "./repo"},
	}, {
		name: "file URL passthrough",
		url:  "file:///tmp/repo",
		want: ParsedURL{RepoURL: "file:///tmp/repo"},
	}, {
		name: "ssh URL passthrough",
		url:  "ssh://git@example.com/o/r",
		want: ParsedURL{RepoURL: "ssh://git@example.com/o/r"},
	}, {
		name: "scp-like passthrough",
		url:  "git@example.com:o/r",
		want: ParsedURL{RepoURL: "git@example.com:o/r"},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWebURL(tt.url, tt.cliRef)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseWebURL(%q, %q) = %+v, want error containing %q", tt.url, tt.cliRef, got, tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseWebURL(%q, %q): %v", tt.url, tt.cliRef, err)
			}
			got.Ref = cmp.Or(tt.cliRef, got.Ref)
			if got != tt.want {
				t.Fatalf("ParseWebURL(%q, %q) = %+v, want %+v", tt.url, tt.cliRef, got, tt.want)
			}
		})
	}
}

func TestParseWebURLCommitRefIsNotConsumedByCLIRef(t *testing.T) {
	got, err := ParseWebURL("https://github.com/o/r/commit/abc123/dir/file", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if got.RepoURL != "https://github.com/o/r" {
		t.Fatalf("RepoURL = %q", got.RepoURL)
	}
	if got.Ref != "abc123" {
		t.Fatalf("parsed Ref = %q, want abc123", got.Ref)
	}
	if got.Subpath != "dir/file" {
		t.Fatalf("Subpath = %q, want dir/file (the SHA must not leak into it)", got.Subpath)
	}
}

func TestParseWebURLPreservesUserinfoAndPort(t *testing.T) {
	got, err := ParseWebURL("https://user:pass@ghe.example.com:8443/o/r/blob/main/file", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.RepoURL != "https://user:pass@ghe.example.com:8443/o/r" {
		t.Fatalf("RepoURL = %q", got.RepoURL)
	}
}

func TestCleanSubpathRejectsAbsoluteAndTraversal(t *testing.T) {
	for _, sub := range []string{"..", "../x", "a/../../b", "/etc/passwd", "a\x00b"} {
		if _, err := cleanSubpath(sub); err == nil {
			t.Errorf("cleanSubpath(%q) should fail", sub)
		}
	}
	got, err := cleanSubpath("a/./b/../c")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a/c" {
		t.Fatalf("cleanSubpath = %q, want a/c", got)
	}
}
