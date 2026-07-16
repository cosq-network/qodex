# Release: GPG Signing Setup

Use this document to configure the signing key required by the public release workflow.

For the full repository setup, including `RELEASE_PLEASE_TOKEN`, Actions settings, and branch protection guidance, see [github-release-setup.md](./github-release-setup.md).

## Purpose

Signing attaches a detached `.sig` to every release artifact so users can verify the binary came from this project. The release workflow also publishes the matching ASCII-armored public key as `gpg-public-key.asc`.

## How this project uses signing now

- Public tag releases require `GPG_PRIVATE_KEY` and `GPG_FINGERPRINT` GitHub Actions secrets.
- The release workflow imports the private key, runs GoReleaser, signs every published artifact, and uploads `gpg-public-key.asc` to the GitHub Release.
- Development builds and snapshot packaging do not require GPG.

## Prerequisites

- `gpg` installed
  - macOS: `brew install gnupg`
  - Ubuntu: `sudo apt-get install gnupg2`
  - Windows: WSL or Git Bash
- A GitHub repository with Actions enabled
- Optional but recommended: an empty passphrase for CI key import

## Step 1: create a key

```sh
gpg --full-generate-key
```

Choose:
- (9) ECC / (1) Curve 25519
- Signing only
- Name and email you already use with GitHub
- Empty passphrase to match `passphrase: ""` in the release workflow

## Step 2: get the fingerprint

```sh
gpg --list-keys --keyid-format=long
```

Copy the 40-character hex string under the `fpr` line. This is the `GPG_FINGERPRINT`.

## Step 3: export the private key

```sh
gpg --armor --export-secret-keys YOUR_KEY_ID > gpg-private-key.asc
```

Paste the file contents into the GitHub secret `GPG_PRIVATE_KEY`.

## Step 4: add GitHub secrets

Go to the repository:
`Settings -> Secrets and variables -> Actions -> New repository secret`

Add:
- `GPG_PRIVATE_KEY`: full contents of `gpg-private-key.asc`
- `GPG_FINGERPRINT`: the 40-character hex fingerprint

## Safety notes

- Do not commit `gpg-private-key.asc`
- Delete the file after storing it in GitHub secrets
  - macOS/Linux: `shred -u gpg-private-key.asc`
  - Windows: `del gpg-private-key.asc`

## Optional: verify with the public key

```sh
gpg --export --armor YOUR_KEY_ID > gpg-public-key.asc
```

Users can verify:
```sh
gpg --import gpg-public-key.asc
gpg --verify qodex_*.tar.gz.sig qodex_*.tar.gz
```

## Development vs release

You do not need GPG for local builds, CI test runs, or snapshot packaging:

```sh
goreleaser release --snapshot --clean --skip=publish,sign
```

Public tag releases are intentionally stricter. If the signing secrets are missing, `.github/workflows/release.yml` fails before publishing anything.
