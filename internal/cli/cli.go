// Package cli implements the source-fetch command line.
package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/zanza00/source-fetch/internal/cache"
	"github.com/zanza00/source-fetch/internal/fetch"
	"github.com/zanza00/source-fetch/internal/git"
	"github.com/zanza00/source-fetch/internal/manifest"
	"github.com/zanza00/source-fetch/internal/resolve"
	"github.com/zanza00/source-fetch/internal/skills"
)

const prog = "source-fetch"

// Version is the release version. It is injected at build time with
// -X github.com/zanza00/source-fetch/internal/cli.Version=...; see
// versionString for the fallbacks used when it is left at "dev".
var Version = "dev"

// buildInfo is a seam for tests. It returns nil when the binary carries no
// build info.
var buildInfo = func() *debug.BuildInfo {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info
}

// versionString resolves the reported version: the ldflags-injected value
// wins; otherwise a module-version build (`go install pkg@vX.Y.Z`, e.g. via
// mise) falls back to the version recorded in the build info; plain local
// builds report "dev" (or the VCS-derived version on toolchains that record
// one, e.g. "v0.3.0+dirty").
func versionString(injected string, bi *debug.BuildInfo) string {
	if injected != "" && injected != "dev" {
		return injected
	}
	if bi != nil && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

// currentVersion is what `version` prints.
func currentVersion() string {
	return versionString(Version, buildInfo())
}

// Run dispatches args and returns the process exit code.
func Run(args []string) int {
	if err := dispatch(args); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintf(os.Stderr, "%s: %s\n", prog, err)
		}
		return 1
	}
	return 0
}

func dispatch(args []string) error {
	if len(args) == 0 {
		return cmdList()
	}
	switch args[0] {
	case "list":
		return cmdList()
	case "path":
		return cmdPath(args[1:])
	case "peek":
		return cmdPeek(args[1:])
	case "update":
		return cmdUpdate(args[1:])
	case "rm":
		return cmdRm(args[1:])
	case "clean":
		return cmdClean(args[1:])
	case "manifest":
		return cmdManifest(args[1:])
	case "skills":
		return cmdSkills(args[1:])
	case "version", "--version", "-v":
		return cmdVersion()
	case "help", "-h", "--help":
		usage(os.Stderr)
		return nil
	}
	if strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("unknown option %q", args[0])
	}
	return cmdFetch(args)
}

func progress(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", prog, fmt.Sprintf(format, args...))
}

// loadManifest discovers and loads the manifest for the current directory.
func loadManifest() (*manifest.Manifest, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	path, err := manifest.Discover(wd)
	if err != nil {
		return nil, err
	}
	return manifest.Load(path)
}

// entry resolves a manifest + one named entry + the cache root.
func entry(name string) (*manifest.Manifest, *manifest.Entry, string, error) {
	m, err := loadManifest()
	if err != nil {
		return nil, nil, "", err
	}
	e, ok := m.Lookup(name)
	if !ok {
		return nil, nil, "", fmt.Errorf("unknown source %q", name)
	}
	root, err := cache.Root(m.Path)
	if err != nil {
		return nil, nil, "", err
	}
	return m, e, root, nil
}

