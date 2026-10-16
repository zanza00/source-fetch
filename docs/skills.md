# Skills

source-fetch ships agent skills in two tiers, mirroring the design of
[agent-browser](https://github.com/vercel-labs/agent-browser). The split
solves the version-drift problem: the skill installed in an agent is a thin
stub that almost never changes, while the CLI serves usage content bundled
with its own version by default.

## Discovery stub (`skills/`)

`skills/source-fetch/SKILL.md` is a stable, minimal skill. Install it into
your agent with:

```sh
npx skills add zanza00/source-fetch
```

Its description carries trigger words for source-code searches, questions
about how a library works internally, and links to source on GitHub, Codeberg,
GitLab, or similar hosts. It directs agents to fetch the whole repository and
read the local checkout instead of downloading individual files, using
`source-fetch peek` for one-off repository links and `source-fetch path` for
manifest entries. Those reads need no further setup. For details, it routes
agents to the bundled guide that matches the task: `read`, `manifest`, or
`core`.
Its content intentionally does not change between releases, so an agent that
installed it long ago is never left with stale instructions.

## Bundled content (`source-fetch skills`)

The actual usage guides are embedded in the binary at build time
(`internal/skills/data/`) and served by the `skills` command. They are split
by workflow rather than by command:

| Skill | Covers |
|---|---|
| `read` | Reading source: links to Git hosts, libraries, `path`, `peek`, ref overrides, refreshing checkouts |
| `manifest` | `.source-fetch.toml`: discovery, fields, version matching, ref precedence, `manifest add/set/rm` |
| `core` | Shared concepts: command reference, output contract, checkout and cache behavior, troubleshooting |

By default, the guides come from the installed binary:

| Command | Description |
|---|---|
| `source-fetch skills` or `skills list` | List skills (name, description) |
| `source-fetch skills get <name>` | Print a skill's full content |
| `source-fetch skills get <name> --full` | Include `references/` and `templates/` files |
| `source-fetch skills get --all` | Print every skill |
| `source-fetch skills list --json` | Structured output (`{"success":true,"data":[...]}`) |

Hidden skills (frontmatter `hidden: true`) are omitted from `list` and
`--all` but can still be fetched by name — this is how a bundled stub can
redirect to a real skill without polluting listings.

`$SOURCE_FETCH_SKILLS_DIR` points the command at a filesystem directory
instead of the embedded copy (development and testing).
With this override, the content may differ from the installed CLI version.

## Authoring

- Bundled skills live in `internal/skills/data/<name>/SKILL.md`; a skill may
  add `references/` and `templates/` subdirectories (served with `--full`).
  New content requires a release, which is the point: it is version-locked to
  the binary.
- The discovery stub lives in `skills/source-fetch/SKILL.md`. Keep it thin
  and stable: description with trigger words, install instructions, and the
  redirect to the bundled guides. Do not add usage details that can drift.
  Older binaries only ship `core`, so the stub must keep `core` as a fallback.
- Put each topic in one bundled skill. Concepts shared by several workflows
  belong in `core`; `read` and `manifest` link to each other instead of
  duplicating content.
- Frontmatter is parsed by `internal/skills.ParseFrontmatter`, which
  understands `name`, `description` (folded multi-line) and `hidden`.
