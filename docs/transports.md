# Transports

Today the only transport is `git`: `url` is a git remote URL and the fetch is a
shallow `clone`/`checkout`.

## Seam for future transports

The model already carries a `transport` field (default `"git"`), validated to
the set of supported kinds. Adding a transport means:

1. accepting the new `transport` value in `internal/manifest`,
2. implementing a fetcher with the same contract as `internal/fetch.Ensure`
   (given a target dir and a resolved ref, materialise a checkout and a marker),
3. wiring it in dispatch.

The marker file (`.source-fetch-ref`) and the `at_ref` fast path are
transport-agnostic and can be reused.

## Planned (not built)

- `tarball` / `http` — download and unpack an archive at a URL.
- registry-aware version mapping — resolve the ref from a lockfile
  (`package.json`, `Cargo.toml`, `go.mod`, `pyproject.toml`) instead of a CLI.
