# Release: GPG Signing Setup

Use this document to enable or skip GPG signing for Qodex GitHub Releases.

## Purpose

Signing attaches a detached `.sig` to every release artifact so users can verify the binary came from this project. It does not change how GoReleaser builds or publishes releases.

## How this project uses signing now

- CI imports the key only when `GPG_FINGERPRINT` is set.
- GoReleaser already runs `go mod tidy`, builds, creates archives, checksums, and a GitHub Release.
- Signing is used only if you provide `GPG_PRIVATE_KEY` and `GPG_FINGERPRINT` as GitHub Actions secrets.

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

## Optional: skip signing entirely

You do not need GPG for development builds or internal testing. If `GPG_FINGERPRINT` is not set:
- The release workflow skips key import.
- GoReleaser produces unsigned artifacts and continues the release.

To remove signing end to end:
1. Delete the `GPG_PRIVATE_KEY` and `GPG_FINGERPRINT` secrets.
2. Remove or comment the `signs:` section in `.goreleaser.yaml`.
