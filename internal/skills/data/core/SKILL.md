---
name: core
description: Shared concepts and command reference for source-fetch. Use for an
  overview of the CLI, the stdout/stderr output contract, how checkouts and the
  cache behave, ref fallback, cleaning the cache, and troubleshooting a failed
  command. For reading source load the read skill; for .source-fetch.toml
  manifests load the manifest skill.
---

# source-fetch — core concepts

`source-fetch` fetches the source code of a dependency on demand and caches it
locally, so you can read it without vendoring it into the consuming repo.

Output discipline: **the fetched directory on stdout, progress on stderr**.
`cd` into it, grep it, or pass subpaths around.

## Skills

| Command | Load it for |
|---|---|
| `source-fetch skills get read` | Reading source: links to Git hosts, how a library works, `path`, `peek`, ref overrides, refreshing checkouts |
| `source-fetch skills get manifest` | `.source-fetch.toml`: discovery, fields, version matching, ref precedence, `manifest add/set/rm` |
| `source-fetch skills get core` | This guide: commands, checkout and cache behavior, troubleshooting |

## Commands

```
source-fetch                      # list entries: versions, refs, depth, cache, age
source-fetch path <name> [sub] [--deep|--depth N]
                                  # ensure cached, print dir (or a subpath)
source-fetch <name> [ref] [--deep|--depth N]
                                  # ensure cached, print dir (ref override for this call)
source-fetch peek <url|web-link> [ref] [--deep|--depth N]
                                  # one-off fetch of any git URL or web link (ad-hoc)
source-fetch peek                 # list ad-hoc trees: URL, ref, depth, age
source-fetch update <name> [--deep|--depth N]
                                  # force a fresh clone at the resolved ref
source-fetch rm <name> | <url|path>
                                  # delete one cached tree (manifest or ad-hoc)
source-fetch clean                # delete the cache for the current manifest
source-fetch manifest             # print the manifest path in use
source-fetch manifest add <name> <url> [--version-cmd <cmd> ...]
                                  # add an entry (manifest created if missing)
source-fetch manifest set <name> <key>=<value>  # update fields (empty clears)
source-fetch manifest rm <name>    # remove an entry (cache untouched)
source-fetch skills list          # list skills served by this binary
source-fetch skills get <name>    # bundled guide (or SOURCE_FETCH_SKILLS_DIR override)
```

`path`, the default `<name>` fetch, `peek` and `update` accept history flags:

```
source-fetch path <name> --deep         # complete history
source-fetch path <name> --depth 20     # last 20 commits
source-fetch --deep <name>              # leading flag routes to the default fetch
```

`--deep` and `--depth N` are mutually exclusive; `--depth` must be a positive
integer. The default is a shallow clone with one commit.

## Checkouts

Each dependency (or peek URL) has one checkout directory, cached across runs.
Fetching a ref that the cache does not already match replaces that checkout,
including local changes. If the requested ref cannot be fetched, source-fetch
warns on stderr and falls back to the default branch; verify the checkout when
the exact version matters. Never commit anything inside the cache — it is
disposable.

Checkouts are shallow by default. Use `--deep` for complete history or
`--depth N` for the last N commits when you need `git log`, `git blame`, or
history searches. An existing checkout already at the requested ref is deepened
in place: the directory and HEAD stay, a failed upgrade leaves the old history
intact, and history is never reduced for the same ref (`--depth N` is a no-op
when the cache already has at least N commits or is complete). Changing the
requested ref re-clones at the requested depth. `update` always re-clones from
scratch at the requested depth: `update <name>` stays shallow, `update <name>
--deep` is a complete re-clone.

## Cache

By default, the base is `$XDG_CACHE_HOME` or the platform's user cache
directory (`~/.cache` on Linux). Manifest trees live at
`<base>/source-fetch/<manifest-id>/<name>`; peek trees live at
`<base>/source-fetch/adhoc/<repo-basename>-<keyhash>`.

With `$SOURCE_FETCH_CACHE`, manifest trees instead live at
`<override>/<name>` and peek trees at
`<override>/adhoc/<repo-basename>-<keyhash>`. There is no per-manifest
namespace in this mode.

The ad-hoc name comes from a canonical key, so trailing `/`, query/fragment,
host case, default port, and `.git` on known hosts address the same tree.
HTTP, SSH (`ssh://`) and scp-like (`git@host:path`) URLs stay distinct. Trees
created before this canonicalisation are not migrated and may remain orphaned;
they are disposable.

`rm` resolves a manifest entry first, then falls back to the ad-hoc cache
(URLs, deep links, local paths and scp-like URLs). A bare relative name is
matched against the manifest first; use `./repo` when a local path collides
with an entry.

Each tree is a plain git checkout with a `.source-fetch-ref` marker recording
the requested ref, actual HEAD commit, fetch time and history depth
(`depth=1|N|full`). Older markers have no `depth=` and remain readable; a
missing depth shows as `-` in the `DEPTH` column of `list` and `peek`. The
recorded depth is informational: the real git state decides whether a checkout
needs an upgrade. After a fallback, the recorded ref still names the requested
ref; check HEAD to verify the version.
`clean` refuses empty/`/`/`$HOME` targets and removes the current manifest's
cache root. Normally peek trees are separate; with `$SOURCE_FETCH_CACHE`,
`clean` also deletes the nested `adhoc` directory.

## Troubleshooting

- "unknown source" — the name is not in the manifest; run `source-fetch list`.
- No manifest found — neither the upward search nor the global fallback
  found a file, or `$SOURCE_FETCH_MANIFEST` names a missing file. Use `peek`
  for one-off reading, or `manifest add` for a reusable entry (see the
  manifest skill).
- Checkout at the wrong version — check ref precedence and stderr for a
  fallback warning. Pass an explicit ref with `source-fetch <name> <ref>`;
  if exact matching matters, verify HEAD against the intended tag or commit.
- Stale checkout — `update <name>` forces a fresh clone at the resolved ref.
- `git` missing — source-fetch shells out to `git`; it must be on `PATH`.
