# Release Management

This document describes the entire process for creating, signing, and publishing a Qodex release.

## Overview

A release produces:

- Cross-platform binaries for Linux, macOS, and Windows (amd64 + arm64).
- `.tar.gz` archives (`.zip` for Windows).
- GPG-detached signatures (`.sig` files) for every artifact.
- A `checksums.txt` file with SHA-256 hashes.
- A GitHub Release with the changelog and all artifacts.
- An updated Homebrew formula (manual step).

## Prerequisites

### Tools

- [GoReleaser](https://goreleaser.com/install/) (v2 or later)
- `gpg` (GnuPG) for signing
- A GPG key (see [gpg-signing.md](./gpg-signing.md) for setup)
- Write access to the GitHub repository

### GitHub Secrets

The release CI workflow reads these secrets from the repository:

| Secret | Purpose |
|---|---|
| `GPG_PRIVATE_KEY` | ASCII-armored GPG private key (exported with `gpg --export-secret-keys --armor`) |
| `GPG_FINGERPRINT` | The fingerprint of the key (e.g. `A1B2C3D4E5F6G7H8`) |
| `GITHUB_TOKEN` | Automatically provided by GitHub Actions |

## Versioning

Qodex follows [Semantic Versioning](https://semver.org/):

- **v0.Y.Z**: MVP phase. Breaking changes expected.
- **v1.Y.Z**: Stable API and CLI surface.

## Release Process (Step by Step)

### 1. Prepare the release

```sh
git checkout main
git pull origin main
```

Review the changelog and ensure the roadmap is up to date.

### 2. Commit any final changes

Make sure all changes for the release are committed on `main`.

```sh
git status        # should be clean
git log --oneline -5
```

### 3. Tag the release

```sh
git tag -s v0.1.0 -m "v0.1.0"
```

The `-s` flag signs the tag with your GPG key. If you haven't configured Git to use your GPG key:

```sh
git config --global user.signingkey A1B2C3D4E5F6G7H8
git config --global commit.gpgsign true
git config --global tag.gpgSign true
```

### 4. Push the tag (triggers CI release)

```sh
git push origin v0.1.0
```

This pushes the tag and triggers the `.github/workflows/release.yml` workflow, which:

1. Checks out the tag.
2. Runs GoReleaser with the `release` command.
3. Builds all cross-platform binaries.
4. Signs all artifacts with your GPG key if secrets are configured.
5. Creates a GitHub Release with changelog and uploads all artifacts.

### 5. Verify the release

1. Open https://github.com/benoybose/qodex/releases.
2. Confirm the release has all artifacts (archives + checksums.txt + .sig files).
3. Download one archive and verify the signature:

```sh
gpg --import gpg-public-key.asc
gpg --verify qodex_Linux_x86_64.tar.gz.sig qodex_Linux_x86_64.tar.gz
```

Output should include `Good signature from "Your Name <your@email.com>"`.

### 6. Update the Homebrew formula

The formula at `contrib/homebrew/qodex.rb` needs the real SHA-256 checksums for the release archives.

```sh
# Download all release archives and compute checksums
goreleaser release --snapshot --clean  # or download from GitHub Releases
shasum -a 256 dist/*.tar.gz dist/*.zip
```

Update the `sha256` entries in `contrib/homebrew/qodex.rb` with the actual values and commit.

### 7. Test the install script

```sh
curl -fsSL https://github.com/benoybose/qodex/raw/main/scripts/install.sh | sh
qodex version
```

Should print the newly released version.

### 8. Announce the release

- GitHub Release page handles the main announcement.
- Optionally post to relevant forums or social channels.

## Dry Run (for testing)

Before pushing a real tag, test the full release pipeline locally:

```sh
# Requires a local GPG key set up
goreleaser release --snapshot --clean
```

This produces unsigned binaries in `dist/` and skips the GitHub upload. Verify the artifacts look correct:

```sh
ls -la dist/
```

## CI Configuration Reference

### Signing in CI (`.github/workflows/release.yml`)

The release workflow imports your GPG key before running GoReleaser:

```yaml
- name: Import GPG key
  if: ${{ secrets.GPG_FINGERPRINT != '' }}
  uses: crazy-max/ghaction-import-gpg@v6
  with:
    gpg_private_key: ${{ secrets.GPG_PRIVATE_KEY }}
    passphrase: ""
```

This step is required for the `signs` section in `.goreleaser.yaml` to work.

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
| Tag release | `git tag -s v0.1.0 -m "v0.1.0"` |
| Push tag | `git push origin v0.1.0` |
| Verify release | Check GitHub Releases page |
| Update Homebrew | Update SHA-256s in `contrib/homebrew/qodex.rb` |
| Test install | Run `scripts/install.sh` |

## Files Involved

| File | Purpose |
|---|---|
| `.goreleaser.yaml` | GoReleaser config (builds, archives, signing, checksums) |
| `.github/workflows/release.yml` | GitHub Actions workflow that runs on `v*` tags |
| `.github/workflows/ci.yml` | CI workflow (tests + vet on push/PR) |
| `scripts/install.sh` | curl-pipe install script |
| `contrib/homebrew/qodex.rb` | Homebrew formula (SHA-256s updated per release) |
| `gpg-public-key.asc` | GPG public key for signature verification |
| `Makefile` | `release`, `snapshot`, `build-all` targets |
| `docs/release-management.md` | This document |
| `docs/gpg-signing.md` | GPG key and CI secret setup |
