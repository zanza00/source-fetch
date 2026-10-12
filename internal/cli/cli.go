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
		return cmdManifest()
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

func cmdManifest() error {
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
