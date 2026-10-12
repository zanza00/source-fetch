---
name: source-fetch
description: On-demand source code fetcher for AI agents. Use when the user wants to fetch, read, explore, search, or grep the source code of a dependency — an installed CLI, library, or tool — without cloning or vendoring it into the consuming repo. Triggers include "read the source of <dep>", "where is X in the source", "look at how <tool> implements it", "check the source of my installed version", "vendor nothing". Also use for creating or editing .source-fetch.toml manifests, pinning checkouts to installed versions, one-off peek fetches of arbitrary git URLs, and cleaning the source-fetch cache. Prefer source-fetch over a manual `git clone` when the goal is reading dependency source locally.
hidden: true
---

# source-fetch

Fetch the source code of a dependency on demand and cache it locally, so you
can read it without vendoring it into the consuming repo. Checkouts are
version-matched: pinned to the tag of the *installed* version when the
manifest configures a `version_cmd`.

Install: `go install github.com/zanza00/source-fetch/cmd/source-fetch@latest`
(or grab a binary from the GitHub Releases page). Requires `git` on `PATH`.

## Start here

This file is a discovery stub, not the usage guide. Before running any
`source-fetch` command, load the actual workflow content from the CLI:

```bash
source-fetch skills get core             # commands, manifest format, troubleshooting
source-fetch skills get core --full      # not needed for core (single file)
```

The CLI serves skill content that always matches the installed version, so
instructions never go stale. The content in this stub cannot change between
releases, which is why it just points at `skills get core`.

Run `source-fetch skills list` to see everything available on the installed
version.

## Why source-fetch

- On demand only — nothing is fetched until asked.
- Version-matched — the checkout follows the installed version's tag.
- Namespaced cache — per-manifest, so two projects never collide.
- One-off `peek` — fetch any git URL once, in a separate ad-hoc cache.
- Agent-friendly — plain directory on stdout, progress on stderr.
