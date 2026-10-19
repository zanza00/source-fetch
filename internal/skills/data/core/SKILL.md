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
source-fetch                      # list entries: versions, refs, cache, age
source-fetch path <name> [sub]    # ensure cached, print dir (or a subpath)
source-fetch <name> [ref]         # ensure cached, print dir (ref override for this call)
source-fetch peek <url> [ref]     # one-off fetch of any git URL (ad-hoc)
source-fetch peek                 # list ad-hoc cached trees
source-fetch update <name>        # force a fresh clone at the resolved ref
source-fetch rm <name> | <url>    # delete one cached tree
source-fetch clean                # delete the cache for the current manifest
source-fetch cache                # print the cache root in use
source-fetch cache init [dir]     # point the manifest at a local cache dir
source-fetch cache ignore         # make sure git ignores the cache root
source-fetch --cache <dir> path <name>  # use another cache root for this call
source-fetch manifest             # print the manifest path in use
source-fetch manifest add <name> <url> [--version-cmd <cmd> ...]
                                  # add an entry (manifest created if missing)
source-fetch manifest set <name> <key>=<value>  # update fields (empty clears)
source-fetch manifest rm <name>    # remove an entry (cache untouched)
source-fetch skills list          # list skills served by this binary
source-fetch skills get <name>    # bundled guide (or SOURCE_FETCH_SKILLS_DIR override)
```

## Checkouts

Each dependency (or peek URL) has one checkout directory, cached across runs.
Fetching a ref that the cache does not already match replaces that checkout,
including local changes. If the requested ref cannot be fetched, source-fetch
warns on stderr and falls back to the default branch; verify the checkout when
the exact version matters. Never commit anything inside the cache — it is
disposable.

## Cache

Inside a git repository the cache is project-local — the repository of the
current directory, not the one that owns the manifest. Manifest trees live at
`<project>/.source-fetch/<name>` and peek trees at
`<project>/.source-fetch/adhoc/<url-derived-name>`. The directory is added to
`.git/info/exclude` on first use, so it never shows up in `git status`.

Outside a git repository the base is `$XDG_CACHE_HOME` or the platform's user
cache directory (`~/.cache` on Linux). Manifest trees live at
`<base>/source-fetch/<manifest-id>/<name>`; peek trees live at
`<base>/source-fetch/adhoc/<url-derived-name>`.

The root is picked in this order: `--cache <dir>` (this invocation only) →
`$SOURCE_FETCH_CACHE` → the manifest's `cache_dir` → `<project>/.source-fetch`
inside a git repository → `<base>/source-fetch/<manifest-id>` elsewhere.
Relative paths from the environment or the manifest are resolved against the
project root (the git toplevel that contains the current directory, else the
git toplevel that owns the manifest, else the manifest's directory).

Each tree is a plain git checkout with a `.source-fetch-ref` marker recording
the requested ref, actual HEAD commit and fetch time. After a fallback, the
recorded ref still names the requested ref; check HEAD to verify the version.
`clean` refuses empty/`/`/`$HOME` targets, the root of a git repository, and
a directory that holds the manifest or the current directory; it removes the
current manifest's cache root. Normally peek trees are separate; with
`$SOURCE_FETCH_CACHE` and with a project-local cache, `clean` also deletes the
nested `adhoc` directory.

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
