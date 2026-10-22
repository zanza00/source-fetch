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
- **Reusable cache** — keep dependency checkouts outside your project,
  namespaced by manifest.
- **One-off `peek`** — fetch any git URL once, in a separate ad-hoc cache.
- **Deep links** — paste a GitHub, GitLab, or Codeberg link to a file,
  directory, commit, or tag and get the local path.
- **History on demand** — shallow by default; `--deep` or `--depth N` when you
  need `git log` or `git blame`.
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
source-fetch path chezmoi --deep  # ensure full history for git log / blame
cd "$(source-fetch path chezmoi)" # read the source
source-fetch chezmoi v2.73.0       # ref override for this call
source-fetch chezmoi --depth 20    # fetch the last 20 commits
source-fetch peek <url|web-link> [ref] [--deep | --depth N]
                                  # one-off fetch of a git URL or deep link
source-fetch peek                 # list ad-hoc cached trees
source-fetch update chezmoi       # force re-clone at the resolved (pinned) ref
source-fetch update chezmoi --deep # force a complete re-clone
source-fetch rm chezmoi           # drop one cached tree
source-fetch rm <url>             # drop one ad-hoc tree
source-fetch clean                # drop the cache for the current manifest
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

## Deep links

`peek` also accepts a web link and prints the local path directly, so you can
paste the URL from a browser instead of rebuilding it by hand:

```sh
source-fetch peek https://github.com/twpayne/chezmoi/blob/master/README.md
# /…/adhoc/chezmoi-<hash>/README.md
source-fetch peek https://github.com/twpayne/chezmoi/tree/master/internal
# /…/adhoc/chezmoi-<hash>/internal
```

The ref and path are taken from the link. An explicit ref argument still wins
when you pass one.

| Host | Supported links |
|---|---|
| GitHub | `/{owner}/{repo}`, `/{owner}/{repo}/(blob\|raw\|blame)/{ref}/{path}`, `/{owner}/{repo}/tree/{ref}[/{path}]`, `/{owner}/{repo}/commit/{sha}[/{path}]`, `/{owner}/{repo}/releases/tag/{tag}` |
| GitLab | `/{namespace…}/{repo}/-/blob/{ref}/{path}`, `/-/tree/{ref}[/{path}]`, `/-/raw/{ref}/{path}`, `/-/blame/{ref}/{path}`, `/-/commit/{sha}[/{path}]`, `/-/tags/{tag}`, `/-/releases/{tag}` |
| Codeberg / Forgejo / Gitea | `/{owner}/{repo}/src/(branch\|tag)/{ref}[/{path}]`, `/{owner}/{repo}/src/commit/{sha}[/{path}]`, `/{owner}/{repo}/(raw\|blame)/(branch\|tag\|commit)/{ref}/{path}`, `/{owner}/{repo}/releases/tag/{tag}` |
| Generic Git host | `/-/…` (GitLab self-hosted), `/src/(branch\|tag\|commit)/…` (Gitea self-hosted), and `/(blob\|tree\|commit\|raw\|blame)/…` at the second path segment (GitHub Enterprise without a base path) |

`rm` accepts the same links, so `peek <repo>.git` and
`rm <deep-link>` address the same cache entry.

Limits:

- GitHub issues, pulls, actions, search and similar pages are rejected with an
  error instead of being cloned as a repository.
- GitLab legacy links without `/-/` are rejected on `gitlab.com`. On an
  unknown host they may be read as a repository path and fail at clone time.
- Bitbucket, Azure DevOps, `raw.githubusercontent.com` and GitHub Enterprise
  installations behind a base path are not supported.
- A ref containing `/` is ambiguous in a URL: without an explicit ref, the
  first segment becomes the ref and the rest the subpath. Pass the ref to
  disambiguate, for example `peek <url> release/1.0`.

## History depth

Checkouts are shallow by default: one commit, enough to read the current tree.
`git log`, `git blame`, and history searches need more:

```sh
source-fetch path chezmoi --depth 20   # last 20 commits
source-fetch path chezmoi --deep       # complete history
source-fetch peek <url> --depth 50
source-fetch update chezmoi --deep     # forced complete re-clone
```

The flags work after the command for `path`, the default `<name>` fetch,
`peek`, and `update`; `source-fetch --deep <name>` is also accepted for the
default fetch. `--deep` and `--depth` are mutually exclusive, and `--depth`
must be a positive integer.

An existing checkout is deepened in place: the directory and HEAD stay where
they are, and a failed upgrade leaves the previous history intact. Upgrades
never reduce history, so `--depth N` is a no-op when the cache already has at
least N commits or full history. `update` is different: it always re-clones at
the requested depth.

## Cache

By default, `<base>` is `$XDG_CACHE_HOME` or the platform's user cache directory:
`~/.cache` on Linux, `~/Library/Caches` on macOS, and `%LocalAppData%` on Windows.
Manifest entries live at `<base>/source-fetch/<manifest-id>/<name>`;
one-off `peek` trees live at `<base>/source-fetch/adhoc/<repo-basename>-<urlhash>`.

With `$SOURCE_FETCH_CACHE`, entries instead live at `<override>/<name>` and
peek trees at `<override>/adhoc/<repo-basename>-<urlhash>`, without a per-manifest
namespace.

- `<manifest-id>` is the basename of the git toplevel owning the manifest, else
  `path-<hash>`.
- Ad-hoc names are `<repo-basename>-<urlhash>`: same-named repos never collide.
  The hash is computed from a canonical key, so spelling variants of the same
  repository (trailing `/`, query/fragment, host case, default port, and `.git`
  on known hosts) share one tree. HTTP, SSH and scp-like URLs stay distinct.
  Trees created by versions before this canonicalisation are not migrated and
  may remain orphaned; they are disposable.
- Each tree is a plain git checkout with a `.source-fetch-ref` marker recording
  the requested ref, actual HEAD commit, fetch time and history depth
  (`depth=1|N|full`). Markers written by older versions have no `depth=` and
  are still read; the depth is informational because the real git state decides
  whether an upgrade is needed.
- `$SOURCE_FETCH_CACHE` overrides the root entirely (no namespace).
- The cache is safe to delete at any time; `clean` refuses to run on
  empty/`/`/`$HOME`. Normally `clean` removes only the current manifest's cache;
  with `$SOURCE_FETCH_CACHE`, it removes the entire override, including `adhoc/`.

## Development

For development and contribution instructions, see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT — see [LICENSE](LICENSE).
