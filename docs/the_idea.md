# source-fetch — origin and design rationale

> **Status:** historical note. This file records *why* `source-fetch` exists,
> not *how it works*. For current commands and behavior use the
> [README](../README.md), the [manifest reference](manifest.md), and
> `source-fetch skills get --all`.

## What the tool is

`source-fetch` fetches the **source code of a dependency on demand** and caches
it locally, so a human or an agent can read/inspect it without vendoring it
into the consuming repository.

The dependency can be anything that identifies an upstream git repository
today (a CLI tool, a library, a component library, an OS distro repo, …). The
design must not assume "CLI tool": in a React project you may want the source of
a component library, in a language-X project the source of a library.

Core behaviours:

1. **On demand only.** Nothing is fetched until asked. No git submodules, no
   sources tracked by git, nothing pulled by a consumer's normal update path.
2. **Version-matched.** When a `version_cmd` is configured, the checkout is
   pinned to the tag matching the *installed* version, falling back to a
   default branch/ref when unknown.
3. **Namespaced cache.** Sources live under a per-manifest cache namespace, so
   two projects with different manifests never collide.
4. **Agent-friendly contract.** The command prints a plain directory on stdout
   (quiet, scriptable); progress goes to stderr.

## Why it exists (origin)

`source-fetch` was extracted from a dotfiles repo, where it started as `ctx`, a
single-file bash helper. There it replaced ~250 MB of git submodules (tool
sources + the bazzite OS repos) that were previously vendored. Submodules made
every `chezmoi update` heavy and required re-pinning after every tool upgrade.
The on-demand cache keeps the repo light and removes the fragile submodule
checkout issues.

## Why the name is not `ctx`

`ctx` reads as "context", but the tool does **not** manage context — it fetches
*source* from an upstream, and it is not limited to CLI tools. `source-fetch` is
neutral (works for a tool, a library, a component library, an OS repo), reads as
an imperative command for an agent, and leaves the door open to non-git sources.

## Future scope (design for, do not build now)

The user's intent: "anything consultable and downloadable from git for now, and
something else in the future". Leave seams for:

- **Transport abstraction.** See [transports](transports.md) for the current
  seam (`transport = "git"` today) and the planned kinds.
- **Project-manifest auto-detection.** A per-project file could be derived from
  `package.json`, `Cargo.toml`, `go.mod`, `pyproject.toml` to know the
  dependencies, then fetch their upstream sources. Not in scope now; the
  manifest model should not make it impossible.
- **Registry-aware version mapping.** For npm/pypi/etc. the "installed version"
  may come from a lockfile rather than a CLI. A later `version_from` field could
  generalise the existing `version_cmd`/`ref` fields.
