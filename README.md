# source-fetch

**A CLI and an agent skill for reading dependency source code on demand.**

source-fetch fetches dependency source into a local cache and returns the path,
so you or your coding agent can explore it without vendoring it into your project.
The CLI handles fetching, version resolution, and caching. The companion skill
teaches your agent when and how to use it, with a usage guide bundled in the
binary to match the installed CLI version.

- **On demand only** — nothing is fetched until asked.
- **Version-aware** — resolve a tag from the *installed* version when a
  `version_cmd` is configured, or choose a tag, branch, or commit yourself.
- **Reusable cache** — inside a git repository the checkouts live in the
  project (`.source-fetch/`), git-ignored; elsewhere in the user cache
  directory, namespaced by manifest.
- **One-off `peek`** — fetch any git URL once, in a separate ad-hoc cache.
- **Agent-friendly** — prints a plain directory on stdout, progress on stderr.

## Install the CLI and skill

For coding agents, install both parts: the CLI runs the commands, and the skill
adds the workflow to your agent. They currently require two installation steps.

### 1. Install the CLI

Choose one of the following methods. Homebrew, mise, and eget install prebuilt
binaries, so Go is not required.

**Homebrew** (macOS or Linux):

```sh
brew install zanza00/tap/source-fetch
```

**[mise](https://mise.jdx.dev/dev-tools/backends/github.html)**:

```sh
mise use -g github:zanza00/source-fetch@latest
```

To pin a version for a project, add it to `mise.toml`:

```toml
[tools]
"github:zanza00/source-fetch" = "0.4.1"
```

**[eget](https://github.com/zyedidia/eget)**:

```sh
mkdir -p ~/.local/bin
eget zanza00/source-fetch --to ~/.local/bin
```

Keep `~/.local/bin` on `PATH`, or choose another directory with `--to`.

**Go** (with Go installed):

```sh
go install github.com/zanza00/source-fetch/cmd/source-fetch@latest
```

Or download a binary from [GitHub Releases](https://github.com/zanza00/source-fetch/releases)
for Linux, macOS, or Windows and put it on `PATH`.

Requires `git` and `source-fetch` on `PATH`; Go is only needed for `go install`.

### 2. Install the agent skill

With Node.js/npm available, run:

```sh
npx skills add zanza00/source-fetch
```

Select your coding agent in the installer. The skill tells it to fetch whole
repositories and read them locally, and to load the guides bundled with the CLI
(`source-fetch skills get read`, `manifest`, or `core`), so the usage
instructions come from your installed binary. Updating the CLI also updates
those bundled guides.

The CLI also works directly from a terminal without an agent skill.

## Quick start

With the CLI and skill installed, ask your agent, for example:

> Read the source of my installed chezmoi version and show how it applies templates.

You can also use the CLI directly. Fetch a repository once, without a manifest:

```sh
source-fetch peek https://github.com/twpayne/chezmoi.git
```

For a dependency you revisit, add it to a manifest and configure version detection:

```sh
source-fetch manifest add chezmoi https://github.com/twpayne/chezmoi.git \
  --version-cmd "chezmoi --version" --tag-prefix v
source-fetch path chezmoi
```

`path` prints the cached directory on stdout; progress goes to stderr. Further
calls reuse the checkout when it already matches the resolved ref. If a requested
ref cannot be fetched, the CLI reports the fallback to the default branch on stderr.

## Usage

These are independent command examples. Named sources must already be in your
manifest; the quick start above and `manifest.example.toml` configure `chezmoi`.

```
source-fetch                      # list every entry: versions, refs, cache
source-fetch path chezmoi         # fetch if needed, print dir (quiet stdout)
source-fetch path chezmoi <subpath> # print a subpath inside the cached tree
cd "$(source-fetch path chezmoi)" # read the source
source-fetch chezmoi v2.73.0       # ref override for this call
source-fetch peek <url> [ref]     # one-off fetch of any git URL (ad-hoc cache)
source-fetch peek                 # list ad-hoc cached trees
source-fetch update chezmoi       # force re-clone at the resolved (pinned) ref
source-fetch rm chezmoi           # drop one cached tree
source-fetch rm <url>             # drop one ad-hoc tree
source-fetch clean                # drop the cache for the current manifest
source-fetch cache                # print the cache root in use
source-fetch cache init [dir]     # point the manifest at a local cache dir
source-fetch cache ignore         # make sure git ignores the cache root
source-fetch --cache <dir> path chezmoi  # use another cache root for this call
source-fetch manifest             # which manifest is in use
source-fetch manifest add <name> <url> [--version-cmd <cmd> ...]
                                  # add an entry (manifest created if missing)
source-fetch manifest set <name> key=value  # update fields (empty value clears)
source-fetch manifest rm <name>    # remove an entry (cache untouched)
source-fetch skills               # list skills served by this binary
source-fetch skills get read      # print the guide for reading source
source-fetch version              # print the version
```

An explicit ref leaves that checkout cached without editing the manifest. If
the manifest resolves to the default branch (empty ref), `path` reuses it;
use `update <name>` to restore the default branch.

## Skills

The installed [agent skill](skills/source-fetch/SKILL.md) is a small discovery
stub: it describes when to use source-fetch and points to the usage guides
embedded in the CLI. This keeps operational instructions aligned with the binary
instead of duplicating them in a separately installed skill.

```sh
source-fetch skills list           # name + description of every skill
source-fetch skills get read       # reading source: path, peek, refs
source-fetch skills get manifest   # manifests, version matching, pins
source-fetch skills get core       # commands, cache behavior, troubleshooting
source-fetch skills get core --full  # include references/ and templates/
source-fetch skills get --all      # every visible skill
```

All skills commands support `--json` for structured output.
See [`docs/skills.md`](docs/skills.md) for the full command reference.

## Manifest

Flat TOML, one `[source.<name>]` table per dependency. See
[`docs/manifest.md`](docs/manifest.md) and [`manifest.example.toml`](manifest.example.toml).

Set `$SOURCE_FETCH_MANIFEST` explicitly, or let the CLI walk upward from `$PWD`
for `.source-fetch.toml`, falling back to `~/.config/source-fetch/manifest.toml`.
There is no built-in manifest; `manifest add` creates one if needed.

## One-off peek

`source-fetch peek <url> [ref]` fetches a git URL that lives in no manifest —
for reading the source of something you are investigating, once. It uses a
separate ad-hoc cache, never touches a project namespace, and needs no manifest
at all. `peek` with no URL lists the ad-hoc trees with their remote URLs;
`rm <url>` drops one. Peek clones unreviewed code — it executes nothing from
the repo, but read it before trusting it.

## Cache

Inside a git repository the cache is **project-local**: the repository you
are working in, not the one that owns the manifest. Manifest entries live at
`<project>/.source-fetch/<name>` and one-off `peek` trees at
`<project>/.source-fetch/adhoc/<repo-basename>-<urlhash>`. The directory is
added to `.git/info/exclude` the first time it is used, so it never shows up
in `git status` and the tracked files of the repository stay untouched.

Outside a git repository the cache lives in the user cache directory:
`$XDG_CACHE_HOME`, else `~/.cache` on Linux, `~/Library/Caches` on macOS,
`%LocalAppData%` on Windows. Manifest entries live at
`<base>/source-fetch/<manifest-id>/<name>` and `peek` trees at
`<base>/source-fetch/adhoc/<repo-basename>-<urlhash>`.

The root is picked in this order:

1. `--cache <dir>` — for this invocation only.
2. `$SOURCE_FETCH_CACHE` — replaces the root entirely (no namespace).
3. `cache_dir` in the manifest — relative to the project root, or absolute.
4. `<project>/.source-fetch` — the default inside a git repository.
5. `<base>/source-fetch/<manifest-id>` — the default everywhere else.

The project root is the git toplevel that contains the current directory; when
the current directory is not in a work tree, it is the git toplevel that owns
the manifest, else the manifest's directory. Relative paths from the
environment or the manifest are resolved against the project root, so the
cache does not move when a command runs from a subdirectory. `--cache` is
resolved against the current directory.

```sh
source-fetch cache                # print the root in use
source-fetch cache init           # point the manifest at ./.source-fetch
source-fetch cache init vendor/src # ... or at another directory
source-fetch cache ignore         # write the ignore rule, if it is missing
```

- `<manifest-id>` is the basename of the git toplevel owning the manifest, else
  `path-<hash>`.
- Ad-hoc names are `<repo-basename>-<urlhash>`: same-named repos never collide.
- Each tree is a plain git checkout with a `.source-fetch-ref` marker.
- The cache is safe to delete at any time; `clean` refuses to run on
  empty/`/`/`$HOME`, on the root of a git repository, and on a directory that
  holds the manifest or the current directory. Normally `clean` removes only
  the current manifest's cache; with `$SOURCE_FETCH_CACHE`, it removes the
  entire override, including `adhoc/`.

## Development

For development and contribution instructions, see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT — see [LICENSE](LICENSE).
