# AGENTS.md

## Project Overview

`source-fetch` is a Go CLI that fetches the source code of a dependency on
demand and caches it locally, so a human or an agent can read it without
vendoring it into the consuming repo. Checkouts are version-matched (pinned to
the tag of the installed version when a `version_cmd` is configured),
namespaced per manifest, and cacheable via a one-off `peek` command.

- Language: Go 1.27.2 (module `github.com/zanza00/source-fetch`)
- Single external dependency: `github.com/BurntSushi/toml`
- Requires `git` on `PATH` (used as a subprocess wrapper)
- Output discipline: plain directory on stdout, progress on stderr — keep it

## Setup Commands

```sh
go build ./...          # compile everything
go build -o source-fetch ./cmd/source-fetch   # build the binary
go mod download         # fetch dependencies
```

No environment setup is needed beyond a working Go toolchain and `git`.
For local runs, `SOURCE_FETCH_CACHE` overrides the cache root entirely
(useful in tests/CI to avoid touching `$HOME`).

## Development Workflow

```sh
make build              # go build -o source-fetch ./cmd/source-fetch
make test               # go test ./...
make vet                # go vet ./...
make fmt                # gofmt -w .
make smoke              # test/run.sh (end-to-end smoke test, offline)
```

There is no watch mode; this is a CLI, not a server. Edit, build, test.

## Testing Instructions

```sh
go test ./...           # all unit tests (21 tests across 7 packages)
go test ./internal/fetch/          # one package
go test ./internal/fetch/ -run TestName   # one test
make smoke              # end-to-end smoke test
```

- Unit tests live next to the code: `<pkg>/<pkg>_test.go` in the same
  package (`manifest_test.go`, `cache_test.go`, etc.).
- The smoke test (`test/run.sh`) is fully offline: it builds the binary and
  runs it against a local bare git repo fixture. It is skipped on Windows in
  CI; keep new smoke assertions POSIX-shell compatible.
- Tests must not touch the network or the real user cache. Use
  `SOURCE_FETCH_CACHE` pointing at a temp dir when a cache root is needed.

## Code Style

- Standard Go conventions; run `gofmt -w .` (or `make fmt`) before committing.
- `go vet ./...` must be clean; CI fails otherwise.
- Package layout:

```
cmd/source-fetch/      entrypoint (main.go only — logic lives in internal/)
internal/cli/          command dispatch, version injection target
internal/manifest/     manifest model, load, discovery
internal/resolve/      version detection + ref precedence
internal/fetch/        fetch algorithm + at-ref fast path + marker
internal/cache/        cache root resolution, git ignore rules, clean guards
internal/git/          git subprocess wrapper
internal/skills/       skills served by the CLI (go:embed'd in data/)
skills/source-fetch/   discovery stub (installed into agents via `npx skills add`)
```

- Keep `main.go` minimal; all behavior belongs in `internal/`.
- Error handling: return errors up; `cli.Run` is the single exit-point
  boundary (it returns the exit code).
- Version is injected at build time via
  `-X github.com/zanza00/source-fetch/internal/cli.Version=<version>`;
  do not hardcode it.

## Build and Deployment

- CI (`.github/workflows/ci.yml`) calls reusable `checks.yml`: build, vet,
  test on ubuntu/macos/windows plus cross-compilation for amd64/arm64 and
  GoReleaser configuration validation. Smoke tests skip Windows.
- Work on a branch and open a PR into protected `main`. The `CI required`
  check must pass; direct pushes are blocked and no reviewer approval is
  required.
- Releases (`.github/workflows/release.yml`) start on stable `vX.Y.Z` tags,
  verify main ancestry, and run the same checks on the tagged commit before
  GoReleaser publishes. Do not publish releases locally.
- Releases use [goreleaser](https://goreleaser.com) (`make release-check`,
  config in `.goreleaser.yaml`): CGO disabled, stripped binaries, tar.gz
  archives (zip on Windows), checksums, CHANGELOG.
- `dist/` (goreleaser output) is gitignored — never commit it.
- Cross-compile locally with `GOOS=<os> GOARCH=<arch> go build ./cmd/source-fetch`.

## Pull Request Guidelines

- Commit messages follow Conventional Commits: `feat:`, `fix:`, `docs:`,
  `test:`, `chore:` (see `git log` for examples).
- Before submitting: `go build ./...`, `go vet ./...`, `go test ./...` and
  `make smoke` must all pass — CI runs exactly these.
- Add or update unit tests for the code you change; prefer table-driven
  tests, matching the existing `_test.go` files.

## Additional Notes

- Manifest files are flat TOML (`.source-fetch.toml`), discovered by walking
  upward from `$PWD`, or forced via `$SOURCE_FETCH_MANIFEST`. See
  `docs/manifest.md` and `manifest.example.toml`.
- The cache layout is documented in `README.md`; `clean` has safety guards
  (refuses empty/`/`/`$HOME`, the repository root, and any directory holding
  the manifest or the current directory) — preserve them when touching
  `internal/cache`.
- Inside a git repository the cache is project-local (`.source-fetch/`) and
  git-ignored through `.git/info/exclude`; outside one it lives in the user
  cache directory, namespaced per manifest. The project root is the git
  toplevel of the **current directory** — not of the manifest.
- Design notes live in `docs/` (`the_idea.md`, `transports.md`,
  `manifest.md`) — consult them before changing core fetch/resolve behavior.
