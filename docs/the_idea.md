---
date: 2026-10-12T14:37
tags: []
---

# source-fetch — extraction spec & handoff

> **Audience:** an agent (or developer) tasked with extracting the current `ctx`
> helper out of the dotfiles repo into a standalone, general-purpose project named
> **`source-fetch`**, and adapting the dotfiles repo to consume it.
>
> **Status:** the implementation below is the *current, working* `ctx` helper
> (bash, single file). This document is a complete description of its behaviour,
> not a wish-list — treat it as the source of truth to port, then apply the
> rename map and the future-proofing notes.

---

## 1. What the tool is

`source-fetch` fetches the **source code of a dependency on demand** and caches it
locally, so a human or an agent can read/inspect it without vendoring it into the
consuming repository.

The dependency can be anything that identifies an upstream git repository today
(a CLI tool, a library, a component library, an OS distro repo, …). The design
must not assume "CLI tool": in a React project you may want the source of a
component library, in a language-X project the source of a library. Git is the
only supported transport **for now**; the model should leave room for other
transports later (see §12).

Core behaviours:

1. **On demand only.** Nothing is fetched until asked. No git submodules, no
   sources tracked by git, nothing pulled by `chezmoi update` (or any consumer's
   normal update path).
2. **Version-matched.** When a `version_cmd` is configured, the checkout is pinned
   to the tag matching the *installed* version (e.g. installed `topgrade 16.0.4`
   → tag `v16.0.4`), falling back to a default branch/ref when unknown.
3. **Namespaced cache.** Sources live under a per-manifest cache namespace, so two
   projects with different manifests never collide.
4. **Agent-friendly contract.** The command prints a plain directory on stdout
   (quiet, scriptable); progress goes to stderr.

### Why it exists (origin)

In the dotfiles repo `ctx` replaces ~250 MB of git submodules (tool sources +
the bazzite/aurora OS repos) that were previously vendored. Submodules made every
`chezmoi update` heavy and required re-pinning after every tool upgrade. The
on-demand cache keeps the repo light and removes the fragile submodule checkout
issues. `source-fetch` is the same mechanism, promoted to a general tool.

### Why the name changes

`ctx` reads as "context", but the tool does **not** manage context — it fetches
*source* from an upstream, and it is not limited to CLI tools. `source-fetch` is
neutral (works for a tool, a library, a component library, an OS repo), reads as
an imperative command for an agent, and leaves the door open to non-git sources.

---

## 2. Requirements / environment

- **Language:** bash (the current implementation is a single self-contained bash
  script using `set -euo pipefail`). A standalone project may keep bash or port
  it; the behaviour in this doc must be preserved either way.
- **Required:** `bash`, `git`, `awk`, `sed`, `date`, `cksum`, standard coreutils.
- **Optional:** [`yq`](https://github.com/mikefarah/yq) v4.25+ (mikefarah, TOML
  support) for manifest parsing. If missing, an internal `awk` fallback handles
  the same fields. No yq-specific syntax is required in manifests.
- **Platforms:** macOS (darwin) and Linux. Must not depend on GNU-only flags
  without a BSD fallback (the current script uses only portable flags).

---

## 3. CLI reference

Invocation: `source-fetch [command] [args]`. Default command is `list`.

| Command | Behaviour | stdout | stderr |
|---|---|---|---|
| `source-fetch` / `source-fetch list` | List every entry in the manifest with installed version, resolved ref, cache status and age (days). Footer prints the cache root and manifest path. | table | errors |
| `source-fetch path <name> [subpath]` | Ensure `<name>` is cached (fetch if needed), then print its directory, or `<dir>/<subpath>` if given. **Quiet on stdout** — ideal for `cd "$(source-fetch path X)"`. | dir path | progress |
| `source-fetch <name> [ref]` | Ensure `<name>` is cached and print its directory. Optional explicit `ref` (tag/branch/SHA) overrides resolution for this call only. | dir path | progress |
| `source-fetch update <name>` | **Force** a fresh shallow clone at the *resolved* ref (keeps manifest pins). | dir path | progress |
| `source-fetch rm <name>` | Delete one cached tree. Fails if not cached. | — | info/errors |
| `source-fetch clean` | Delete the whole cache **for the current manifest** only. Refuses to run on empty/`/`/`$HOME`. | — | info/errors |
| `source-fetch manifest` | Print the manifest path in use. | path | — |
| `source-fetch help` / `-h` / `--help` | Print usage. | — | usage |

