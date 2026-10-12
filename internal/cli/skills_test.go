package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zanza00/source-fetch/internal/skills"
)

func TestSkillsListEmbedded(t *testing.T) {
	t.Setenv(skills.EnvSkillsDir, "")
	var code int
	out := captureStdout(t, func() { code = Run([]string{"skills", "list"}) })
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "core") {
		t.Fatalf("list output missing core: %q", out)
	}
}

func TestSkillsGetCore(t *testing.T) {
	t.Setenv(skills.EnvSkillsDir, "")
	var code int
	out := captureStdout(t, func() { code = Run([]string{"skills", "get", "core"}) })
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "---") {
		t.Fatalf("get output should keep frontmatter, got %q", out)
	}
	if !strings.Contains(out, "source-fetch") {
		t.Fatalf("get output missing content: %q", out)
	}
}

func TestSkillsGetNotFound(t *testing.T) {
	t.Setenv(skills.EnvSkillsDir, "")
	old := os.Stderr
	_, w, _ := os.Pipe()
	os.Stderr = w
	code := Run([]string{"skills", "get", "nope"})
	_ = w.Close()
	os.Stderr = old
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestSkillsJSON(t *testing.T) {
	t.Setenv(skills.EnvSkillsDir, "")
	var code int
	out := captureStdout(t, func() { code = Run([]string{"skills", "list", "--json"}) })
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	var res struct {
		Success bool `json:"success"`
		Data    []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
		t.Fatalf("invalid JSON: %v: %q", err, out)
	}
	if !res.Success || len(res.Data) == 0 || res.Data[0].Name != "core" {
		t.Fatalf("unexpected JSON result: %s", out)
	}

	// Error path: JSON on stdout, exit 1, nothing on stderr.
	errOut := captureStdout(t, func() { code = Run([]string{"skills", "get", "nope", "--json"}) })
	if code != 1 {
		t.Fatalf("error exit code = %d, want 1", code)
	}
	var errRes struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(errOut)), &errRes); err != nil {
		t.Fatalf("invalid error JSON: %v: %q", err, errOut)
	}
	if errRes.Success || errRes.Error == "" {
		t.Fatalf("expected failure envelope, got %s", errOut)
	}
}

func TestSkillsHiddenOmittedFromList(t *testing.T) {
	dir := t.TempDir()
	writeSkillDir(t, dir, "real", "---\nname: real\ndescription: A real skill.\n---\n\n# Real\n")
	writeSkillDir(t, dir, "stub", "---\nname: stub\ndescription: A redirect stub.\nhidden: true\n---\n")
	t.Setenv(skills.EnvSkillsDir, dir)

	var code int
	out := captureStdout(t, func() { code = Run([]string{"skills"}) })
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if strings.Contains(out, "stub") {
		t.Fatalf("hidden skill leaked into list: %q", out)
	}
	if !strings.Contains(out, "real") {
		t.Fatalf("visible skill missing from list: %q", out)
	}

	// get --all serves every non-hidden skill.
	outAll := captureStdout(t, func() { code = Run([]string{"skills", "get", "--all"}) })
	if code != 0 || !strings.Contains(outAll, "# Real") {
		t.Fatalf("get --all: code=%d out=%q", code, outAll)
	}
}

func TestSkillsGetFullIncludesSupplementary(t *testing.T) {
	dir := t.TempDir()
	writeSkillDir(t, dir, "rich", "---\nname: rich\ndescription: With extras.\n---\n\n# Rich\n")
	refDir := filepath.Join(dir, "rich", "references")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "extra.md"), []byte("# Extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(skills.EnvSkillsDir, dir)

	var code int
	out := captureStdout(t, func() { code = Run([]string{"skills", "get", "rich", "--full"}) })
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out, "--- references/extra.md ---") || !strings.Contains(out, "# Extra") {
		t.Fatalf("full output missing supplementary file: %q", out)
	}
}

func writeSkillDir(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
