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

// options holds the global settings parsed from the command line.
type options struct {
	// cacheOverride is the --cache value: it replaces the cache root for
	// this invocation.
	cacheOverride string
}

// parseGlobal extracts the global flags that may precede the command.
func parseGlobal(args []string) (*options, []string, error) {
	opt := &options{}
	rest := args
	for len(rest) > 0 && strings.HasPrefix(rest[0], "--") {
		name, value, hasValue := strings.Cut(strings.TrimPrefix(rest[0], "--"), "=")
		if name != "cache" {
			return nil, nil, fmt.Errorf("unknown option %q", rest[0])
		}
		if !hasValue {
			if len(rest) < 2 {
				return nil, nil, fmt.Errorf("missing value for --cache")
			}
			value = rest[1]
			rest = rest[1:]
		}
		if value == "" {
			return nil, nil, fmt.Errorf("--cache needs a value")
		}
		opt.cacheOverride = value
		rest = rest[1:]
	}
	return opt, rest, nil
}

func dispatch(args []string) error {
	opt, args, err := parseGlobal(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return cmdList(opt)
	}
	switch args[0] {
	case "list":
		return cmdList(opt)
	case "path":
		return cmdPath(opt, args[1:])
	case "peek":
		return cmdPeek(opt, args[1:])
	case "update":
		return cmdUpdate(opt, args[1:])
	case "rm":
		return cmdRm(opt, args[1:])
	case "clean":
		return cmdClean(opt, args[1:])
	case "cache":
		return cmdCache(opt, args[1:])
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
	return cmdFetch(opt, args)
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

// entry resolves a manifest + one named entry + the cache location.
func entry(o *options, name string) (*manifest.Manifest, *manifest.Entry, cache.Location, error) {
	m, err := loadManifest()
	if err != nil {
		return nil, nil, cache.Location{}, err
	}
	e, ok := m.Lookup(name)
	if !ok {
		return nil, nil, cache.Location{}, fmt.Errorf("unknown source %q", name)
	}
	loc, err := o.location(m)
	if err != nil {
		return nil, nil, cache.Location{}, err
	}
	return m, e, loc, nil
}

// location resolves the cache root for a manifest.
func (o *options) location(m *manifest.Manifest) (cache.Location, error) {
	if m == nil {
		return cache.Resolve("", "", o.cacheOverride)
	}
	return cache.Resolve(m.Path, m.CacheDir, o.cacheOverride)
}

// adhocLocation resolves the ad-hoc cache root. The manifest is optional:
// peek works without one.
func (o *options) adhocLocation() (cache.Location, error) {
	m, err := loadManifest()
	if err != nil {
		return cache.ResolveAdhoc("", "", o.cacheOverride)
	}
	return cache.ResolveAdhoc(m.Path, m.CacheDir, o.cacheOverride)
}

// ensureIgnored makes sure a project-local cache is ignored by git, so it
// never shows up in git status. A failure is a warning, not an error: the
// checkout is still worth having.
func ensureIgnored(loc cache.Location) {
	if !loc.Local {
		return
	}
	res, err := cache.EnsureIgnored(loc.Base)
	if err != nil {
		progress("warning: cannot make %s git-ignored: %v", loc.Base, err)
		return
	}
	if res == cache.IgnoreAdded {
		progress("ignoring %s in .git/info/exclude", loc.Base)
	}
}

func cmdFetch(o *options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	name := args[0]
	cliRef := ""
	if len(args) > 1 {
		cliRef = args[1]
	}
	_, e, loc, err := entry(o, name)
	if err != nil {
		return err
	}
	ensureIgnored(loc)
	dir, err := fetch.Ensure(loc.Path, e, resolve.Ref(e, cliRef), false, progress)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

func cmdPath(o *options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	name := args[0]
	sub := ""
	if len(args) > 1 {
		sub = args[1]
	}
	_, e, loc, err := entry(o, name)
	if err != nil {
		return err
	}
	ensureIgnored(loc)
	dir, err := fetch.Ensure(loc.Path, e, resolve.Ref(e, ""), false, progress)
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
func cmdPeek(o *options, args []string) error {
	loc, err := o.adhocLocation()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return listAdhoc(loc)
	}
	url := args[0]
	cliRef := ""
	if len(args) > 1 {
		cliRef = args[1]
	}
	e := &manifest.Entry{Name: cache.AdhocName(url), URL: url, Transport: "git"}
	ensureIgnored(loc)
	dir, err := fetch.Ensure(loc.Path, e, resolve.Ref(e, cliRef), false, progress)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

// listAdhoc prints every tree in the ad-hoc cache with its remote URL.
func listAdhoc(loc cache.Location) error {
	root := loc.Path
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

func cmdUpdate(o *options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	name := args[0]
	_, e, loc, err := entry(o, name)
	if err != nil {
		return err
	}
	ensureIgnored(loc)
	dir, err := fetch.Ensure(loc.Path, e, resolve.Ref(e, ""), true, progress)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

func cmdRm(o *options, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("missing source name")
	}
	target := args[0]
	var dir string
	if looksLikeURL(target) {
		loc, err := o.adhocLocation()
		if err != nil {
			return err
		}
		dir = loc.Dir(cache.AdhocName(target))
	} else {
		_, _, loc, err := entry(o, target)
		if err != nil {
			return err
		}
		dir = loc.Dir(target)
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

func cmdClean(o *options, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: clean")
	}
	m, err := loadManifest()
	if err != nil {
		return err
	}
	loc, err := o.location(m)
	if err != nil {
		return err
	}
	if err := cache.GuardCleanTarget(loc.Path, m.Path); err != nil {
		return err
	}
	if err := os.RemoveAll(loc.Path); err != nil {
		return err
	}
	progress("cleaned %s", loc.Path)
	return nil
}

// cmdCache inspects and configures the cache root.
//
//	source-fetch cache            # print the cache root in use
//	source-fetch cache init [dir] # point the manifest at a local cache dir
//	source-fetch cache ignore     # make sure git ignores the cache root
func cmdCache(o *options, args []string) error {
	if len(args) == 0 {
		return cmdCacheStatus(o)
	}
	switch args[0] {
	case "init":
		return cmdCacheInit(o, args[1:])
	case "ignore":
		return cmdCacheIgnore(o)
	default:
		return fmt.Errorf("unknown cache subcommand %q (want init or ignore)", args[0])
	}
}

// cmdCacheStatus prints the cache root in use on stdout and its kind on
// stderr.
func cmdCacheStatus(o *options) error {
	loc, err := o.statusLocation()
	if err != nil {
		return err
	}
	fmt.Println(loc.Path)
	progress("cache: %s (%s)", loc.Path, cacheKind(loc))
	return nil
}

// statusLocation resolves the cache root of the manifest in use, or of the
// current directory when there is no manifest.
func (o *options) statusLocation() (cache.Location, error) {
	m, err := loadManifest()
	if err != nil {
		return cache.Resolve("", "", o.cacheOverride)
	}
	return o.location(m)
}

// cacheKind describes a cache location for humans.
func cacheKind(loc cache.Location) string {
	switch loc.Source {
	case cache.SourceProject:
		return "project-local"
	case cache.SourceUser:
		return "user cache"
	case cache.SourceEnv:
		return "environment override"
	case cache.SourceManifest:
		return "manifest cache_dir"
	case cache.SourceOverride:
		return "--cache override"
	}
	return loc.Source
}

// cmdCacheInit points the manifest at a local cache directory and makes sure
// git ignores it.
func cmdCacheInit(o *options, args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: cache init [dir]")
	}
	dir := cache.LocalDirName
	if len(args) == 1 {
		dir = args[0]
	}
	path, err := manifestWritePath(false)
	if err != nil {
		return err
	}
	m, err := loadForEdit(path)
	if err != nil {
		return err
	}
	m.CacheDir = dir
	if err := m.Save(); err != nil {
		return err
	}
	progress("cache_dir = %s in %s", dir, path)

	loc, err := o.location(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(loc.Path, 0o755); err != nil {
		return err
	}
	ensureIgnored(loc)
	fmt.Println(loc.Path)
	return nil
}

// cmdCacheIgnore makes sure git ignores the cache root, adding the rule to
// .git/info/exclude when it is missing.
func cmdCacheIgnore(o *options) error {
	loc, err := o.statusLocation()
	if err != nil {
		return err
	}
	res, err := cache.EnsureIgnored(loc.Base)
	if err != nil {
		return err
	}
	switch res {
	case cache.IgnoreAdded:
		progress("ignoring %s in .git/info/exclude", loc.Base)
	case cache.IgnoreAlready:
		progress("%s is already ignored by git", loc.Base)
	default:
		progress("%s is not inside a git repository", loc.Base)
	}
	fmt.Println(loc.Base)
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

func cmdList(o *options) error {
	m, err := loadManifest()
	if err != nil {
		return err
	}
	loc, err := o.location(m)
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
		dir := loc.Dir(e.Name)
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
	fmt.Printf("\ncache:    %s\nmanifest: %s\n", loc.Path, m.Path)
	return nil
}

func usage(w *os.File) {
	fmt.Fprintf(w, `%[1]s — fetch and cache dependency sources on demand

usage: %[1]s [--cache <dir>] [command] [args]

commands:
  (none) | list          list every manifest entry (version, ref, cache, age)
  path <name> [subpath]  ensure cached, print its directory (quiet stdout)
  <name> [ref]           ensure cached, print its directory (one-shot ref)
  peek <url> [ref]       one-off fetch of a git URL in no manifest (ad-hoc cache)
  peek                   list the ad-hoc cache
  update <name>          force a fresh clone at the resolved ref
  rm <name> | <url>      delete one cached tree (manifest entry or ad-hoc)
  clean                  delete the cache for the current manifest
  cache                  print the cache root in use
  cache init [dir]       point the manifest at a local cache dir (.source-fetch)
  cache ignore           make sure git ignores the cache root
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

options:
  --cache <dir>          use <dir> as the cache root for this invocation

environment:
  %[2]s      explicit manifest path
  %[3]s          cache root override (no namespace)
  %[4]s  serve skills from a directory instead of the embedded content

The command prints a plain directory on stdout; progress goes to stderr.
`, prog, manifest.EnvManifest, cache.EnvCache, skills.EnvSkillsDir)
}