Errors: `die()` prints `source-fetch: <message>` to stderr and exits `1`. Progress
`info()` prints `source-fetch: <message>` to stderr. Unknown `-*` option → error.

### stdout/stderr contract (critical for agents)

- `path` must keep stdout empty except for the single directory line, so it can be
  captured. All progress must go to stderr.
- The printed path always points at a real, plain git checkout on disk.

### Examples

```bash
source-fetch                       # list everything: versions, refs, cache
source-fetch path topgrade         # fetch if needed, print dir (quiet stdout)
source-fetch path aurora just/     # print a subpath inside the cached tree
cd "$(source-fetch path chezmoi)"  # read the source
source-fetch react v18.3.1         # one-shot explicit ref
source-fetch update chezmoi        # force re-clone at the resolved (pinned) ref
source-fetch rm chezmoi            # drop one cached tree
source-fetch clean                 # drop the cache for the current manifest
source-fetch manifest              # which manifest is in use
```

---

## 4. Manifest format

Flat TOML. One table per dependency (currently `[tool.<name>]`; see the rename
note in §10 — for a general tool prefer `[source.<name>]`).

```toml
[tool.chezmoi]
repo           = "https://github.com/twpayne/chezmoi.git"
version_cmd    = "chezmoi --version"   # optional; "" or omitted = no version detection
tag_prefix     = "v"                    # optional; prefix between version and tag ("" or "v")
ref            = "v2.73.0"              # optional persistent pin: tag/branch/SHA; wins over version_cmd
default_ref    = "stable"               # optional fallback when version is unknown/unparseable
```

| Field | Required | Meaning |
|---|---|---|
| `repo` | yes | git remote URL to clone |
| `version_cmd` | no | shell command whose output contains the installed version. `""`/omitted = none. |
| `tag_prefix` | no | string between the version and the release tag (`""` or `"v"`) |
| `ref` | no | **persistent pin** (tag, branch or commit SHA). Wins over `version_cmd`. |
| `default_ref` | no | fallback ref when the version is empty/unparseable (e.g. `stable`) |

Manifests without `ref`/`default_ref` keep the plain "version tag or default
branch" behaviour.

### Parsing

Prefer `yq -p=toml`; if unavailable, a strict `awk -F'"'` parser supports the same
fields. Internally rows are emitted as `name<US>repo<US>version_cmd<US>tag_prefix <US>ref<US>default_ref` where `US` = ASCII `0x1f` (unit separator). Rationale:
a non-whitespace `IFS` preserves empty fields (tab collapses runs of IFS
whitespace under `read`). The `awk` fallback assumes simple `"..."` values with no
escaped quotes.

### Manifest resolution (first match wins)

1. `$SOURCE_FETCH_MANIFEST` (explicit override).
2. **Auto-discovery** from `$PWD` upwards: at each directory level, the first of
   `.source-fetch.toml`, `tools.toml`, `docs/vendor/tools.toml`,
   `docs/context/tools.toml` *(see §10 for the candidate-name decision)*.
3. **Consumer default** — **this is a dotfiles-specific hack**: the current script
   bakes in `DEFAULT_MANIFEST="{{ .chezmoi.sourceDir }}/docs/context/tools.toml"`
   at chezmoi render time. A standalone `source-fetch` must NOT bake a path; make
   the default optional/configurable (e.g. `~/.config/source-fetch/manifest.toml`)
   or drop it entirely. The consuming repo supplies its own manifest via discovery
   or `$SOURCE_FETCH_MANIFEST`.

Missing manifest at `list`/`path`/`update`/`<name>` time → error
`manifest not found: <path> (set SOURCE_FETCH_MANIFEST)`.

---

## 5. Ref resolution precedence

