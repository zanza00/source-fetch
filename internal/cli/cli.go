// Package cli implements the source-fetch command line.
package cli

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
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
		if isFetchFlag(args[0]) {
			return cmdFetch(args)
		}
		return fmt.Errorf("unknown option %q", args[0])
	}
	return cmdFetch(args)
}

// isFetchFlag reports whether arg is one of the flags accepted by the default
// fetch command, so `source-fetch --deep <name>` routes there while other
// leading flags remain unknown options.
func isFetchFlag(arg string) bool {
	if !strings.HasPrefix(arg, "--") {
		return false
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	return name == "deep" || name == "depth"
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

// fetchFlags are the history flags accepted by every command that fetches:
// path, the default <name> fetch, peek and update.
var fetchFlags = map[string]bool{
	"deep":  false,
	"depth": true,
}

// parseDepthFlags turns --deep/--depth into a fetch depth. Zero flags mean the
// default shallow clone; the two flags are mutually exclusive.
func parseDepthFlags(flags map[string]string) (int, error) {
	_, deepSet := flags["deep"]
	_, depthSet := flags["depth"]
	if deepSet && depthSet {
		return 0, fmt.Errorf("cannot use --deep and --depth together")
	}
	if depthSet {
		n, err := strconv.Atoi(flags["depth"])
		if err != nil || n < 1 {
			return 0, fmt.Errorf("--depth must be a positive integer, got %q", flags["depth"])
		}
		return n, nil
	}
	if deepSet {
		switch flags["deep"] {
		case "true":
			return fetch.DepthFull, nil
		case "false":
			return fetch.DepthDefault, nil
		default:
			return 0, fmt.Errorf("--deep must be true or false, got %q", flags["deep"])
		}
	}
	return fetch.DepthDefault, nil
}

func cmdFetch(args []string) error {
	pos, flags, err := parseFlags(args, fetchFlags)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("missing source name")
	}
	if len(pos) > 2 {
		return fmt.Errorf("usage: <name> [ref] [--deep | --depth N]")
	}
	name := pos[0]
	cliRef := ""
	if len(pos) > 1 {
		cliRef = pos[1]
	}
	depth, err := parseDepthFlags(flags)
	if err != nil {
		return err
	}
	_, e, root, err := entry(name)
	if err != nil {
		return err
	}
	dir, err := fetch.Ensure(root, e, resolve.Ref(e, cliRef), fetch.EnsureOpts{Depth: depth, Progress: progress})
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

func cmdPath(args []string) error {
	pos, flags, err := parseFlags(args, fetchFlags)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("missing source name")
	}
	if len(pos) > 2 {
		return fmt.Errorf("usage: path <name> [subpath] [--deep | --depth N]")
	}
	name := pos[0]
	sub := ""
	if len(pos) > 1 {
		sub = pos[1]
	}
	depth, err := parseDepthFlags(flags)
	if err != nil {
		return err
	}
	_, e, root, err := entry(name)
	if err != nil {
		return err
	}
	dir, err := fetch.Ensure(root, e, resolve.Ref(e, ""), fetch.EnsureOpts{Depth: depth, Progress: progress})
	if err != nil {
		return err
	}
	if sub != "" {
		dir, err = subpathPath(dir, sub)
		if err != nil {
			return err
		}
	}
	fmt.Println(dir)
	return nil
}

// subpathPath joins sub to dir, verifies it exists and that symlinks cannot
// escape the checkout, and returns the stable lexical path.
func subpathPath(dir, sub string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(sub))
	if cleaned == "." {
		return dir, nil
	}
	if !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("path %q escapes the repository", sub)
	}
	p := filepath.Join(dir, cleaned)
	if _, err := os.Stat(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("path %q not found in checkout %s", sub, dir)
		}
		return "", err
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	realP, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realDir, realP)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q resolves outside the checkout", sub)
	}
	return p, nil
}

