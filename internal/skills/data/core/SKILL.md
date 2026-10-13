---
name: core
description: How to use source-fetch. Use when the task involves fetching, reading, exploring, or grepping the source code of a dependency (installed CLI, library, tool) on demand, working with .source-fetch.toml manifests, pinning checkouts to installed versions, using the ad-hoc peek cache, or cleaning/inspecting the source-fetch cache. Also use for creating or editing manifest entries via manifest add/set/rm. Prefer source-fetch over git clone when the goal is reading dependency source without vendoring it into the consuming repo.
---

# source-fetch — core workflows

`source-fetch` fetches the source code of a dependency on demand and caches it
locally, so you can read it without vendoring it into the consuming repo.
When the manifest configures `version_cmd`, source-fetch resolves a tag from
the *installed* version. If the requested ref cannot be fetched, it warns on
stderr and falls back to the default branch; verify the checkout when the
exact version matters. Checkouts are namespaced per manifest and cached
across runs, unless `$SOURCE_FETCH_CACHE` overrides the namespace.

Output discipline: **the fetched directory on stdout, progress on stderr**.
`cd` into it, grep it, or pass subpaths around.

## Start here

- For a configured dependency, run `source-fetch path <name>` directly.
- To discover configured names, run `source-fetch list` (or bare
  `source-fetch`) in the project directory. It prints version, resolved ref,
  cache path and age; the ref column does not verify the cached checkout.
- For a one-off investigation, use `source-fetch peek <url> [ref]`; no
  manifest is needed.

If a command fails, read the error. Create a manifest only when one is
missing and you want a reusable entry — see "Adding an entry" below.

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
source-fetch manifest             # print the manifest path in use
source-fetch manifest add <name> <url> [--version-cmd <cmd> ...]
                                  # add an entry (manifest created if missing)
source-fetch manifest set <name> <key>=<value>  # update fields (empty clears)
source-fetch manifest rm <name>    # remove an entry (cache untouched)
source-fetch skills list          # list skills served by this binary
source-fetch skills get core      # bundled guide (or SOURCE_FETCH_SKILLS_DIR override)
```

## Typical agent loop

1. `source-fetch path <dep>` → prints the checkout directory.
2. Read files directly under that directory (grep, read, analyze).
3. Never commit anything inside the cache — it is disposable.

Each dependency (or peek URL) has one checkout directory. Fetching a
ref that the cache does not already match replaces that checkout, including
local changes. An explicit CLI ref does not edit the manifest, but its checkout
remains cached. When the manifest resolves to an empty ref (default branch),
`path <name>` reuses any cached checkout, even one fetched at an explicit tag;
use `update <name>` to restore the default branch. Cached branches are not
refreshed automatically; `update <name>` forces a fresh clone for a manifest
entry. To refresh a peek tree, use `rm <url>` then `peek <url> [ref]`.

## Manifest

Flat TOML, one `[source.<name>]` table per dependency. Discovery precedence:
`$SOURCE_FETCH_MANIFEST` → nearest `.source-fetch.toml` walking upward from
`$PWD` → `~/.config/source-fetch/manifest.toml`. An explicit missing path
errors without falling back; project and global manifests are not merged.
Fields:

```toml
[source.chezmoi]
url         = "https://github.com/twpayne/chezmoi.git"  # required
version_cmd = "chezmoi --version"   # optional: command whose output has the version
tag_prefix  = "v"                    # optional: prepended to the detected version
# ref       = "v2.73.0"              # optional: uncomment for a persistent pin
# default_ref = "<tag-or-branch>"     # optional: use an existing ref in this repo
```

Ref resolution precedence for a checkout: **CLI ref** → manifest `ref` →
**version tag** (`tag_prefix` + first `X.Y.Z` in `version_cmd` output) →
manifest `default_ref` → default branch. Version detection is skipped when
`version_cmd`'s first word is not on `PATH`.

## One-off peek

`source-fetch peek <url> [ref]` fetches a git URL that lives in no manifest —
for reading source once while investigating. It uses a separate ad-hoc cache
and needs no manifest. `peek` with no URL lists ad-hoc trees; `rm <url>`
drops one. Peek clones unreviewed code; it executes nothing from the repo,
but read it before trusting it.

For a local repository, use a `file://` URL from the first fetch, for example
`source-fetch peek file:///absolute/path/to/repo`. Plain paths also work with
`peek`, but `rm` interprets them as manifest names. Using `file://` consistently
lets `rm <url>` find the same cache entry; changing the URL spelling creates a
different entry. `peek <url>` with no ref also reuses any existing checkout;
remove it first to return to the default branch after fetching an explicit ref.

