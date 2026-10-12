// Package cli implements the source-fetch command line.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/zanza00/source-fetch/internal/cache"
	"github.com/zanza00/source-fetch/internal/fetch"
	"github.com/zanza00/source-fetch/internal/manifest"
	"github.com/zanza00/source-fetch/internal/resolve"
)

const prog = "source-fetch"

// Run dispatches args and returns the process exit code.
func Run(args []string) int {
	if err := dispatch(args); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %s\n", prog, err)
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
	case "update":
		return cmdUpdate(args[1:])
	case "rm":
		return cmdRm(args[1:])
	case "clean":
		return cmdClean(args[1:])
	case "manifest":
		return cmdManifest()
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
	name := args[0]
	_, _, root, err := entry(name)
	if err != nil {
		return err
	}
	dir := cache.Dir(root, name)
	if !fetch.HasGit(dir) {
		return fmt.Errorf("%s is not cached", name)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	progress("removed %s", name)
	return nil
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
  update <name>          force a fresh clone at the resolved ref
  rm <name>              delete one cached tree
  clean                  delete the cache for the current manifest
  manifest               print the manifest path in use
  help | -h | --help     print this usage

environment:
  %[2]s      explicit manifest path
  %[3]s          cache root override (no namespace)

The command prints a plain directory on stdout; progress goes to stderr.
`, prog, manifest.EnvManifest, cache.EnvCache)
}