func cmdFetch(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	name := args[0]
	cliRef := ""
	if len(args) > 1 {
		cliRef = args[1]
	}
	_, e, root, err := entry(name)
	if err != nil {
		return err
	}
	dir, err := fetch.Ensure(root, e, resolve.Ref(e, cliRef), false, progress)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

func cmdPath(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	name := args[0]
	sub := ""
	if len(args) > 1 {
		sub = args[1]
	}
	_, e, root, err := entry(name)
	if err != nil {
		return err
	}
	dir, err := fetch.Ensure(root, e, resolve.Ref(e, ""), false, progress)
	if err != nil {
		return err
	}
	if sub != "" {
		dir = filepath.Join(dir, sub)
	}
	fmt.Println(dir)
	return nil
}

// cmdPeek implements one-off fetches of URLs that live in no manifest: fetch
// (into the ad-hoc cache) and print the directory. With no URL it lists the
// ad-hoc cache.
func cmdPeek(args []string) error {
	root, err := cache.AdhocRoot()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return listAdhoc(root)
	}
	url := args[0]
	cliRef := ""
	if len(args) > 1 {
		cliRef = args[1]
	}
	e := &manifest.Entry{Name: cache.AdhocName(url), URL: url, Transport: "git"}
	dir, err := fetch.Ensure(root, e, resolve.Ref(e, cliRef), false, progress)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

// listAdhoc prints every tree in the ad-hoc cache with its remote URL.
func listAdhoc(root string) error {
	dirs, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	names := []string{}
	for _, d := range dirs {
		if d.IsDir() && fetch.HasGit(filepath.Join(root, d.Name())) {
			names = append(names, d.Name())
		}
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tURL\tREF\tCACHE\tAGE")
	for _, name := range names {
		dir := filepath.Join(root, name)
		url, err := git.RemoteURL(dir)
		if err != nil || url == "" {
			url = "-"
		}
		ref := "(default)"
		age := "-"
		if mk, ok := fetch.ReadMarker(dir); ok {
			if mk.Ref != "" {
				ref = mk.Ref
			}
			if mk.Fetched > 0 {
				days := int(time.Since(time.Unix(mk.Fetched, 0)).Hours() / 24)
				age = fmt.Sprintf("%dd", days)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, url, ref, dir, age)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\ncache: %s\n", root)
	return nil
}

func cmdUpdate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	name := args[0]
	_, e, root, err := entry(name)
	if err != nil {
		return err
	}
	dir, err := fetch.Ensure(root, e, resolve.Ref(e, ""), true, progress)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

func cmdRm(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	target := args[0]
	var dir string
	if looksLikeURL(target) {
		root, err := cache.AdhocRoot()
		if err != nil {
			return err
		}
		dir = cache.Dir(root, cache.AdhocName(target))
	} else {
		_, _, root, err := entry(target)
		if err != nil {
			return err
		}
		dir = cache.Dir(root, target)
	}
	if !fetch.HasGit(dir) {
		return fmt.Errorf("%s is not cached", target)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	progress("removed %s", target)
	return nil
}

// looksLikeURL decides whether rm's argument addresses an ad-hoc tree (a
// remote-style URL) or a manifest name. Plain local paths are treated as
// manifest names.
func looksLikeURL(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "git@")
}

func cmdClean(args []string) error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	root, err := cache.Root(m.Path)
	if err != nil {
		return err
	}
	if err := cache.GuardCleanTarget(root); err != nil {
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	progress("cleaned %s", root)
	return nil
}

func cmdVersion() error {
	fmt.Println(currentVersion())
	return nil
}

func cmdManifest(args []string) error {
	if len(args) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		path, err := manifest.Discover(wd)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	}
	switch args[0] {
	case "add":
		return cmdManifestAdd(args[1:])
	case "set":
		return cmdManifestSet(args[1:])
	case "rm":
		return cmdManifestRm(args[1:])
	default:
		return fmt.Errorf("unknown manifest subcommand %q (want add, set or rm)", args[0])
	}
}

// parseFlags splits args into positionals and a flag map. knownFlags lists
// every accepted flag; a value of true means the flag takes an argument
// (--name <value> or --name=<value>, never empty), false means boolean
// (--name, or --name=true|false). Unknown flags are an error.
func parseFlags(args []string, knownFlags map[string]bool) ([]string, map[string]string, error) {
	pos := []string{}
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		takesValue, ok := knownFlags[name]
		if !ok {
			return nil, nil, fmt.Errorf("unknown flag --%s", name)
		}
		if takesValue {
			if !hasValue {
				if i+1 >= len(args) {
					return nil, nil, fmt.Errorf("missing value for --%s", name)
				}
				i++
				value = args[i]
			}
			if value == "" {
				return nil, nil, fmt.Errorf("--%s needs a value", name)
			}
		} else if !hasValue {
			value = "true"
		}
		flags[name] = value
	}
	return pos, flags, nil
}

// manifestWritePath picks the file that manifest add/set/rm edit: with
// --global the per-user manifest, else $SOURCE_FETCH_MANIFEST, else the
// discovered manifest, else .source-fetch.toml in the current directory.
func manifestWritePath(global bool) (string, error) {
	if global {
		return manifest.UserPath()
	}
	if v := os.Getenv(manifest.EnvManifest); v != "" {
		return filepath.Abs(v)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if path, err := manifest.Discover(wd); err == nil {
		return path, nil
	}
	return filepath.Join(wd, manifest.CanonicalName), nil
}

// loadForEdit loads path when it exists, else returns a fresh manifest for
// it (so add can create the file).
func loadForEdit(path string) (*manifest.Manifest, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &manifest.Manifest{Path: path}, nil
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("manifest %s is a directory", path)
	}
	return manifest.Load(path)
}

// putEntry replaces the entry named e.Name, or appends it.
func putEntry(m *manifest.Manifest, e *manifest.Entry) {
	for i, x := range m.Entries {
		if x.Name == e.Name {
			m.Entries[i] = e
			return
		}
	}
	m.Entries = append(m.Entries, e)
}

func cmdManifestAdd(args []string) error {
	pos, flags, err := parseFlags(args, map[string]bool{
		"version-cmd": true,
		"tag-prefix":  true,
		"ref":         true,
		"default-ref": true,
		"global":      false,
		"force":       false,
	})
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return fmt.Errorf("usage: manifest add <name> <url> [--version-cmd <cmd>] [--tag-prefix <p>] [--ref <ref>] [--default-ref <ref>] [--global] [--force]")
	}
	name, url := pos[0], pos[1]
	if len(pos) > 2 {
		return fmt.Errorf("unexpected argument %q", pos[2])
	}
	path, err := manifestWritePath(flags["global"] == "true")
	if err != nil {
		return err
	}
	m, err := loadForEdit(path)
	if err != nil {
		return err
	}
	if _, exists := m.Lookup(name); exists && flags["force"] != "true" {
		return fmt.Errorf("source %q already exists (use --force to overwrite, or manifest set)", name)
	}
	e := &manifest.Entry{
		Name:       name,
		URL:        url,
		Transport:  "git",
		VersionCmd: flags["version-cmd"],
		TagPrefix:  flags["tag-prefix"],
		Ref:        flags["ref"],
		DefaultRef: flags["default-ref"],
	}
	if err := e.Validate(); err != nil {
		return err
	}
	putEntry(m, e)
	if err := m.Save(); err != nil {
		return err
	}
	progress("added %s to %s", name, path)
	return nil
}

// setEntryField assigns value to the named field of e. An empty value clears
// the field.
func setEntryField(e *manifest.Entry, key, value string) error {
	switch key {
	case "url":
		e.URL = value
	case "version_cmd":
		e.VersionCmd = value
	case "tag_prefix":
		e.TagPrefix = value
	case "ref":
		e.Ref = value
	case "default_ref":
		e.DefaultRef = value
	default:
		return fmt.Errorf("unknown field %q (want url, version_cmd, tag_prefix, ref or default_ref)", key)
	}
	return nil
}

func cmdManifestSet(args []string) error {
	pos, flags, err := parseFlags(args, map[string]bool{"global": false})
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return fmt.Errorf("usage: manifest set <name> <key>=<value>... (url, version_cmd, tag_prefix, ref, default_ref; empty value clears)")
	}
	path, err := manifestWritePath(flags["global"] == "true")
	if err != nil {
		return err
	}
	m, err := loadForEdit(path)
	if err != nil {
		return err
	}
	e, ok := m.Lookup(pos[0])
	if !ok {
		return fmt.Errorf("unknown source %q", pos[0])
	}
	for _, kv := range pos[1:] {
		key, value, found := strings.Cut(kv, "=")
		if !found {
			return fmt.Errorf("expected key=value, got %q", kv)
		}
		if err := setEntryField(e, key, value); err != nil {
			return err
		}
	}
	if err := e.Validate(); err != nil {
		return err
	}
	if err := m.Save(); err != nil {
		return err
	}
	progress("updated %s in %s", pos[0], path)
	return nil
}