For a given entry: **CLI ref** (`source-fetch <name> <ref>`) → manifest **`ref`** →
**version tag** (`tag_prefix` + first `X.Y.Z` printed by `version_cmd`) → manifest
**`default_ref`** → **default branch** (empty ref).

Version detection (`installed_version`):
- If `version_cmd` is empty → no version.
- If the first word of `version_cmd` is not found via `command -v` → no version
  (the command is skipped entirely).
- Otherwise run `version_cmd` (via `eval`), grep the first occurrence of
  `[0-9]+\.[0-9]+\.[0-9]+`, take the first match.

---

## 6. Cache layout

Default root: `${XDG_CACHE_HOME:-$HOME/.cache}/source-fetch/<manifest-id>/`.

- `<name>` = the manifest table key.
- `<manifest-id>` = stable id of the repo owning the manifest:
  - `basename` of `git -C <manifest-dir> rev-parse --show-toplevel` when inside a
    git repo (e.g. `chezmoi`, `homelab`);
  - else `path-<cksum>` (cksum of the absolute manifest path) outside a repo.
- `$SOURCE_FETCH_CACHE` overrides the cache root **entirely** (entries then live
  directly under it, no namespace) — used for tests/CI.

Each cached tree is a **plain git checkout** containing a small marker file
`.<tool>-ref` — rename to e.g. `.source-fetch-ref` — with:

```
ref=<requested ref>
commit=<resolved HEAD SHA>
fetched=<epoch seconds>
```

Safety:
- The cache is safe to delete at any time.
- `clean` refuses to run when the cache root is empty, `/`, or `$HOME`.
- **Migration, never lose a populated cache silently:**
  - pre-namespaced `~/.cache/chezmoi-context` → migrated to the new root once when
    the manifest-id is `chezmoi` and the new root does not yet exist;
  - a plain `~/.cache/ctx/<id>` → also migrate into `~/.cache/source-fetch/<id>`
    during the transition (keep this migration for at least one release).

---

## 7. Fetch algorithm (exact behaviour to port)

`fetch name repo ref [force]`:

1. If `<cache_root>/<name>/.git` exists, not forced, and already at `ref`
   (`at_ref`) → print dir and return. **No network.**
2. If a stale tree exists → `rm -rf` it (message `refreshing <name>`).
3. `mkdir -p <dir>`, then:
   - **empty ref** → `git clone -q --depth 1 <repo> <dir>` (default branch).
   - **ref given** → try `git clone -q --depth 1 --branch <ref> <repo> <dir>`
     (tag or branch).
   - **clone --branch failed** → treat `<ref>` as a commit SHA: fresh `git init`,
     `git remote add origin <repo>`, `git fetch -q --depth 1 origin <ref>`,
     `git checkout -q --detach FETCH_HEAD`.
   - **SHA fetch also failed** → discard, clone default branch, warn
     `ref '<ref>' not found, using default branch`.
4. Write the `.<tool>-ref` marker.
5. Print the directory.

`at_ref dir ref`:
- empty ref → returns success (so default-branch checkouts are **never**
  re-refreshed automatically — only `update` forces them);
- no `.git` → not at ref;
- if the marker's `ref=` equals the requested ref → **success without touching the
  network** (fast path, even if the remote moved);
- else compare `git rev-parse <ref>^{commit}` (if resolvable) with `HEAD`; equal →
  at ref.

Consequences worth preserving: repeated `path`/`<name>` calls are offline and fast;
a pin change triggers a re-clone; `update` always re-clones.

---

## 8. Current consumers & integration points (dotfiles)

