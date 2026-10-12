# Binary distribution

GitHub Actions runs GoReleaser after the release tag's validation and shared
checks pass. GoReleaser publishes the six platform archives and `checksums.txt`,
then updates `Formula/source-fetch.rb` in
[`zanza00/homebrew-tap`](https://github.com/zanza00/homebrew-tap).
The formula installs release binaries on macOS and Linux, for Intel and ARM64,
and declares Git as a runtime dependency. The tap tests installation on Linux
and both macOS architectures when the formula changes.

mise's GitHub backend and eget install directly from the release archives;
they need no separate package registry. See the installation commands in the
[README](../README.md#1-install-the-cli).

## Homebrew credentials

The release workflow's `GITHUB_TOKEN` can publish source-fetch releases but
cannot write to another repository. Configure a separate fine-grained token:

1. [Create a fine-grained personal access token](https://github.com/settings/personal-access-tokens/new)
   with resource owner `zanza00` and a suitable expiration date.
2. Choose **Only select repositories**, then select **homebrew-tap**.
3. Under repository permissions, set **Contents** to **Read and write**.
   Metadata read access is included automatically.
4. Save the token as an Actions repository secret named `HOMEBREW_TAP_TOKEN`
   in [source-fetch settings](https://github.com/zanza00/source-fetch/settings/secrets/actions/new).

The release job checks that this secret is present before publishing any
artifacts. Rotate it before expiration by replacing the same secret. The tap
has no scheduled update workflow: GoReleaser updates it as part of each release.

The supported custom publisher in `.goreleaser.yaml` runs
`scripts/publish-homebrew.py` for the checksum artifact after the release
archives are published. It generates the formula from those SHA-256 sums and
commits it through GitHub's Contents API. Identical content is a no-op, making
retries safe. The formula supports both macOS and Linux.

## Local validation

```sh
make release-check
goreleaser release --snapshot --clean --skip=publish
python3 scripts/publish-homebrew.py --tag v0.4.1 --artifact /path/to/release/checksums.txt --dry-run
```

The snapshot generates archives and checksums in `dist/` without publishing.
The publisher's `--dry-run` generates a formula next to stable release checksums
without calling GitHub. These checks do not verify the real token's write
permission; that is exercised by the next tagged release. Never commit `dist/`.
