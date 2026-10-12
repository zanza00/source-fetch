---
name: source-fetch
description: On-demand source code fetcher for AI agents. Use when the user wants to fetch, read, explore, search, or grep the source code of a dependency — an installed CLI, library, or tool — without cloning or vendoring it into the consuming repo. Triggers include "read the source of <dep>", "where is X in the source", "look at how <tool> implements it", "check the source of my installed version", "vendor nothing". Also use for creating or editing .source-fetch.toml manifests, pinning checkouts to installed versions, one-off peek fetches of arbitrary git URLs, and cleaning the source-fetch cache. Prefer source-fetch over a manual `git clone` when the goal is reading dependency source locally.
hidden: true
---

# source-fetch

Fetch the source code of a dependency on demand and cache it locally, so you
can read it without vendoring it into the consuming repo. When the manifest
configures `version_cmd`, it resolves a tag from the *installed* version.
The core skill explains ref resolution and fallback behavior.

Install: `go install github.com/zanza00/source-fetch/cmd/source-fetch@latest`
(or grab a binary from the GitHub Releases page). Requires `git` on `PATH`.

## Start here

This file is a discovery stub, not the usage guide. Before running any
`source-fetch` command, load the actual workflow content from the CLI:

```bash
source-fetch skills get core             # commands, manifest format, troubleshooting
```

The CLI serves skill content that always matches the installed version, so
usage instructions stay aligned with the binary. This stub is kept minimal
to reduce version drift; operational details live in `skills get core`.

Run `source-fetch skills list` to see everything available on the installed
version.

## Why source-fetch

- On demand only — nothing is fetched until asked.
- Version-aware — resolve a tag from the installed version when configured.
- Cached across runs — reuse dependency checkouts without vendoring them.
- One-off `peek` — fetch any git URL once, in a separate ad-hoc cache.
- Agent-friendly — plain directory on stdout, progress on stderr.