## Cache

By default, the base is `$XDG_CACHE_HOME` or the platform's user cache
directory (`~/.cache` on Linux). Manifest trees live at
`<base>/source-fetch/<manifest-id>/<name>`; peek trees live at
`<base>/source-fetch/adhoc/<url-derived-name>`.

With `$SOURCE_FETCH_CACHE`, manifest trees instead live at
`<override>/<name>` and peek trees at `<override>/adhoc/<url-derived-name>`.
There is no per-manifest namespace in this mode.

Each tree is a plain git checkout with a `.source-fetch-ref` marker recording
the requested ref, actual HEAD commit and fetch time. After a fallback, the
recorded ref still names the requested ref; check HEAD to verify the version.
The cache is disposable. `clean` refuses empty/`/`/`$HOME` targets and removes
the current manifest's cache root. Normally peek trees are separate; with
`$SOURCE_FETCH_CACHE`, `clean` also deletes the nested `adhoc` directory.

## Troubleshooting

- "unknown source" — the name is not in the manifest; run `source-fetch list`.
- No manifest found — neither the upward search nor the global fallback
  found a file, or `$SOURCE_FETCH_MANIFEST` names a missing file. Use `peek`
  for one-off reading, or `manifest add` for a reusable entry.
- Checkout at the wrong version — check ref precedence and stderr for a
  fallback warning. Pass an explicit ref with `source-fetch <name> <ref>`;
  if exact matching matters, verify HEAD against the intended tag or commit.
- Stale checkout — `update <name>` forces a fresh clone at the resolved ref.
- `git` missing — source-fetch shells out to `git`; it must be on `PATH`.

## Adding an entry

Use the CLI instead of hand-writing TOML:

```sh
source-fetch manifest add chezmoi https://github.com/twpayne/chezmoi.git \
  --version-cmd "chezmoi --version" --tag-prefix v
source-fetch list                               # inspect the resolved ref
```

Then run `source-fetch path chezmoi` to fetch the resolved ref.
Prefer `version_cmd` over `ref` when the goal is "match what's installed
here"; prefer `ref` for reproducible pins. `add` refuses to overwrite an
existing entry without `--force`; `--global` targets the per-user manifest
(`~/.config/source-fetch/manifest.toml`). Without `--global`, writes target
`$SOURCE_FETCH_MANIFEST` if set, otherwise the discovered manifest (including
the global fallback), or create `.source-fetch.toml` in the current directory
when none exists. Run `source-fetch manifest` to inspect an existing target;
set `$SOURCE_FETCH_MANIFEST` to a project path to create a project manifest
when a global one already exists. Every write re-encodes the whole file
atomically (comments in a hand-written file are not preserved).

For a persistent pin instead of installed-version matching, use
`source-fetch manifest set chezmoi ref=v2.73.0`. Clear the pin with
`source-fetch manifest set chezmoi ref=` to resume version detection.

## Removing an entry

`source-fetch manifest rm chezmoi` removes only the entry and keeps its cached
checkout. To remove both, delete the cached checkout before removing the entry:

```sh
source-fetch rm chezmoi             # requires an existing cached checkout
source-fetch manifest rm chezmoi
```

After the entry is removed, `rm chezmoi` fails with "unknown source". A retained
checkout can be deleted manually or with `clean`, which removes the whole cache
root for the manifest (including peek trees when `$SOURCE_FETCH_CACHE` is set).