func cmdManifestRm(args []string) error {
	pos, flags, err := parseFlags(args, map[string]bool{"global": false})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: manifest rm <name> (removes the manifest entry; the cache is untouched, use rm <name> for that)")
	}
	name := pos[0]
	path, err := manifestWritePath(flags["global"] == "true")
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no manifest at %s", path)
		}
		return err
	}
	m, err := manifest.Load(path)
	if err != nil {
		return err
	}
	idx := -1
	for i, e := range m.Entries {
		if e.Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("unknown source %q", name)
	}
	m.Entries = append(m.Entries[:idx], m.Entries[idx+1:]...)
	if err := m.Save(); err != nil {
		return err
	}
	progress("removed %s from %s (cache untouched; use rm %s to drop it)", name, path, name)
	return nil
}

func cmdList() error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	root, err := cache.Root(m.Path)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tREF\tCACHE\tAGE")
	for _, e := range m.Entries {
		version := resolve.InstalledVersion(e)
		if version == "" {
			version = "-"
		}
		ref := resolve.Ref(e, "")
		if ref == "" {
			ref = "(default)"
		}
		cacheCol := "(not cached)"
		age := "-"
		dir := cache.Dir(root, e.Name)
		if fetch.HasGit(dir) {
			cacheCol = dir
			if mk, ok := fetch.ReadMarker(dir); ok && mk.Fetched > 0 {
				days := int(time.Since(time.Unix(mk.Fetched, 0)).Hours() / 24)
				age = fmt.Sprintf("%dd", days)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, version, ref, cacheCol, age)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("\ncache:    %s\nmanifest: %s\n", root, m.Path)
	return nil
}

func usage(w *os.File) {
	fmt.Fprintf(w, `%[1]s — fetch and cache dependency sources on demand

usage: %[1]s [command] [args]

commands:
  (none) | list          list every manifest entry (version, ref, cache, age)
  path <name> [subpath]  ensure cached, print its directory (quiet stdout)
  <name> [ref]           ensure cached, print its directory (one-shot ref)
  peek <url> [ref]       one-off fetch of a git URL in no manifest (ad-hoc cache)
  peek                   list the ad-hoc cache
  update <name>          force a fresh clone at the resolved ref
  rm <name> | <url>      delete one cached tree (manifest entry or ad-hoc)
  clean                  delete the cache for the current manifest
  manifest               print the manifest path in use
  manifest add <name> <url> [--version-cmd <cmd>] [--tag-prefix <p>]
                         [--ref <ref>] [--default-ref <ref>] [--global]
                         [--force]   add an entry (manifest created if missing)
  manifest set <name> <key>=<value>...
                         set entry fields (url, version_cmd, tag_prefix, ref,
                         default_ref; empty value clears)
  manifest rm <name>     remove an entry from the manifest (cache untouched)
  skills | skills list   list skills served by this binary (name, description)
  skills get <name>      print a skill's content (matches this version)
  skills get --all       print every skill
  version | --version    print the version
  help | -h | --help     print this usage

environment:
  %[2]s      explicit manifest path
  %[3]s          cache root override (no namespace)
  %[4]s  serve skills from a directory instead of the embedded content

The command prints a plain directory on stdout; progress goes to stderr.
`, prog, manifest.EnvManifest, cache.EnvCache, skills.EnvSkillsDir)
}
