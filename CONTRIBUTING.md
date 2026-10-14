# Contributing to source-fetch

## Requirements

- Go 1.27.2 or later (see [go.mod](go.mod)).
- `git` on `PATH`.
- A POSIX shell for the offline smoke test.

No other environment setup is needed. Download dependencies with
`go mod download`; Go also downloads them when you build or test.

## Build and run locally

```sh
make build
./source-fetch help
```

`make build` runs `go build -o source-fetch ./cmd/source-fetch`. You can also
run commands without creating a binary:

```sh
go run ./cmd/source-fetch skills get core
```

For local experiments, set `SOURCE_FETCH_CACHE` to a temporary directory to
avoid touching your normal cache. It overrides the cache root entirely,
without a manifest namespace.

## Development workflow

Edit the code, format it, and run the checks before submitting a change:

```sh
make fmt               # gofmt -w .
go build ./...
make vet               # go vet ./...
make test              # go test ./...
make smoke             # test/run.sh
```

CI runs build, vet, and unit tests on Linux, macOS, and Windows. The smoke
test runs on Linux and macOS and uses a local bare Git repository fixture;
it needs no network access. CI also cross-compiles for Linux, macOS, and
Windows on amd64 and arm64.

To run a single package or test:

```sh
go test ./internal/fetch/
go test ./internal/fetch/ -run TestName
```

Add or update unit tests for behavior changes. Tests live next to the code
in `<pkg>_test.go`; prefer table-driven tests where appropriate. Tests must
not access the network or the real user cache. Use temporary directories
and `SOURCE_FETCH_CACHE` when a cache root is needed. Keep smoke assertions
POSIX-shell compatible.

## Project structure

```text
cmd/source-fetch/      minimal entrypoint
internal/cli/          command dispatch and exit-code boundary
internal/manifest/     manifest model, loading, discovery, and editing
internal/resolve/      version detection and ref precedence
internal/fetch/        fetching, cache reuse, and checkout markers
internal/cache/        cache roots, namespaces, and clean guards
internal/git/          Git subprocess wrapper
internal/skills/       skills embedded in the CLI
skills/source-fetch/   discovery skill installed into coding agents
test/                 offline end-to-end smoke test
docs/                 reference documentation and design notes
```

## Code conventions

Follow standard Go conventions and run `gofmt` before committing. Keep
`main.go` minimal; put behavior in `internal/`. Return errors to callers;
`cli.Run` is the boundary that returns an exit code.

Commands that fetch source print a plain directory on stdout and progress
on stderr. Preserve that contract so scripts and agents can use the output.
Preserve the safety guards in cache cleanup, including rejection of empty,
root, and home-directory paths.

Before changing fetch or ref resolution, consult
[the design rationale](docs/the_idea.md), [transports](docs/transports.md), and
[the manifest reference](docs/manifest.md).

## Working on the skill

The discovery skill in `skills/source-fetch/SKILL.md` describes when to use
source-fetch and points to `source-fetch skills get core`. Keep it small;
operational instructions belong in `internal/skills/data/core/`, where they
are embedded in the binary and released with the CLI.

To preview edited bundled content without rebuilding:

```sh
SOURCE_FETCH_SKILLS_DIR=internal/skills/data source-fetch skills get core --full
```

See [the skills documentation](docs/skills.md) for authoring details.

## Submitting changes

Use Conventional Commits, such as `feat:`, `fix:`, `docs:`, `test:`, or
`chore:`. Explain the problem, the resulting behavior, and the checks you
ran in your pull request. Work on a branch and open a PR into `main`;
direct pushes to `main` are blocked. The required `CI required` check must
pass before merging. No second reviewer approval is required.

Build, vet, unit tests, and the smoke test must all pass before submission.
The shared checks in `.github/workflows/checks.yml` run on PRs, on `main`
after a merge, and on release tags.

## Release builds

Release builds use [`.goreleaser.yaml`](.goreleaser.yaml) and run on GitHub
Actions through [the release workflow](.github/workflows/release.yml).
After merging a PR and verifying CI on the exact `main` commit, a maintainer
pushes a lightweight `vX.Y.Z` tag. The workflow checks that the commit belongs
to `main`, runs the shared CI checks on it, and publishes only if they pass.
It builds six archives (Linux/macOS/Windows, amd64/arm64) and `checksums.txt`.
GoReleaser also updates the Homebrew tap using the `HOMEBREW_TAP_TOKEN` secret.
See [distribution setup](docs/distribution.md) for credentials and local validation.

Choose a minor bump for new features and a patch bump for fixes or maintenance.
See [the release skill](.agents/skills/release/SKILL.md) for the procedure.
`make release-check` validates the GoReleaser configuration locally without
publishing. The generated `dist/` directory is ignored and must not be
committed.

The release version is injected at build time using
`-X github.com/zanza00/source-fetch/internal/cli.Version=<version>`.
Do not hardcode it in the source.
