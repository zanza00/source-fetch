package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, CanonicalName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadCanonicalAndAlias(t *testing.T) {
	path := write(t, t.TempDir(), `
[source.a]
url = "https://example.com/a.git"
tag_prefix = "v"

[source.b]
repo = "https://example.com/b.git"
`)
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("got %d entries", len(m.Entries))
	}
	// sorted by name
	if m.Entries[0].Name != "a" || m.Entries[1].Name != "b" {
		t.Fatalf("not sorted: %v", m.Entries)
	}
	if m.Entries[0].Transport != "git" {
		t.Fatalf("default transport = %q", m.Entries[0].Transport)
	}
	if m.Entries[1].URL != "https://example.com/b.git" {
		t.Fatalf("repo alias not applied: %q", m.Entries[1].URL)
	}
}

func TestLoadRejectsBadEntries(t *testing.T) {
	cases := map[string]string{
		"missing url": `
[source.x]
tag_prefix = "v"
`,
		"bad transport": `
[source.x]
url = "https://example.com/x.git"
transport = "hg"
`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(write(t, t.TempDir(), body)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDiscoverWalksUp(t *testing.T) {
	root := t.TempDir()
	want := write(t, root, "[source.a]\nurl = \"https://example.com/a.git\"\n")
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Discover(nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Discover = %s, want %s", got, want)
	}
}
