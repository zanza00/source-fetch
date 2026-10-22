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
		name: "gitlab releases",
		url:  "https://gitlab.com/group/sub/repo/-/releases/v1.0.0",
		want: ParsedURL{RepoURL: "https://gitlab.com/group/sub/repo", Ref: "v1.0.0"},
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
		name:    "codeberg unsupported page",
		url:     "https://codeberg.org/o/r/issues/1",
		wantErr: "unsupported Codeberg URL",
	}, {
		name: "generic host gitlab marker",
		url:  "https://git.example.com/group/sub/repo/-/tree/main/dir",
		want: ParsedURL{RepoURL: "https://git.example.com/group/sub/repo", Ref: "main", Subpath: "dir"},
	}, {
		name: "generic host gitea src",
		url:  "https://gitea.example.com/o/r/src/branch/main/file",
		want: ParsedURL{RepoURL: "https://gitea.example.com/o/r", Ref: "main", Subpath: "file"},
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
		name: "generic host dash repo with tree",
		url:  "https://example.com/group/blob/-/tree/main",
		want: ParsedURL{RepoURL: "https://example.com/group/blob", Ref: "main"},
	}, {
		name:    "generic host base path",
		url:     "https://ghe.example.com/git/o/r/blob/main/file",
		wantErr: "unsupported URL form",
	}, {
		name:    "bitbucket src form",
		url:     "https://bitbucket.org/o/r/src/main/file",
		wantErr: "unsupported URL form",
	}, {
		name:    "generic host unsupported page",
		url:     "https://git.example.com/group/repo/-/issues/1",
		wantErr: "unsupported URL form",
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
