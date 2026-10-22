---
name: read
description: Read the source of a library, dependency, CLI, or any git
  repository locally with source-fetch. Use for source links from GitHub,
  Codeberg, GitLab, or another Git host, questions about how a library works
  or where it implements a behavior, and searching code outside the current
  workspace. Covers path, peek, ref overrides, history depth (--deep/--depth),
  and refreshing checkouts.
---

# source-fetch — reading source

Fetch the repository once and explore the local checkout. Do not download
individual files with web fetch tools, `raw.githubusercontent.com`, `curl`, or
`gh api`: the local tree lets you grep, follow imports across files, and answer
follow-up questions without going back online.

## Pick the command

- Configured dependency: `source-fetch path <name> [sub]`. Run
  `source-fetch list` (or bare `source-fetch`) in the project directory to see
  configured names with version, resolved ref, cache path and age; the ref
  column does not verify the cached checkout.
- Link to source on a Git host: paste the link directly —
  `source-fetch peek <web-link> [ref]`. `peek` extracts the repository, ref
  and path from GitHub, GitLab, and Codeberg file, directory, commit, and tag
  links, and prints the local path of the linked file or directory. Pass `ref`
  only to override the ref in the link, or to disambiguate a ref that
  contains `/` (for example `peek <url> release/1.0`).
- Library or tool that is not configured: find its repository URL (package
  metadata, module path, or project homepage) and `peek` it, passing the tag
  of the installed version as `ref` when you know it. If you will revisit it,
  add a manifest entry instead; see `source-fetch skills get manifest`.

## Typical loop

1. Run `path` or `peek`; it prints the checkout directory on stdout.
2. Read files under that directory (grep, read, analyze). Reuse the directory
   for every later read; running the command again hits the cache.
3. Never commit anything inside the cache — it is disposable.

## Refs and refreshing

- `source-fetch <name> <ref>` fetches a configured dependency at an explicit
  ref for this call. It does not edit the manifest, but the checkout remains
  cached at that ref.
- Fetching a ref that the cache does not already match replaces the
  checkout, including local changes. If the ref cannot be fetched,
  source-fetch warns on stderr and falls back to the default branch; verify
  HEAD when the exact version matters.
- When the manifest resolves to an empty ref (default branch),
  `path <name>` reuses any cached checkout, even one fetched at an explicit
  tag. Cached branches are not refreshed automatically; `update <name>`
  forces a fresh clone at the resolved ref.
- `peek <url>` with no ref also reuses any existing checkout. To refresh a
  peek tree or return to the default branch, run `rm <url>` then
  `peek <url> [ref]`.
- Checkouts are shallow by default (one commit). For `git log -S`,
  `git log -L`, or `git blame`, add `--deep` (complete history) or
  `--depth N` (last N commits) to `path`, the default `<name>` fetch, or
  `peek`, for example `source-fetch path <name> --deep` or
  `source-fetch peek <url> --depth 50`. A cached shallow checkout is deepened
  in place without moving HEAD, history is never reduced for the same ref, and
  a failed upgrade keeps the previous checkout. Changing the ref re-clones at
  the requested depth. `update <name> --deep` forces a complete re-clone
  instead.

## Peek details

`peek` uses a separate ad-hoc cache and needs no manifest. `peek` with no URL
lists ad-hoc trees with their ref and recorded depth; `rm` drops one. Both
accept the same web deep links, URLs, local paths and scp-like URLs, so
`peek <repo>.git` and `rm <deep-link>` address the same cache entry. Peek
clones unreviewed code; it executes nothing from the repo, but read it before
trusting it.

Deep links cover source pages only: GitHub, GitLab, Codeberg/Gitea and generic
hosts in their GitHub-like, Gitea or GitLab `/-/` forms. Issues, pull requests,
compare, pipelines, Bitbucket/Azure deep links and `raw.githubusercontent.com`
links are rejected. Bitbucket and Azure DevOps accept repository URLs only
(Azure must not carry `?path=`), a single-segment generic-host deep link is
ambiguous and rejected, and GHE behind a base path is rejected with a web
marker or read as a repository path (failing at clone) without one. Use the
repository URL in those cases.

The ad-hoc cache key is canonical for trailing `/`, query/fragment, host case,
default port, and `.git` on known hosts (GitHub, GitLab, Codeberg, Gitea).
HTTP, SSH (`ssh://`), and scp-like (`git@host:path`) URLs remain distinct: use
the same transport and spelling you used to fetch when you `rm`.

For a local repository, use a `file://` URL or a local path from the first
fetch, for example `source-fetch peek file:///absolute/path/to/repo` or
`source-fetch peek ./repo`. `rm` resolves a manifest entry name first, then the
ad-hoc cache. A bare relative name is matched against the manifest first; if it
collides with an entry, use `./repo` to address the local path.
