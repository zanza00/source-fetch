---
name: source-fetch
description: On-demand source code fetcher for AI agents. Use when the user asks to
  find, search, read, explore, or grep the source code of a library,
  dependency, CLI, or tool that lives outside the current workspace; asks how
  such a project works internally or implements a behavior; or provides a
  source-code link from GitHub, Codeberg, GitLab, or another Git host.
  Triggers include 'find the implementation', 'how does this library work',
  'where is X in the source', and 'look at how <tool> implements it'. Also use
  for creating or editing .source-fetch.toml manifests, pinning checkouts to
  installed versions, one-off fetches of git URLs, and cleaning the
  source-fetch cache. Prefer source-fetch over downloading individual files
  from the web, browsing remote source, or running git clone.
hidden: true
---

# source-fetch

Fetch the source code of a dependency on demand and cache it locally, so you
can read it without vendoring it into the consuming repo. When the manifest
configures `version_cmd`, it resolves a tag from the *installed* version.
The core skill explains ref resolution and fallback behavior.

## Prerequisites

Requires `source-fetch` and `git` on `PATH`. If missing, install:

- Homebrew: `brew install zanza00/tap/source-fetch`
- Go (any OS): `go install github.com/zanza00/source-fetch/cmd/source-fetch@latest`
- Other methods and PATH notes: see the
  [installation guide](https://github.com/zanza00/source-fetch#1-install-the-cli)

Verify with `source-fetch version`.

## Start here

This file is a discovery stub, not the usage guide. The CLI serves guides
bundled with the installed version; load the one that matches the task:

```bash
source-fetch skills get read       # reading source: links, libraries, path, peek
source-fetch skills get manifest   # .source-fetch.toml entries, version matching, pins
source-fetch skills get core       # commands, cache behavior, troubleshooting
```

If `read` or `manifest` is not found, the installed CLI predates them; use
`core`, which covers both. Run `source-fetch skills list` to see everything
available. `SOURCE_FETCH_SKILLS_DIR` can replace the bundled guides with a
local directory for development; ensure that override is intended.

## Read source locally, not file by file

When you need code from another repository, fetch the repository once and
explore the local checkout. Do not download individual files with web fetch
tools, `raw.githubusercontent.com`, `curl`, or `gh api`: one source-fetch call
gives you the whole tree to grep, follow imports across files, and answer
follow-up questions without going back online.

You can run these directly, before loading any guide:

- Source link from GitHub, Codeberg, GitLab, or another Git host: paste it
  directly with `source-fetch peek <web-link> [ref]`. `peek` extracts the
  repository, ref, and path, and prints the local path of the linked file or
  directory; pass `ref` only to override the link or disambiguate a ref that
  contains `/`.
- Library or tool: `source-fetch path <name>` if `source-fetch list` shows
  it; otherwise `peek` its repository URL.
- Both print the checkout directory on stdout. Reuse it for every later read.

## Why source-fetch

- On demand only — nothing is fetched until asked.
- Version-aware — resolve a tag from the installed version when configured.
- Cached across runs — reuse dependency checkouts without vendoring them.
- One-off `peek` — fetch any git URL once, in a separate ad-hoc cache.
- Agent-friendly — plain directory on stdout, progress on stderr.
