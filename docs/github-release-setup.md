# GitHub Release Pipeline Setup

This guide covers the one-time setup required to make the Qodex GitHub release pipelines work end to end.

It covers:

- generating a GPG signing key for release artifacts
- exporting the private key and fingerprint for GitHub Actions
- creating the GitHub token used by Release Please
- configuring repository Actions settings and branch protections
- validating the first release flow

This document matches the repository workflow setup as of July 16, 2026.

## What the pipelines expect

This repository uses three GitHub Actions workflows:

- `.github/workflows/ci.yml`
  Runs lint, tests, and snapshot packaging validation on pushes and pull requests.
- `.github/workflows/release-please.yml`
  Maintains the release PR and changelog, then creates the semantic version tag when the release PR is merged.
- `.github/workflows/release.yml`
  Runs on `v*` tags, executes release-time tests, builds artifacts, signs them, and publishes the GitHub Release.

For these workflows to work correctly, the repository needs:

- a GPG signing key
- GitHub Actions repository secrets
- a Release Please token that can create tags and pull requests
- Actions workflow permissions that allow release publishing

## 1. Install prerequisites locally

You need:

- `gpg`
- GitHub repository admin or maintainer access
- a GitHub account that can create a personal access token

Install GnuPG if needed:

```sh
# macOS
brew install gnupg

# Ubuntu / Debian
sudo apt-get update
sudo apt-get install -y gnupg2
```

On Windows, use one of:

- WSL2
- Git Bash with GnuPG installed
- Gpg4win

## 2. Generate a GPG key for releases

Create a dedicated signing key for release automation:

```sh
gpg --full-generate-key
```

Recommended choices:

- key type: `ECC`
- curve: `Curve 25519`
- usage: `signing only`
- expiration: choose your policy, for example `1y` or `2y`
- name: your maintainer name
- email: the email you use with GitHub

For this repository’s current workflow, the simplest path is an empty passphrase because `.github/workflows/release.yml` imports the key with `passphrase: ""`.

If you want a passphrase-protected CI key later, the workflow will need to be updated to pass and use that secret explicitly.

## 3. Get the fingerprint and key ID

List the key:

```sh
gpg --list-keys --keyid-format=long
```

Example output shape:

```text
pub   ed25519/0123456789ABCDEF 2026-07-16 [SC]
      111122223333444455556666777788889999AAAA
uid                 [ultimate] Your Name <you@example.com>
```

Record:

- the long fingerprint, for example `111122223333444455556666777788889999AAAA`
- the key ID, for example `0123456789ABCDEF`

The full fingerprint becomes the `GPG_FINGERPRINT` GitHub secret.

## 4. Export the private and public keys

Export the private key in ASCII-armored form:

```sh
gpg --armor --export-secret-keys YOUR_KEY_ID > gpg-private-key.asc
```

Export the public key too:

```sh
gpg --armor --export YOUR_KEY_ID > gpg-public-key.asc
```

You will:

- paste `gpg-private-key.asc` into the `GPG_PRIVATE_KEY` GitHub secret
- keep `gpg-public-key.asc` available for local verification if needed

After storing the secret in GitHub, delete the private key export file:

```sh
shred -u gpg-private-key.asc
```

On systems without `shred`, remove it securely using your platform’s equivalent.

## 5. Create the Release Please token

The release automation cannot rely on the default `GITHUB_TOKEN` for tag creation, because tags created by a workflow with that token do not trigger the separate `release.yml` workflow reliably for this setup.

Create a personal access token and store it as `RELEASE_PLEASE_TOKEN`.

### Fine-grained token

Recommended repository permissions for this repo:

- `Contents: Read and write`
- `Pull requests: Read and write`
- `Metadata: Read`

Limit the token to this repository if possible.

### Classic token

If you use a classic token, `repo` scope is the practical choice.

## 6. Add repository secrets

In GitHub, open:

`Settings -> Secrets and variables -> Actions -> Repository secrets`

Create these secrets:

