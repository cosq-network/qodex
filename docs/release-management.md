# Release Management

This document describes the current automated process for versioning, signing, packaging, and publishing a Qodex release.

## Overview

A release produces:

- Cross-platform binaries for Linux, macOS, and Windows (`amd64` + `arm64`).
- `.tar.gz` archives for Linux/macOS and `.zip` archives for Windows.
- Native Linux packages (`.deb`, `.rpm`, `.apk`).
- GPG-detached signatures (`.sig` files) for every artifact.
- A `checksums.txt` file with SHA-256 hashes.
- An exported `gpg-public-key.asc` file attached to the GitHub Release.
- A GitHub Release with the changelog and all artifacts.
- A repo-managed changelog and semantic version tag generated automatically from merged conventional commits.

## Prerequisites

### Tools

- [GoReleaser](https://goreleaser.com/install/) (v2 or later)
- `gpg` (GnuPG) for signing
- A GPG key (see [gpg-signing.md](./gpg-signing.md) for setup)
- GitHub release pipeline setup (see [github-release-setup.md](./github-release-setup.md))
- Write access to the GitHub repository

### GitHub Secrets

The release CI workflow reads these secrets from the repository:

| Secret | Purpose |
|---|---|
| `GPG_PRIVATE_KEY` | ASCII-armored GPG private key (exported with `gpg --export-secret-keys --armor`) |
| `GPG_FINGERPRINT` | The fingerprint of the key (e.g. `A1B2C3D4E5F6G7H8`) |
| `RELEASE_PLEASE_TOKEN` | Personal access token used by Release Please so created tags trigger the release workflow; it must also be able to create refs for commits that touch workflow files |
| `GITHUB_TOKEN` | Automatically provided by GitHub Actions |

## Versioning

Qodex follows [Semantic Versioning](https://semver.org/) and uses [Release Please](https://github.com/googleapis/release-please) to automate version management:

- **v0.Y.Z**: MVP phase. Breaking changes expected.
- **v1.Y.Z**: Stable API and CLI surface.
- Merged conventional commits on `main` update the release PR and `CHANGELOG.md`.
- Merging the release PR creates the git tag that triggers `.github/workflows/release.yml`.
- `RELEASE_PLEASE_TOKEN` should be a fine-grained or classic token with permission to create pull requests, create refs, and create tags in this repository.
- Fine-grained PATs should include `Contents: Read and write`, `Pull requests: Read and write`, `Workflows: Read and write`, and `Metadata: Read`.
- Classic PATs should include `repo` and `workflow`.

## Release Process (Step by Step)

### 1. Merge changes using conventional commits

```sh
git commit -m "feat: add ..."
git commit -m "fix: resolve ..."
```

Release Please uses commit history to decide the next version and changelog entries.

### 2. Review the release PR

When new releasable commits land on `main`, `.github/workflows/release-please.yml` updates or opens a release PR. Review:

```sh
gh pr view --web
```

Confirm:

- the version bump
- `CHANGELOG.md`
- any doc or formula updates you want included in the release commit

### 3. Merge the release PR

Merging the release PR creates and pushes the next `v*` tag automatically. That tag triggers `.github/workflows/release.yml`, which:

1. Checks out the tag.
2. Verifies the signing secrets are present.
3. Imports the release signing key.
4. Runs `go test -race -count=1 ./...`.
5. Runs GoReleaser.
6. Publishes archives, Linux packages, checksums, and signatures to GitHub Releases.
7. Uploads `gpg-public-key.asc`.

### 4. Verify the release

1. Open https://github.com/benoybose/qodex/releases.
2. Confirm the release has:
   - `qodex_linux_*`, `qodex_darwin_*`, `qodex_windows_*` archives
   - Linux `.deb`, `.rpm`, and `.apk` packages
   - `checksums.txt`
   - `.sig` files
   - `gpg-public-key.asc`
3. Download one artifact and verify the signature:

```sh
gpg --import gpg-public-key.asc
gpg --verify qodex_linux_x86_64.tar.gz.sig qodex_linux_x86_64.tar.gz
```

Output should include `Good signature from "Your Name <your@email.com>"`.

### 5. Update package metadata that is intentionally manual

The Homebrew formula at `contrib/homebrew/qodex.rb` still needs real SHA-256 values per release.

```sh
# Download release archives and compute checksums
shasum -a 256 dist/*.tar.gz dist/*.zip
```

Update the `sha256` entries in `contrib/homebrew/qodex.rb` with the actual values and commit.

### 6. Test install flows

```sh
curl -fsSL https://github.com/benoybose/qodex/raw/main/scripts/install.sh | sh
qodex version
```

On Windows PowerShell:

```powershell
irm https://github.com/benoybose/qodex/raw/main/scripts/install.ps1 | iex
qodex version
```

### 7. Announce the release

- GitHub Release page handles the main announcement.
- Optionally post to relevant forums or social channels.

## Dry Run (for testing)

Before pushing a real tag, test the full release pipeline locally:

```sh
# Requires a local GPG key set up
goreleaser release --snapshot --clean --skip=publish,sign
```

This produces unsigned snapshot artifacts in `dist/` and skips publishing. Verify the artifacts look correct:

```sh
ls -la dist/
```

## CI Configuration Reference

### Signing in CI (`.github/workflows/release.yml`)

The release workflow validates the secrets first, then imports the GPG key before running GoReleaser:

```yaml
- name: Validate signing configuration
  run: |
    if [[ -z "${GPG_PRIVATE_KEY}" || -z "${GPG_FINGERPRINT}" ]]; then
      exit 1
    fi

- name: Import GPG key
  uses: crazy-max/ghaction-import-gpg@v6
  with:
    gpg_private_key: ${{ secrets.GPG_PRIVATE_KEY }}
    passphrase: ""
```

This is required for the `signs` section in `.goreleaser.yaml` to work.

### Signing config (`.goreleaser.yaml`)

```yaml
signs:
  - artifacts: all
    args: ["--batch", "-u", "{{ .Env.GPG_FINGERPRINT }}", "--output", "${signature}", "--detach-sign", "${artifact}"]
    signature: "${artifact}.sig"
```

- `artifacts: all` signs every binary, archive, and checksum.
- `--batch` prevents GPG from prompting for a passphrase (not needed for CI keys).
- The `GPG_FINGERPRINT` env var is set from the secret.

## Rollback

If a release is broken:

1. **Delete the GitHub Release** (optional — it can be left for reference).
2. **Delete the tag**:
   ```sh
   git tag -d v0.1.0
   git push origin :refs/tags/v0.1.0
   ```
3. **Fix the issue**, commit, and tag a new patch version (`v0.1.1`).

Never re-use a tag that was already released.

## Checklist Summary

| Step | Command |
|---|---|
| Create GPG key | `gpg --full-generate-key` |
| Export public key | `gpg --export --armor FINGERPRINT > gpg-public-key.asc` |
| Store secrets | `GPG_PRIVATE_KEY` + `GPG_FINGERPRINT` in GitHub repo |
| Store release token | `RELEASE_PLEASE_TOKEN` in GitHub repo |
| Open/update release PR | automatic on push to `main` |
| Merge release PR | creates and pushes the next `v*` tag |
| Verify release | Check GitHub Releases page |
| Update Homebrew | Update SHA-256s in `contrib/homebrew/qodex.rb` |
| Test install | Run `scripts/install.sh` or `scripts/install.ps1` |

## Files Involved

| File | Purpose |
|---|---|
| `.goreleaser.yaml` | GoReleaser config (builds, archives, signing, checksums) |
| `.github/workflows/release-please.yml` | Automated semantic version and changelog management |
| `.github/workflows/release.yml` | GitHub Actions workflow that runs on `v*` tags |
| `.github/workflows/ci.yml` | CI workflow (tests + vet on push/PR) |
| `scripts/install.sh` | curl-pipe install script |
| `scripts/install.ps1` | PowerShell install script for Windows |
| `contrib/homebrew/qodex.rb` | Homebrew formula (SHA-256s updated per release) |
| `CHANGELOG.md` | Release Please managed changelog |
| `Makefile` | `release`, `snapshot`, `build-all` targets |
| `docs/release-management.md` | This document |
| `docs/gpg-signing.md` | GPG key and CI secret setup |
