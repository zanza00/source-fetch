# Manifest reference

A manifest is flat TOML. Each dependency is a table under `[source.<name>]`.
Only the canonical `[source.*]` prefix is supported.

```toml
[source.chezmoi]
url         = "https://github.com/twpayne/chezmoi.git"
version_cmd = "chezmoi --version"   # optional; ""/omitted = no version detection
tag_prefix  = "v"                    # optional; string between version and tag
ref         = "v2.73.0"              # optional persistent pin (tag/branch/SHA)
default_ref = "stable"               # optional fallback when version is unknown
```

## Fields

| Field | Required | Meaning |
|---|---|---|
| `url` | yes | git remote URL to clone. `repo` is accepted as an alias. |
| `transport` | no | transport kind; only `"git"` today (default). |
| `version_cmd` | no | shell command whose output contains the installed version. |
| `tag_prefix` | no | string between the version and the release tag (`""` or `"v"`). |
| `ref` | no | persistent pin (tag, branch or commit SHA). Wins over `version_cmd`. |
| `default_ref` | no | fallback ref when the version is empty/unparseable. |

`version_cmd` runs through the platform shell (`sh -c` on macOS/Linux,
`cmd /C` on Windows), so commands are shell-dependent.

## Ref resolution precedence

For a given entry: **CLI ref** → manifest **`ref`** → **version tag**
(`tag_prefix` + first `X.Y.Z` from `version_cmd`) → manifest **`default_ref`**
→ **default branch** (empty ref).

Version detection is skipped entirely when `version_cmd`'s first word is not on
`PATH`.

## Discovery (first match wins)

1. `$SOURCE_FETCH_MANIFEST` (explicit override).
2. Walk upward from `$PWD`: the first `.source-fetch.toml`.
3. `~/.config/source-fetch/manifest.toml`.

There is no baked-in default manifest.

## Editing from the CLI

`manifest add`, `manifest set` and `manifest rm` edit the manifest file so
you never hand-write TOML:

```sh
source-fetch manifest add chezmoi https://github.com/twpayne/chezmoi.git \
  --version-cmd "chezmoi --version" --tag-prefix v
source-fetch manifest set chezmoi ref=v2.73.0      # empty value clears: ref=
source-fetch manifest rm chezmoi                   # cache untouched; see rm
```

- `add` refuses to overwrite an existing entry unless `--force` is given.
- Writes target the manifest in use (`$SOURCE_FETCH_MANIFEST`, else the
  discovered one, else `.source-fetch.toml` in the current directory);
  `--global` targets `~/.config/source-fetch/manifest.toml` instead.
- Every write re-encodes the whole file from the model (atomic temp-file +
  rename, permissions preserved). Comments and layout of a hand-written
  file are not preserved, and the implied `transport = "git"` default is
  not written out.
