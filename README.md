# source-fetch

Fetch the **source code of a dependency on demand** and cache it locally, so a
human or an agent can read it without vendoring it into the consuming repo.

- **On demand only** — nothing is fetched until asked.
- **Version-matched** — the checkout is pinned to the tag of the *installed*
  version when a `version_cmd` is configured.
- **Namespaced cache** — per-manifest, so two projects never collide.
- **One-off `peek`** — fetch any git URL once, in a separate ad-hoc cache.
- **Agent-friendly** — prints a plain directory on stdout, progress on stderr.

## Install

```sh
go install github.com/zanza00/source-fetch/cmd/source-fetch@latest
```

or grab a binary from the GitHub Releases page (Linux/macOS/Windows).

Requires `git` on `PATH`.

## Usage

```
source-fetch                      # list every entry: versions, refs, cache
source-fetch path topgrade        # fetch if needed, print dir (quiet stdout)
source-fetch path aurora just/    # print a subpath inside the cached tree
cd "$(source-fetch path chezmoi)" # read the source
source-fetch react v18.3.1        # one-shot explicit ref
source-fetch peek <url> [ref]     # one-off fetch of any git URL (ad-hoc cache)
source-fetch peek                 # list ad-hoc cached trees
source-fetch update chezmoi       # force re-clone at the resolved (pinned) ref
source-fetch rm chezmoi           # drop one cached tree
source-fetch rm <url>             # drop one ad-hoc tree
source-fetch clean                # drop the cache for the current manifest
source-fetch manifest             # which manifest is in use
source-fetch skills               # list skills served by this binary
source-fetch skills get core      # print the core skill (usage guide)
source-fetch version              # print the version
```

## Skills

source-fetch ships agent skills in two tiers, so instructions never go stale
(the same design as [agent-browser](https://github.com/vercel-labs/agent-browser)):

- **Discovery stub** (`skills/source-fetch/SKILL.md` in this repo) — install
  it into your agent with `npx skills add zanza00/source-fetch`. It is a
  thin, stable pointer: trigger words plus a redirect to the CLI command
  below, and it rarely changes between releases.
- **Bundled content** — the actual usage guide lives in the binary itself
  (embedded at build time) and is served by the `skills` command, so it
  always matches the installed version:

```sh
source-fetch skills list           # name + description of every skill
source-fetch skills get core       # the core usage guide
source-fetch skills get core --full  # include references/ and templates/
source-fetch skills get --all      # every skill
```

All skills commands support `--json` for structured output. Set
`$SOURCE_FETCH_SKILLS_DIR` to serve skill content from a directory instead
of the embedded copy (development). See [`docs/skills.md`](docs/skills.md).

## Manifest

Flat TOML, one `[source.<name>]` table per dependency. See
[`docs/manifest.md`](docs/manifest.md) and [`manifest.example.toml`](manifest.example.toml).

It is discovered by walking upward from `$PWD` for `.source-fetch.toml`, or set
`$SOURCE_FETCH_MANIFEST` explicitly.

## One-off peek

`source-fetch peek <url> [ref]` fetches a git URL that lives in no manifest —
for reading the source of something you are investigating, once. It uses a
separate ad-hoc cache, never touches a project namespace, and needs no manifest
at all. `peek` with no URL lists the ad-hoc trees with their remote URLs;
`rm <url>` drops one. Peek clones unreviewed code — it executes nothing from
the repo, but read it before trusting it.

## Cache

`${SOURCE_FETCH_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/source-fetch}/<manifest-id>/<name>`
for manifest entries; `.../source-fetch/adhoc/<name>` for one-off `peek` trees.

- `<manifest-id>` is the basename of the git toplevel owning the manifest, else
  `path-<hash>`.
- Ad-hoc names are `<repo-basename>-<urlhash>`: same-named repos never collide.
- Each tree is a plain git checkout with a `.source-fetch-ref` marker.
- `$SOURCE_FETCH_CACHE` overrides the root entirely (no namespace), for tests/CI.
- The cache is safe to delete at any time; `clean` refuses to run on
  empty/`/`/`$HOME`. `clean` is manifest-scoped and leaves ad-hoc trees alone.

## Development

```sh
go build ./...
go test ./...
test/run.sh            # end-to-end smoke test using a local bare repo (offline)
```

## Layout

```
cmd/source-fetch/      entrypoint
internal/cli/          command dispatch
internal/manifest/     manifest model, load, discovery
internal/resolve/      version detection + ref precedence
internal/fetch/        fetch algorithm + at-ref fast path + marker
internal/cache/        cache root, manifest-id, clean guards
internal/git/          git subprocess wrapper
```

## License

MIT — see [LICENSE](LICENSE).
