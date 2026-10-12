---
name: core
description: How to use source-fetch. Use when the task involves fetching, reading, exploring, or grepping the source code of a dependency (installed CLI, library, tool) on demand, working with .source-fetch.toml manifests, pinning checkouts to installed versions, using the ad-hoc peek cache, or cleaning/inspecting the source-fetch cache. Prefer source-fetch over git clone when the goal is reading dependency source without vendoring it into the consuming repo.
---

# source-fetch — core workflows

`source-fetch` fetches the source code of a dependency on demand and caches it
locally, so you can read it without vendoring it into the consuming repo.
Checkouts are version-matched (pinned to the tag of the *installed* version
when the manifest configures `version_cmd`), namespaced per manifest, and
cached across runs.

Output discipline: **the fetched directory on stdout, progress on stderr**.
`cd` into it, grep it, or pass subpaths around.

## Start here

Run `source-fetch list` (or bare `source-fetch`) in the project directory.
It prints every manifest entry with version, ref, cache path and age. If it
fails, no `.source-fetch.toml` manifest applies — see "Manifest" below.

## Commands

```
source-fetch                      # list entries: versions, refs, cache, age
source-fetch path <name> [sub]    # ensure cached, print dir (or a subpath)
source-fetch <name> [ref]         # ensure cached, print dir (one-shot ref)
source-fetch peek <url> [ref]     # one-off fetch of any git URL (ad-hoc)
source-fetch peek                 # list ad-hoc cached trees
source-fetch update <name>        # force a fresh clone at the resolved ref
source-fetch rm <name> | <url>    # delete one cached tree
source-fetch clean                # delete the cache for the current manifest
source-fetch manifest             # print the manifest path in use
source-fetch skills list          # list skills served by this binary
source-fetch skills get core      # this content (always version-matched)
```

## Typical agent loop

1. `source-fetch path <dep>` → prints the checkout directory.
2. Read files directly under that directory (grep, read, analyze).
3. Never commit anything inside the cache — it is disposable.

## Manifest

Flat TOML, one `[source.<name>]` table per dependency, discovered by walking
upward from `$PWD` for `.source-fetch.toml` (explicit via
`$SOURCE_FETCH_MANIFEST`). Fields:

```toml
[source.chezmoi]
url         = "https://github.com/twpayne/chezmoi.git"  # required
version_cmd = "chezmoi --version"   # optional: command whose output has the version
tag_prefix  = "v"                    # optional: string between version and tag
ref         = "v2.73.0"              # optional: persistent pin (wins over version_cmd)
default_ref = "stable"               # optional: fallback when version unknown
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

## Cache

`${SOURCE_FETCH_CACHE:-$XDG_CACHE_HOME|~/.cache}/source-fetch/<manifest-id>/<name>`;
ad-hoc peek trees live under `.../source-fetch/adhoc/`. Each tree is a plain
git checkout with a `.source-fetch-ref` marker. The cache is safe to delete
at any time; `clean` refuses empty/`/`/`$HOME` targets and leaves ad-hoc
trees alone. `$SOURCE_FETCH_CACHE` overrides the root (no namespace).

## Troubleshooting

- "unknown source" — the name is not in the manifest; run `source-fetch list`.
- No manifest found — walk up from `$PWD` finds nothing; create
  `.source-fetch.toml` or set `$SOURCE_FETCH_MANIFEST`.
- Checkout at the wrong version — the resolved ref is pinned by
  `version_cmd`/`ref`; check precedence above, or pass an explicit ref:
  `source-fetch <name> <ref>`.
- Stale checkout — `update <name>` forces a fresh clone at the resolved ref.
- `git` missing — source-fetch shells out to `git`; it must be on `PATH`.

## Adding an entry

Append a `[source.<name>]` table to `.source-fetch.toml`, then run
`source-fetch list` to confirm the resolved ref before fetching. Prefer
`version_cmd` over `ref` when the goal is "match what's installed here";
prefer `ref` for reproducible pins.
