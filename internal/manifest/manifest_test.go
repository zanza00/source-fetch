package manifest

import (
	"os"
	"path/filepath"
	"strings"
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

func TestSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), CanonicalName)
	m := &Manifest{Path: path, Entries: []*Entry{
		{Name: "b", URL: "https://example.com/b.git", Transport: "git", VersionCmd: "b --version", TagPrefix: "v"},
		{Name: "a", URL: "https://example.com/a.git", Transport: "git", Ref: "v1.0.0", DefaultRef: "main"},
	}}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// defaults and empty fields are omitted; canonical url key only
	for _, unwanted := range []string{"transport", "repo"} {
		if strings.Contains(string(body), unwanted) {
			t.Fatalf("save wrote %q:\n%s", unwanted, body)
		}
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(got.Entries))
	}
	a, _ := got.Lookup("a")
	if a.URL != "https://example.com/a.git" || a.Ref != "v1.0.0" || a.DefaultRef != "main" || a.Transport != "git" {
		t.Fatalf("entry a round-tripped wrong: %+v", a)
	}
	b, _ := got.Lookup("b")
	if b.VersionCmd != "b --version" || b.TagPrefix != "v" {
		t.Fatalf("entry b round-tripped wrong: %+v", b)
	}
}

func TestSaveRejectsInvalidEntry(t *testing.T) {
	m := &Manifest{Path: filepath.Join(t.TempDir(), CanonicalName), Entries: []*Entry{
		{Name: "x"}, // missing url
	}}
	if err := m.Save(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSaveKeepsPermissions(t *testing.T) {
	path := write(t, t.TempDir(), "[source.a]\nurl = \"https://example.com/a.git\"\n")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveCreatesMissingDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", CanonicalName)
	m := &Manifest{Path: path, Entries: []*Entry{
		{Name: "a", URL: "https://example.com/a.git", Transport: "git"},
	}}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}
