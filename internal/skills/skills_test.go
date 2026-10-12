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
	var core *Skill
	for i := range found {
		if found[i].Name == "core" {
			core = &found[i]
		}
	}
	if core == nil {
		t.Fatalf("core skill not discovered, got %v", found)
	}
	if core.Hidden {
		t.Error("core skill must not be hidden")
	}
	if core.Description == "" {
		t.Error("core skill has no description")
	}
	if core.Dir != "embedded" {
		t.Errorf("Dir = %q, want %q", core.Dir, "embedded")
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
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"this is a longer description that should be truncated", 20, "this is a longer..."},
	}
	for _, tt := range tests {
		if got := Truncate(tt.in, tt.max); got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
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
