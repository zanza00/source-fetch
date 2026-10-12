---
name: release
description: Load this skill when cutting a release of source-fetch (versioning, tagging, goreleaser publish, verifying the GitHub release), or when the user asks to publish, tag, or ship a new version of this repo.
metadata:
  internal: true
---

# Release source-fetch

Releases are built and published **locally** with goreleaser. There is no
release workflow in CI (`.github/workflows/ci.yml` only runs tests and
cross-compiles). Tags are lightweight. The release version is injected at build
time via `cli.Version` (see `.goreleaser.yaml` `ldflags`).

## Preconditions

- Working tree clean; all work committed and `main` up to date.
- Green locally: `go build ./... && go vet ./... && go test ./... && test/run.sh`.
- Decide the version (semver): new feature → bump minor (v0.1.2 → v0.2.0),
  fix/chore → bump patch. Existing tags: `git tag -l`.
- `gh` must be authenticated (`gh auth status`) — `GITHUB_TOKEN` is not in the
  environment; extract it with `gh auth token`.

## Steps, in this exact order

The order matters: goreleaser creates the **remote** tag at the remote
default-branch HEAD when the tag is missing on the remote. If `main` has not
been pushed yet, the remote tag lands on a stale commit and the release is
broken (this happened with v0.2.0 and had to be repaired by hand).

1. `git push origin main` — push the code **first**.
2. `git tag v0.x.y` (lightweight, matches existing tags).
3. `git push origin v0.x.y` — so the remote tag exists before goreleaser runs
   and cannot be recreated on the wrong commit.
4. Publish: `GITHUB_TOKEN=$(gh auth token) goreleaser release --clean`
   (same as `make release`). Requires a clean git tree. Builds
   linux/darwin/windows amd64/arm64, archives, `checksums.txt`, changelog from
   commit messages, publishes the GitHub Release.
5. Verify:
   - `gh release view v0.x.y` — not a draft, 7 assets (6 archives + checksums).
   - `git ls-remote origin refs/tags/v0.x.y` — must point to the released
     commit, not an older one.
   - `git fetch --tags origin` — local and remote tags in sync.

## Repair, if the remote tag points to the wrong commit

goreleaser-built artifacts are always correct (they build from the local
checkout and local tag); only the tag ref can be wrong. Move it with an
expected-value lease:

```
git push --force-with-lease=refs/tags/v0.x.y:<wrong-sha> origin refs/tags/v0.x.y
```

A GitHub Release attaches to the tag by name, so moving the tag ref keeps the
release and its assets intact.

## Authorization

Pushing `main`, pushing a tag, and publishing a release are remote,
hard-to-undo actions: get the user's go-ahead before step 1, and state the
version and what will be published in one line.
