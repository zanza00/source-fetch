// Package skills serves the skill content bundled in the binary.
//
// Skill content is split across two places, mirroring the two-tier design of
// agent-browser (vercel-labs/agent-browser):
//
//   - skills/source-fetch/ at the repo root — the discovery stub installed
//     into agents by external tooling (`npx skills add zanza00/source-fetch`).
//     It is intentionally thin and stable: trigger words plus a pointer to
//     `source-fetch skills get core`. It never changes between releases, so
//     agents installed long ago are not left with stale instructions.
//   - data/ here — the runtime skill content embedded in the binary and
//     served by the `skills` command. It always matches the installed
//     version, so instructions never drift from the CLI behavior.
package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed data
var embedded embed.FS

// EnvSkillsDir overrides the skills source directory (embedded content is
// used otherwise). Useful in development to serve a working copy.
const EnvSkillsDir = "SOURCE_FETCH_SKILLS_DIR"

// Skill is one discovered skill directory.
type Skill struct {
	Name        string
	Description string
	// Hidden skills are omitted from list output but can still be fetched
	// by name. Used for stubs that only redirect to the real content.
	Hidden bool
	// Dir is the skill's source: "embedded" or the filesystem directory.
	Dir string
	// Content is the full SKILL.md, including frontmatter.
	Content string
	// Supplementary holds references/ and templates/ files (path, content),
	// served only with --full. Sorted by path.
	Supplementary []File
}

// File is a named piece of skill content.
type File struct {
	Path    string
	Content string
}

// Frontmatter is the parsed header of a SKILL.md.
type Frontmatter struct {
	Name        string
	Description string
	Hidden      bool
	OK          bool
}

// ParseFrontmatter parses the YAML frontmatter of a SKILL.md. Only the
// fields this tooling needs are understood: name, description (with folded
// continuation lines) and hidden.
func ParseFrontmatter(content string) Frontmatter {
	trimmed := strings.TrimLeft(content, " \t\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return Frontmatter{}
	}
	rest := trimmed[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Frontmatter{}
	}

	var fm Frontmatter
	lines := strings.Split(rest[:end], "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		var val string
		switch {
		case cutPrefix(line, "name:", &val):
			fm.Name = strings.TrimSpace(val)
		case cutPrefix(line, "description:", &val):
			var b strings.Builder
			b.WriteString(strings.TrimSpace(val))
			// Fold YAML continuation lines (indented).
			for i+1 < len(lines) && isContinuation(lines[i+1]) {
				i++
				b.WriteByte(' ')
				b.WriteString(strings.TrimSpace(lines[i]))
			}
			fm.Description = b.String()
		case cutPrefix(line, "hidden:", &val):
			fm.Hidden = val == "true" || val == "yes"
		}
	}
	if fm.Name == "" {
		return Frontmatter{}
	}
	fm.OK = true
	return fm
}

func cutPrefix(line, prefix string, val *string) bool {
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	*val = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	return true
}

func isContinuation(line string) bool {
	return strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")
}

// Discover returns every skill available to the installed binary, sorted by
// name. It serves the embedded content by default; when EnvSkillsDir points
// at a directory, skills are read from there instead.
func Discover() ([]Skill, error) {
	if dir := os.Getenv(EnvSkillsDir); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return discoverDir(dir)
		}
	}
	return discoverFS(embedded, "data")
}

// discoverFS reads skills from an fs.FS rooted at root.
func discoverFS(fsys fs.FS, root string) ([]Skill, error) {
	entries, err := fs.ReadDir(fsys, root)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		content, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(root, name, "SKILL.md")))
		if err != nil {
			continue
		}
		if s := skillFrom(name, string(content), func(sub string) ([]File, error) {
			return readSubFS(fsys, filepath.ToSlash(filepath.Join(root, name, sub)))
		}); s.Name != "" {
			skills = append(skills, s)
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// discoverDir reads skills from a filesystem directory.
func discoverDir(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, err := os.ReadFile(filepath.Join(path, "SKILL.md"))
		if err != nil {
			continue
		}
		if s := skillFrom(e.Name(), string(content), func(sub string) ([]File, error) {
			return readSubDir(filepath.Join(path, sub))
		}); s.Name != "" {
			s.Dir = path
			skills = append(skills, s)
		}
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

// skillFrom builds a Skill from a directory name and its SKILL.md content.
// readSub serves the references/ and templates/ supplementary files.
func skillFrom(dirName, content string, readSub func(string) ([]File, error)) Skill {
	fm := ParseFrontmatter(content)
	if !fm.OK {
		return Skill{}
	}
	s := Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Hidden:      fm.Hidden,
		Dir:         "embedded",
		Content:     content,
	}
	for _, sub := range []string{"references", "templates"} {
		files, err := readSub(sub)
		if err != nil {
			continue
		}
		s.Supplementary = append(s.Supplementary, files...)
	}
	sort.Slice(s.Supplementary, func(i, j int) bool {
		return s.Supplementary[i].Path < s.Supplementary[j].Path
	})
	return s
}

func readSubFS(fsys fs.FS, dir string) ([]File, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		content, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(dir, e.Name())))
		if err != nil {
			continue
		}
		files = append(files, File{
			Path:    filepath.ToSlash(filepath.Join(filepath.Base(dir), e.Name())),
			Content: string(content),
		})
	}
	return files, nil
}

func readSubDir(dir string) ([]File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []File
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		files = append(files, File{
			Path:    filepath.ToSlash(filepath.Join(filepath.Base(dir), e.Name())),
			Content: string(content),
		})
	}
	return files, nil
}

// Truncate shortens a description to at most max runes, cutting on a space
// boundary and appending an ellipsis.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s
	for i := range s {
		if i > max {
			cut = s[:i]
			break
		}
	}
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return cut + "..."
}