// cmdPeek implements one-off fetches of URLs that live in no manifest: fetch
// (into the ad-hoc cache) and print the directory. URLs may also be web links
// to a file, directory, commit or tag. With no URL it lists the ad-hoc cache.
func cmdPeek(args []string) error {
	pos, flags, err := parseFlags(args, fetchFlags)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		if len(flags) > 0 {
			return fmt.Errorf("missing URL")
		}
		root, err := cache.AdhocRoot()
		if err != nil {
			return err
		}
		return listAdhoc(root)
	}
	if len(pos) > 2 {
		return fmt.Errorf("usage: peek <url> [ref] [--deep | --depth N]")
	}
	depth, err := parseDepthFlags(flags)
	if err != nil {
		return err
	}
	root, err := cache.AdhocRoot()
	if err != nil {
		return err
	}
	cliRef := ""
	if len(pos) > 1 {
		cliRef = pos[1]
	}
	parsed, err := resolve.ParseWebURL(pos[0], cliRef)
	if err != nil {
		return err
	}
	ref := cmp.Or(cliRef, parsed.Ref)
	e := &manifest.Entry{Name: cache.AdhocName(parsed.RepoURL), URL: parsed.RepoURL, Transport: "git"}
	dir, err := fetch.Ensure(root, e, ref, fetch.EnsureOpts{Depth: depth, Progress: progress})
	if err != nil {
		return err
	}
	if parsed.Subpath != "" {
		dir, err = subpathPath(dir, parsed.Subpath)
		if err != nil {
			return err
		}
	}
	fmt.Println(dir)
	return nil
}

// listAdhoc prints every tree in the ad-hoc cache with its remote URL.
func listAdhoc(root string) error {
	dirs, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	names := []string{}
	for _, d := range dirs {
		if d.IsDir() && fetch.HasGit(filepath.Join(root, d.Name())) {
			names = append(names, d.Name())
		}
	}
	slices.Sort(names)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tURL\tREF\tCACHE\tAGE")
	for _, name := range names {
		dir := filepath.Join(root, name)
		url, err := git.RemoteURL(dir)
		if err != nil {
			url = ""
		}
		url = cmp.Or(url, "-")
		ref := "(default)"
		age := "-"
		if mk, ok := fetch.ReadMarker(dir); ok {
			ref = cmp.Or(mk.Ref, ref)
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
	pos, flags, err := parseFlags(args, fetchFlags)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("missing source name")
	}
	if len(pos) > 1 {
		return fmt.Errorf("usage: update <name> [--deep | --depth N]")
	}
	depth, err := parseDepthFlags(flags)
	if err != nil {
		return err
	}
	name := pos[0]
	_, e, root, err := entry(name)
	if err != nil {
		return err
	}
	dir, err := fetch.Ensure(root, e, resolve.Ref(e, ""), fetch.EnsureOpts{Force: true, Depth: depth, Progress: progress})
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
		parsed, err := resolve.ParseWebURL(target, "")
		if err != nil {
			return err
		}
		dir = cache.Dir(root, cache.AdhocName(parsed.RepoURL))
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
		if errors.Is(err, fs.ErrNotExist) {
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
		return fmt.Errorf("usage: manifest rm <name> (removes the manifest entry; to also delete its cached checkout, run rm <name> first)")
	}
	name := pos[0]
	path, err := manifestWritePath(flags["global"] == "true")
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("no manifest at %s", path)
		}
		return err
	}
	m, err := manifest.Load(path)
	if err != nil {
		return err
	}
	idx := slices.IndexFunc(m.Entries, func(e *manifest.Entry) bool { return e.Name == name })
	if idx == -1 {
		return fmt.Errorf("unknown source %q", name)
	}
	m.Entries = slices.Delete(m.Entries, idx, idx+1)
	if err := m.Save(); err != nil {
		return err
	}
	progress("removed %s from %s (cache untouched)", name, path)
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
		version := cmp.Or(resolve.InstalledVersion(e), "-")
		ref := cmp.Or(resolve.Ref(e, ""), "(default)")
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
  path <name> [subpath] [--deep|--depth N]
                         ensure cached, print its directory (quiet stdout)
  <name> [ref] [--deep|--depth N]
                         ensure cached, print its directory (one-shot ref)
  peek <url|web-link> [ref] [--deep|--depth N]
                         one-off fetch of a git URL or deep link (ad-hoc cache)
  peek                   list the ad-hoc cache
  update <name> [--deep|--depth N]
                         force a fresh clone at the resolved ref
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

history flags (path, <name>, peek, update):
  --deep                 fetch complete history
  --depth N              fetch the last N commits

depth:
  default is a shallow clone (1 commit); --deep fetches full history,
  --depth N fetches the last N commits (for git log / git blame).

environment:
  %[2]s      explicit manifest path
  %[3]s          cache root override (no namespace)
  %[4]s  serve skills from a directory instead of the embedded content

The command prints a plain directory on stdout; progress goes to stderr.
`, prog, manifest.EnvManifest, cache.EnvCache, skills.EnvSkillsDir)
}