| Secret | Value |
|---|---|
| `GPG_PRIVATE_KEY` | Full contents of `gpg-private-key.asc` |
| `GPG_FINGERPRINT` | Full 40-character fingerprint |
| `RELEASE_PLEASE_TOKEN` | Fine-grained PAT or classic PAT with the required repo permissions |

Notes:

- `GITHUB_TOKEN` is provided automatically by GitHub Actions. Do not create it manually.
- If you rotate the GPG key, update both `GPG_PRIVATE_KEY` and `GPG_FINGERPRINT`.
- If you rotate the Release Please token, update `RELEASE_PLEASE_TOKEN`.

## 7. Configure GitHub Actions settings

Open:

`Settings -> Actions -> General`

Recommended settings:

- Actions permissions: allow GitHub Actions to run
- Workflow permissions: `Read and write permissions`
- Enable: `Allow GitHub Actions to create and approve pull requests`

These settings are important because:

- `release-please.yml` updates or creates the release PR
- `release.yml` uploads release assets to GitHub Releases

## 8. Configure branch protection for `main`

Open:

`Settings -> Branches`

Create or update the branch protection rule for `main`.

Recommended protections:

- require a pull request before merging
- require at least one approval
- require status checks to pass before merging
- require branches to be up to date before merging

Recommended required checks:

- `Lint`
- `Test (1.26, ubuntu-latest)`
- `Test (1.26, macos-latest)`
- `Test (1.26, windows-latest)`
- `Packaging snapshot`

If the exact job labels differ later, update the branch protection rule to match the current workflow names.

## 9. Use conventional commits

Release Please derives release notes and version bumps from commit history.

Use commit messages like:

```text
feat: add Windows installer
fix: restore release-time tests
docs: update release setup guide
chore: refresh CI workflow
```

Common effect:

- `fix:` usually bumps patch version
- `feat:` usually bumps minor version
- breaking changes should be marked in the conventional-commit format expected by your team

## 10. Verify the first automated release

After the secrets and settings are in place:

1. Merge a normal pull request into `main`.
2. Confirm `CI` passes.
3. Confirm `Release Please` opens or updates a release PR.
4. Review the generated version bump and `CHANGELOG.md`.
5. Merge the release PR.
6. Confirm the merged release PR creates a new `v*` tag.
7. Confirm `.github/workflows/release.yml` runs for that tag.
8. Open the GitHub Releases page and verify the artifacts.

Expected release outputs:

- Linux archives
- macOS archives
- Windows archives
- Linux `.deb`, `.rpm`, and `.apk` packages
- `checksums.txt`
- `.sig` files
- `gpg-public-key.asc`

## 11. Verify a signature locally

Download the public key and one artifact from the GitHub Release, then run:

```sh
gpg --import gpg-public-key.asc
gpg --verify qodex_linux_x86_64.tar.gz.sig qodex_linux_x86_64.tar.gz
```

You should see a `Good signature` message for your release key.

## 12. Troubleshooting

### Release Please PR is not created

Check:

- `RELEASE_PLEASE_TOKEN` exists
- the token has repo write access
- Actions are enabled for the repository
- commits merged to `main` use releasable conventional commit types such as `feat:` or `fix:`

### Release PR merges but no signed release is published

Check:

- the merge created a `v*` tag
- `release.yml` ran for that tag
- `RELEASE_PLEASE_TOKEN` is being used by `.github/workflows/release-please.yml`

### Release workflow fails before publishing

Check:

- `GPG_PRIVATE_KEY` is valid ASCII-armored private key material
- `GPG_FINGERPRINT` matches the imported private key
- the exported private key was not passphrase-protected unless the workflow is updated for passphrase support

### Windows installer download fails

Check that the GitHub Release contains:

- `qodex_windows_x86_64.zip`
- `qodex_windows_arm64.zip`

### Homebrew formula still has placeholder hashes

This is expected until you manually update `contrib/homebrew/qodex.rb` after a real release.

## Related documents

- [gpg-signing.md](./gpg-signing.md)
- [release-management.md](./release-management.md)
