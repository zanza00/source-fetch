---
name: manifest
description: Configure .source-fetch.toml manifests for source-fetch. Use to
  add, update, pin, or remove a dependency entry, match a checkout to the
  installed version with version_cmd and tag_prefix, or understand manifest
  discovery and ref precedence.
---

# source-fetch — manifests

A manifest makes a dependency reusable: name it once, then
`source-fetch path <name>` fetches it at the resolved ref. For a one-off read,
`peek` needs no manifest (see `source-fetch skills get read`).

## Discovery

Discovery precedence: `$SOURCE_FETCH_MANIFEST` → nearest `.source-fetch.toml`
walking upward from `$PWD` → `~/.config/source-fetch/manifest.toml`. An
explicit missing path errors without falling back; project and global
manifests are not merged. Checkouts are namespaced per manifest unless the
cache root is overridden (`--cache`, `$SOURCE_FETCH_CACHE`, or the
manifest's `cache_dir`) or the current directory is in a git repository:
there the cache is project-local, in `<project>/.source-fetch`.

## Fields

Flat TOML, one `[source.<name>]` table per dependency:

```toml
cache_dir = ".source-fetch"   # optional top-level key: where the checkouts live

[source.chezmoi]
url         = "https://github.com/twpayne/chezmoi.git"  # required
version_cmd = "chezmoi --version"   # optional: command whose output has the version
tag_prefix  = "v"                    # optional: prepended to the detected version
# ref       = "v2.73.0"              # optional: uncomment for a persistent pin
# default_ref = "<tag-or-branch>"     # optional: use an existing ref in this repo
```

`cache_dir` is a top-level key, not a per-source field: it says where the
checkouts of every entry live, relative to the project root or absolute. See
`source-fetch skills get core` for the full cache layout.

## Ref precedence

Ref resolution precedence for a checkout: **CLI ref** → manifest `ref` →
**version tag** (`tag_prefix` + first `X.Y.Z` in `version_cmd` output) →
manifest `default_ref` → default branch. Version detection is skipped when
`version_cmd`'s first word is not on `PATH`. If the resolved ref cannot be
fetched, source-fetch warns on stderr and falls back to the default branch.

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

## Pinning

For a persistent pin instead of installed-version matching, use
`source-fetch manifest set chezmoi ref=v2.73.0`. Clear the pin with
`source-fetch manifest set chezmoi ref=` to resume version detection.
Changing the resolved ref replaces the cached checkout on the next `path`;
after clearing a pin back to the default branch, run `update <name>`, because
`path` reuses whatever checkout is cached.

## Removing an entry

`source-fetch manifest rm chezmoi` removes only the entry and keeps its cached
checkout. To remove both, delete the cached checkout before removing the entry:

```sh
source-fetch rm chezmoi             # requires an existing cached checkout
source-fetch manifest rm chezmoi
```

After the entry is removed, `rm chezmoi` fails with "unknown source". A retained
checkout can be deleted manually or with `clean`, which removes the whole cache
root for the manifest (including peek trees when the cache is project-local or
`$SOURCE_FETCH_CACHE` is set).