These must be adapted to the new name (they are the extraction's tail work):

- **Script source:** `dot_local/bin/executable_ctx.tmpl` (chezmoi template — the
  only templating is `DEFAULT_MANIFEST = "{{ .chezmoi.sourceDir }}/..."`, which
  the standalone project must not keep). Installs to `~/.local/bin/ctx`.
- **Manifest:** `docs/context/tools.toml` (see §9).
- **Docs:** `docs/context/README.md` (full description), `README.md` (usage +
  tree), `AGENTS.md` (agent-facing summary).
- **Agent skill:** `.agents/skills/tool-source/SKILL.md` (`name: tool-source`,
  describes when/how to use `ctx`). The skill id and the command name should stay
  consistent after the rename; consider renaming the skill if the command becomes
  `source-fetch`.
- **Ignore rules:** `docs` is `.chezmoiignore`d (repo-only), so the manifest and
  docs never land in `$HOME`.
- **Troubleshooting:** `docs/troubleshoot/troubleshooting-context-submodule-gitlinks.md`
  (the old-submodule cleanup procedure) references the tool.
- Nothing in `dot_config/topgrade.toml.tmpl` references `ctx`.

### Full rename map (suggested)

| Current | New |
|---|---|
| binary `ctx` | `source-fetch` |
| `$CTX_MANIFEST` | `$SOURCE_FETCH_MANIFEST` |
| `$CTX_CACHE` | `$SOURCE_FETCH_CACHE` |
| cache `~/.cache/ctx/<id>/` | `~/.cache/source-fetch/<id>/` |
| legacy `~/.cache/chezmoi-context` | keep the one-time migration |
| marker `.ctx-ref` | `.source-fetch-ref` |
| discovery candidate `.ctx.toml` | `.source-fetch.toml` (keep `.ctx.toml` and `tools.toml` as accepted aliases) |
| manifest table `[tool.<name>]` | `[source.<name>]` (accept `[tool.*]` for back-compat) |
| message prefix `ctx:` | `source-fetch:` |
| `docs/context/` dir | consider `docs/sources/` (docs-only; keep or rename at will) |
| skill `tool-source` | keep, or rename to match the command |

Back-compat options to consider: ship a `ctx` symlink/shim → `source-fetch` for one
release; accept both env-var names (prefer new, fall back to old) and both marker
filenames during migration.

---

## 9. Current manifest contents (the dotfiles entry set)

These are the entries the dotfiles manifest ships today (`docs/context/tools.toml`).
They are examples of both "version-matched tool" and "no-version, default-branch"
kinds:

| name | repo | version_cmd | tag_prefix |
|---|---|---|---|
| chezmoi | https://github.com/twpayne/chezmoi.git | `chezmoi --version` | `v` |
| topgrade | https://github.com/topgrade-rs/topgrade.git | `topgrade --version` | `v` |
| television | https://github.com/alexpasmantier/television.git | `television --version` | `""` |
| navi | https://github.com/denisidoro/navi.git | `navi --version` | `v` |
| atuin | https://github.com/atuinsh/atuin.git | `atuin --version` | `v` |
| pay-respects | https://github.com/iffse/pay-respects.git | `pay-respects --version` | `v` |
| espanso | https://github.com/espanso/espanso.git | `espanso --version` | `v` |
| forgejo-cli | https://codeberg.org/forgejo-contrib/forgejo-cli | `fj version` | `v` |
| pik | https://github.com/jacek-kurlit/pik.git | `pik --version` | `""` |
| superfile | https://github.com/yorukot/superfile.git | `spf --version` | `v` |
| mistral-vibe | https://github.com/mistralai/mistral-vibe.git | `vibe --version` | `v` |
| rtk | https://github.com/rtk-ai/rtk.git | `rtk --version` | `v` |
| hammerspoon | https://github.com/hammerspoon/hammerspoon.git | `""` (macOS) | `""` |
| bazzite | https://github.com/ublue-os/bazzite.git | `""` (OS repo) | `""` |
| aurora | https://github.com/ublue-os/aurora.git | `""` (OS repo) | `""` |

Non-git or non-CLI entries (`hammerspoon`, `bazzite`, `aurora`) rely on the default
branch — the pattern to keep in mind when adding "library/component-library" entries
for other repos.

---

## 10. Naming decisions to finalise in the standalone project

Decisions the extraction must pin down (recommended values in bold):

1. Binary name: **`source-fetch`**.
2. Env vars: **`SOURCE_FETCH_MANIFEST`**, **`SOURCE_FETCH_CACHE`** (accept legacy
   `CTX_*` as fallback during migration).
3. Cache root: **`~/.cache/source-fetch/`**.
4. Discovery candidate filenames — keep `tools.toml` working? The tool is no longer
   "tools"-only, so the canonical name should be **`.source-fetch.toml`**, with
   `tools.toml` (and `docs/vendor/tools.toml`, `docs/context/tools.toml`) accepted
   for back-compat.
5. Manifest table prefix: **`[source.<name>]`** canonical, `[tool.*]` accepted for
   back-compat.
6. Marker file: **`.source-fetch-ref`**.
7. Default manifest: **none baked in**; optional user-level
   `~/.config/source-fetch/manifest.toml`, plus discovery + env override.
8. Skill/command alignment: keep the agent skill id consistent with the command.

---

## 11. Suggested standalone repo layout

```
source-fetch/
├── README.md                 # what/why, install, usage, manifest reference, cache
├── LICENSE                   # pick one (dotfiles repo has none yet)
├── source-fetch              # the executable (bash), or bin/source-fetch
├── manifest.example.toml     # annotated example manifest
├── docs/
│   ├── manifest.md           # fields, resolution, discovery
│   └── transports.md         # git now; placeholders for future transports
└── test/
    ├── run.sh                # bats-less smoke tests using a local bare repo
    └── fixtures/             # tiny local git repos (no network in tests)
```

Keep it **single-file, dependency-light** (bash + git + awk). This is a feature:
it drops onto any machine with no toolchain, which is exactly what an agent-facing
fetcher wants.

---

## 12. Future scope (design for, do not build now)

The user's intent: "anything consultable and downloadable from git for now, and
something else in the future". Leave seams for:

- **Transport abstraction.** Today `repo` is a git URL and the fetch is
  clone/checkout. Anticipate a `kind`/`transport` field (`git`, later `tarball`,
  `http`, `registry`, …) and an `url` field that subsumes `repo`. Default
  `transport = "git"` keeps existing manifests valid.
- **Project-manifest auto-detection.** Instead of (or in addition to) a hand-written
  manifest, a per-project file could be derived from `package.json`,
  `Cargo.toml`, `go.mod`, `pyproject.toml` to know the dependencies, then fetch
  their upstream sources. Not in scope now; the manifest model should not make it
  impossible.
- **Registry-aware version mapping.** For npm/pypi/etc. the "installed version" may
  come from a lockfile rather than a CLI. The `version_cmd`/`ref` fields already
  cover the simple case; a later `version_from` field could generalise it.

---

## 13. Acceptance criteria (port fidelity)

A correct `source-fetch` must:

1. `source-fetch list` print every manifest entry with installed version (or `-`),
   resolved ref, cache dir (or `(not cached)`), age in days, then cache root and
   manifest path.
2. `source-fetch path <name>` print **only** the directory on stdout (empty stderr
   when already cached), and fetch it first if missing.
3. Resolve refs by the exact precedence in §5, including "not installed → skip
   `version_cmd`" and "first `X.Y.Z` only".
4. Never re-fetch an already-cached tree when `at_ref` passes (offline fast path),
   and never re-refresh an empty-ref (default-branch) tree except via `update`.
5. Handle tag/branch clone, commit-SHA fetch, and default-branch fallback in that
   order, warning on fallback.
6. Namespace the cache by manifest-id (git toplevel basename, else `path-<cksum>`)
   and honour `$SOURCE_FETCH_CACHE` (no namespace).
7. Refuse `clean` on empty/`/`/`$HOME`; migrate legacy caches once without loss.
8. Parse manifests with `yq` when present and the `awk` fallback otherwise, for the
   same five fields.
9. Discover manifests by walking `$PWD` upwards, honouring the env override and the
   candidate-filename order.

---

## 14. Open questions

1. Should `source-fetch` ship a built-in fallback manifest, or always require one
   (via discovery/env/config)? *(Current tool bakes a dotfiles path — must go.)*
2. Rename the discovery/manifest table to `[source.*]` now (breaking) or accept
   `[tool.*]` forever?
3. Distribution: single bash script via curl/Homebrew tap vs. a compiled binary?
   *(Recommend the script.)*
4. Keep a `ctx` shim/alias during the dotfiles transition, or cut over cleanly?
5. Rename the `tool-source` skill to mirror the new command?
