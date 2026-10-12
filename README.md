# source-fetch

Fetch the **source code of a dependency on demand** and cache it locally, so a
human or an agent can read it without vendoring it into the consuming repo.

- **On demand only** — nothing is fetched until asked.
- **Version-matched** — the checkout is pinned to the tag of the *installed*
  version when a `version_cmd` is configured.
- **Namespaced cache** — per-manifest, so two projects never collide.
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
source-fetch update chezmoi       # force re-clone at the resolved (pinned) ref
source-fetch rm chezmoi           # drop one cached tree
source-fetch clean                # drop the cache for the current manifest
source-fetch manifest             # which manifest is in use
source-fetch version              # print the version
```

## Manifest

Flat TOML, one `[source.<name>]` table per dependency. See
[`docs/manifest.md`](docs/manifest.md) and [`manifest.example.toml`](manifest.example.toml).

It is discovered by walking upward from `$PWD` for `.source-fetch.toml`, or set
`$SOURCE_FETCH_MANIFEST` explicitly.

## Cache

`${SOURCE_FETCH_CACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/source-fetch}/<manifest-id>/<name>`.

- `<manifest-id>` is the basename of the git toplevel owning the manifest, else
  `path-<hash>`.
- Each tree is a plain git checkout with a `.source-fetch-ref` marker.
- `$SOURCE_FETCH_CACHE` overrides the root entirely (no namespace), for tests/CI.
- The cache is safe to delete at any time; `clean` refuses to run on
  empty/`/`/`$HOME`.

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
