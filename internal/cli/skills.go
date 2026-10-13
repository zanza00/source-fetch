package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zanza00/source-fetch/internal/skills"
)

// errSilent marks errors already reported to the user (JSON mode prints
// them as JSON on stdout); Run returns 1 without re-printing them.
var errSilent = errors.New("")

// jsonResult is the envelope for `--json` output, mirroring the shape used
// by agent-browser: {"success": bool, "data"|"error": ...}.
type jsonResult struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

func printJSON(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// cmdSkills dispatches the skills subcommand: list (default) and get.
// Content is served from the binary by default; EnvSkillsDir can override it
// with filesystem content for development.
func cmdSkills(args []string) error {
	jsonMode := false
	rest := []string{}
	for _, a := range args {
		if a == "--json" {
			jsonMode = true
			continue
		}
		rest = append(rest, a)
	}
	sub := "list"
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "list":
		return runSkillsList(jsonMode)
	case "get":
		return runSkillsGet(rest[1:], jsonMode)
	default:
		return fmt.Errorf("unknown skills subcommand %q", sub)
	}
}

func runSkillsList(jsonMode bool) error {
	all, err := skills.Discover()
	if err != nil {
		return err
	}
	visible := []skills.Skill{}
	for _, s := range all {
		if !s.Hidden {
			visible = append(visible, s)
		}
	}

	if jsonMode {
		items := []map[string]string{}
		for _, s := range visible {
			items = append(items, map[string]string{"name": s.Name, "description": s.Description})
		}
		return printJSON(jsonResult{Success: true, Data: items})
	}

	max := 0
	for _, s := range visible {
		if len(s.Name) > max {
			max = len(s.Name)
		}
	}
	for _, s := range visible {
		fmt.Printf("  %-*s  %s\n", max, s.Name, skills.Truncate(s.Description, 70))
	}
	return nil
}

func runSkillsGet(args []string, jsonMode bool) error {
	all := false
	full := false
	names := []string{}
	for _, a := range args {
		switch a {
		case "--all":
			all = true
		case "--full":
			full = true
		default:
			names = append(names, a)
		}
	}

	allSkills, err := skills.Discover()
	if err != nil {
		return err
	}

	byName := map[string]skills.Skill{}
	for _, s := range allSkills {
		byName[s.Name] = s
	}

	// --all serves every non-hidden skill; otherwise named lookups.
	targets := []skills.Skill{}
	if all {
		for _, s := range allSkills {
			if !s.Hidden {
				targets = append(targets, s)
			}
		}
	} else {
		for _, name := range names {
			s, ok := byName[name]
			if !ok {
				return skillNotFound(name, jsonMode)
			}
			targets = append(targets, s)
		}
		if len(names) == 0 {
			return fmt.Errorf("no skill name provided. Usage: %s skills get <name>", prog)
		}
	}

	if jsonMode {
		items := []map[string]interface{}{}
		for _, s := range targets {
			item := map[string]interface{}{"name": s.Name, "content": s.Content}
			if full && len(s.Supplementary) > 0 {
				files := []map[string]string{}
				for _, f := range s.Supplementary {
					files = append(files, map[string]string{"path": f.Path, "content": f.Content})
				}
				item["files"] = files
			}
			items = append(items, item)
		}
		return printJSON(jsonResult{Success: true, Data: items})
	}

	for i, s := range targets {
		if i > 0 {
			fmt.Print("\n---\n\n")
		}
		fmt.Print(s.Content)
		if !strings.HasSuffix(s.Content, "\n") {
			fmt.Println()
		}
		if full {
			for _, f := range s.Supplementary {
				fmt.Printf("\n--- %s ---\n\n", f.Path)
				fmt.Print(f.Content)
				if !strings.HasSuffix(f.Content, "\n") {
					fmt.Println()
				}
			}
		}
	}
	return nil
}

// skillNotFound reports a missing skill; in JSON mode the error goes to
// stdout as JSON (with exit code 1), not stderr.
func skillNotFound(name string, jsonMode bool) error {
	msg := fmt.Sprintf("skill not found: %s", name)
	if !jsonMode {
		return errors.New(msg)
	}
	if err := printJSON(jsonResult{Success: false, Error: msg}); err != nil {
		return err
	}
	return errSilent
}
