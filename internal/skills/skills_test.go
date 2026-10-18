package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Frontmatter
	}{
		{
			name:    "basic",
			content: "---\nname: core\ndescription: A test skill.\n---\n\n# Core\n",
			want:    Frontmatter{Name: "core", Description: "A test skill.", OK: true},
		},
		{
			name:    "multiline description folded",
			content: "---\nname: test\ndescription: First line\n  continued here\n  and here\n---\n",
			want:    Frontmatter{Name: "test", Description: "First line continued here and here", OK: true},
		},
		{
			name:    "hidden true",
			content: "---\nname: stub\ndescription: A stub.\nhidden: true\n---\n",
			want:    Frontmatter{Name: "stub", Description: "A stub.", Hidden: true, OK: true},
		},
		{
			name:    "hidden false",
			content: "---\nname: visible\ndescription: Visible.\nhidden: false\n---\n",
			want:    Frontmatter{Name: "visible", Description: "Visible.", OK: true},
		},
		{
			name:    "no frontmatter",
			content: "# Just a heading\n\nNo frontmatter here.\n",
			want:    Frontmatter{},
		},
		{
			name:    "missing name",
			content: "---\ndescription: No name field\n---\n",
			want:    Frontmatter{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFrontmatter(tt.content)
			if got != tt.want {
				t.Fatalf("ParseFrontmatter() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDiscoverEmbedded(t *testing.T) {
	t.Setenv(EnvSkillsDir, "")
	found, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("expected embedded skills, got none")
	}
	byName := map[string]Skill{}
	for _, s := range found {
		byName[s.Name] = s
	}
	for _, name := range []string{"core", "read", "manifest"} {
		s, ok := byName[name]
		if !ok {
			t.Errorf("%s skill not discovered, got %v", name, found)
			continue
		}
		if s.Hidden {
			t.Errorf("%s skill must not be hidden", name)
		}
		if s.Description == "" {
			t.Errorf("%s skill has no description", name)
		}
		if s.Dir != "embedded" {
			t.Errorf("%s Dir = %q, want %q", name, s.Dir, "embedded")
		}
	}
}

func TestDiscoverDirOverride(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha", "---\nname: alpha\ndescription: Alpha.\n---\n\n# Alpha\n")
	writeSkill(t, dir, "stub", "---\nname: stub\ndescription: Stub.\nhidden: true\n---\n")

	// Supplementary files are picked up for --full output.
	refDir := filepath.Join(dir, "alpha", "references")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "auth.md"), []byte("# Auth\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(EnvSkillsDir, dir)
	found, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("discovered %d skills, want 2", len(found))
	}
	if found[0].Name != "alpha" {
		t.Errorf("first skill = %q, want alpha (sorted)", found[0].Name)
	}

	var alpha, stub *Skill
	for i := range found {
		switch found[i].Name {
		case "alpha":
			alpha = &found[i]
		case "stub":
			stub = &found[i]
		}
	}
	if !stub.Hidden {
		t.Error("stub should be hidden")
	}
	if alpha.Dir == "embedded" {
		t.Errorf("alpha.Dir = %q, want the override directory", alpha.Dir)
	}
	if len(alpha.Supplementary) != 1 || alpha.Supplementary[0].Path != "references/auth.md" {
		t.Fatalf("alpha supplementary = %+v, want references/auth.md", alpha.Supplementary)
	}
}

func TestDiscoverIgnoresDirsNoSkillMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "not-a-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-skill", "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvSkillsDir, dir)
	found, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("discovered %v, want none", found)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{
			name: "shorter than max",
			in:   "short",
			max:  10,
			want: "short",
		},
		{
			name: "cuts on space boundary",
			in:   "this is a longer description that should be truncated",
			max:  20,
			want: "this is a longer...",
		},
		{
			name: "exact max+1 without spaces cuts at max",
			in:   "0123456789a",
			max:  10,
			want: "0123456789...",
		},
		{
			name: "multibyte runes counted as runes not bytes",
			in:   "àèìòù",
			max:  5,
			want: "àèìòù",
		},
		{
			name: "multibyte runes truncated at max runes",
			in:   "àèìòùé",
			max:  5,
			want: "àèìòù...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Truncate(tt.in, tt.max); got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}

func writeSkill(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
