---
name: release
description: Cut a source-fetch release by choosing a version, tagging verified main, and monitoring the GitHub Actions publication.
metadata:
  internal: true
---

# Release source-fetch

GitHub Actions builds and publishes releases through
`.github/workflows/release.yml` when a lightweight `vX.Y.Z` tag is pushed.
The workflow validates that the tagged commit belongs to `main`, runs the
shared checks on that exact commit, and publishes with GoReleaser only after
all checks pass. GoReleaser injects `cli.Version` from the tag and updates
`Formula/source-fetch.rb` in `zanza00/homebrew-tap` from the release checksums.

## Prepare

- Fetch `origin` and tags, inspect `git status` and commits since the latest
  release. Use a minor bump for features and a patch bump for fixes or
  maintenance. The workflow supports only stable `vX.Y.Z` versions.
- All changes must land through a PR with the required CI check green.
  Do not push directly to protected `main`.
- Release from a clean local `main` that matches `origin/main`. Update with
  `git pull --ff-only origin main`; stop if the branch has unmerged local work.
- Verify the `ci` run for the exact release commit is successful with
  `gh run list --workflow ci.yml --commit <sha>` and `gh run view <run-id>`.
  Wait for active runs; fix failures through another PR before releasing.
- `gh` must be authenticated. No local token extraction or local GoReleaser
  publication is needed.
- Check that the `HOMEBREW_TAP_TOKEN` Actions secret is configured (for example,
  inspect secret names with `gh secret list --repo zanza00/source-fetch`).
  The publish job requires it before uploading artifacts. It must grant Contents
  read/write access to `zanza00/homebrew-tap`; secret presence alone does not
  verify that permission. See [distribution setup](../../../docs/distribution.md)
  for configuration. Keep the secret value in GitHub Actions.

## Publish

State the chosen version and that pushing its tag triggers publication of
six platform archives and `checksums.txt`, plus the Homebrew tap update.
Obtain authorization if the current conversation has not already authorized
the release.

1. Record `git rev-parse HEAD` and check that the proposed tag does not exist
   locally or remotely.
2. Create a lightweight tag: `git tag vX.Y.Z <recorded-sha>`.
3. Push only the tag: `git push origin vX.Y.Z`.
4. Find the `release.yml` run for that tag and SHA, then monitor it with
   `gh run watch <run-id> --exit-status`.
5. Verify `gh release view vX.Y.Z`: published, not draft or prerelease,
   seven assets (six archives plus `checksums.txt`). Check
   `git ls-remote origin refs/tags/vX.Y.Z` matches the recorded SHA, and
   `git fetch --tags origin` to synchronize tags.
6. Inspect `Formula/source-fetch.rb` on `zanza00/homebrew-tap` main: its version
   and four macOS/Linux archive URLs must match the release, with SHA-256 values
   matching `checksums.txt`. Check the tap's CI run for the formula update and
   report any installation failures.

## Failed releases

If validation or checks fail, publication is skipped. Fix code or workflow
failures through a PR and release a new version after its CI passes; do not
move existing release tags or publish locally to bypass the checks.

If publication fails transiently, inspect the run and any partial release
before retrying, including whether the Homebrew formula was updated. A failed
tap update can leave release assets already published; the formula publisher
is a no-op when the content already matches. Rerun failed jobs for the same tag
with
`gh run rerun <run-id> --failed` when the retry is within the user's existing
release authorization. Stop and report persistent failures rather than
retrying indefinitely or deleting published assets without authorization.
