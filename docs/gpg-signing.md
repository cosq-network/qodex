# GPG Signing for Qodex Releases

This document explains how Qodex release artifacts are signed with GNU Privacy Guard (GnuPG), how maintainers configure GitHub Actions, and how users verify downloaded artifacts.

For the complete release pipeline, including Release Please, repository permissions, and branch protection, see [GitHub Release Pipeline Setup](github-release-setup.md) and [Release Management](release-management.md).

## 1. What signing provides

Qodex publishes detached OpenPGP signatures for release artifacts. A signature lets a user check that:

- the artifact was signed by the private key corresponding to the published Qodex release key;
- the downloaded artifact has not changed since it was signed;
- the signer identity matches the fingerprint the project publishes and documents.

Signing does not prove that the code is safe, that the build host was uncompromised, or that the signer reviewed every line. It provides artifact integrity and signer-key continuity. Always download the public key and artifacts from a trusted project channel, and treat a changed or unexpected fingerprint as a security incident.

## 2. How Qodex signs releases

The public tag workflow is .github/workflows/release.yml. It:

1. Runs release verification on Linux, macOS, and Windows.
2. Validates that the tag is vMAJOR.MINOR.PATCH and reachable from main.
3. Checks that the required GPG secrets exist.
4. Imports the protected private key into the ephemeral GitHub Actions runner.
5. Runs GoReleaser with the configured fingerprint.
6. Signs every configured artifact, including the checksum file.
7. Verifies checksums and signatures before publishing.
8. Exports gpg-public-key.asc into the runner's temporary directory and uploads it to the GitHub Release.

The workflow requires these repository secrets:

| Secret | Contents |
|---|---|
| GPG_PRIVATE_KEY | Complete ASCII-armored private-key export, including the BEGIN and END lines. |
| GPG_FINGERPRINT | Full 40-hex-character fingerprint of the primary release key. |
| GPG_PASSPHRASE | Passphrase protecting the exported private key. |

Secrets are read only by the release job. Do not print them, commit them, place them in a workflow file, or store them in go.mod, TOML configuration, skills, or issue comments.

## 3. Install GnuPG

### macOS

~~~sh
brew install gnupg
gpg --version
~~~

### Ubuntu or Debian

~~~sh
sudo apt-get update
sudo apt-get install -y gnupg2
gpg --version
~~~

### Fedora

~~~sh
sudo dnf install gnupg2
gpg --version
~~~

### Windows

Use Gpg4win in native Windows, Git Bash with GnuPG on PATH, or WSL2 with the Linux installation above.

~~~powershell
gpg --version
~~~

Use the same environment for key generation, export, and secret preparation. A key generated in WSL2 is available to WSL2's GPG keyring, not automatically to native PowerShell GPG.

## 4. Choose the release-key identity

Use a dedicated release key rather than a personal everyday encryption key. The UID should identify the maintainer or organization responsible for publishing Qodex artifacts.

If multiple keys exist, list them:

~~~sh
gpg --list-secret-keys --keyid-format LONG
~~~

Example:

~~~text
sec   rsa4096/58339D72E1BDD326 2026-08-14 [SC]
      5CD50DEBDDEAA761C21EAAEF58339D72E1BDD326
uid   [ultimate] BENOY BOSE <benoybose@cosq.net>
ssb   rsa4096/EF73FBA5A1678117 2026-08-14 [E]

sec   rsa4096/5AD162AB40A46C0E 2026-08-14 [SC]
      6C9801574799B7C7EEB607255AD162AB40A46C0E
uid   [ultimate] COSQ Network Private Limited <admin@cosq.net>
ssb   rsa4096/457C48712C5DCC0B 2026-08-14 [E]
~~~

The full fingerprint is the long 40-character line below sec. The shorter value after rsa4096/ is a key ID, not the preferred value for CI configuration.

For organization-owned releases, use the organization key. In the example above, that fingerprint is:

~~~text
6C9801574799B7C7EEB607255AD162AB40A46C0E
~~~

Use the key that your project governance actually approves. The example is not a reason to use that key in another repository.

## 5. Generate a dedicated release key

Run:

~~~sh
gpg --full-generate-key
~~~

Recommended choices for a broadly compatible release-signing key:

1. Select RSA and RSA when the prompt offers it.
2. Select a 4096-bit key size.
3. Set an expiration date according to your policy, such as one or two years.
4. Use the project or organization name as the real name.
5. Use a monitored project address.
6. Set a strong, unique passphrase.

Store the passphrase in an approved password manager. Do not use a passphrase from source control, a shell script, or a public CI variable.

### RSA versus ECC

RSA 4096 is a conservative choice for release interoperability. Modern GnuPG versions also support ECC keys, but available curves and signing-only prompts vary by GnuPG version and platform. Use an organization-approved ECC policy only when all consumers and CI tooling support it. The Qodex workflow consumes a normal GPG fingerprint and does not require a particular key algorithm.

### Expiration policy

Before a key expires:

1. extend the key expiration or generate a replacement key;
2. publish the new public key and fingerprint;
3. update GPG_PRIVATE_KEY, GPG_FINGERPRINT, and GPG_PASSPHRASE together;
4. run a test release or signature verification;
5. document the transition.

Never silently replace a release key without publishing the new fingerprint.

## 6. Retrieve fingerprints precisely

Human-readable listing:

~~~sh
gpg --list-secret-keys --keyid-format LONG
gpg --fingerprint 5AD162AB40A46C0E
~~~

Full fingerprint for a specific key ID:

~~~sh
gpg --list-secret-keys --with-colons 5AD162AB40A46C0E |
  awk -F: '$1 == "fpr" { print $10; exit }'
~~~

For the personal key in the example:

~~~sh
gpg --list-secret-keys --with-colons 58339D72E1BDD326 |
  awk -F: '$1 == "fpr" { print $10; exit }'
~~~

Using an email or UID:

~~~sh
gpg --list-secret-keys --with-colons 'admin@cosq.net' |
  awk -F: '$1 == "fpr" { print $10; exit }'
~~~

If nothing is returned, check the current keyring and GPG home:

~~~sh
gpgconf --list-dirs
gpg --list-secret-keys
~~~

The first fpr record is normally the primary key fingerprint. Later records may describe subkeys. For GPG_FINGERPRINT, use the full primary fingerprint unless the workflow explicitly documents a signing-subkey fingerprint.

To list all fingerprints:

~~~sh
gpg --list-secret-keys --with-colons KEY_ID |
  awk -F: '$1 == "fpr" { print $10 }'
~~~

## 7. Export key material safely

Set the selected primary fingerprint:

~~~sh
FINGERPRINT="6C9801574799B7C7EEB607255AD162AB40A46C0E"
~~~

Export the private key in ASCII-armored form:

~~~sh
gpg --armor --export-secret-keys "$FINGERPRINT" > qodex-release-private.asc
~~~

Export the public key:

~~~sh
gpg --armor --export "$FINGERPRINT" > qodex-release-public.asc
~~~

Inspect the public export:

~~~sh
gpg --show-keys --with-fingerprint qodex-release-public.asc
~~~

Confirm that the displayed fingerprint exactly matches GPG_FINGERPRINT.

Check that the private export exists without printing it:

~~~sh
test -s qodex-release-private.asc
wc -c qodex-release-private.asc
~~~

Do not commit the private export. Add a local exclude rule if useful:

~~~sh
printf '\nqodex-release-private.asc\n' >> .git/info/exclude
~~~

## 8. Configure GitHub Actions secrets

Open:

Settings → Secrets and variables → Actions → Repository secrets → New repository secret

Create exactly:

| Name | Value |
|---|---|
| GPG_PRIVATE_KEY | Entire contents of qodex-release-private.asc. |
| GPG_FINGERPRINT | Full primary fingerprint, with no spaces. |
| GPG_PASSPHRASE | The key passphrase. |

The names are case-sensitive. Do not create similarly named values such as GPG_KEY, GPG_KEY_ID, or GPG_PASSWORD.

Using GitHub CLI:

~~~sh
gh secret set GPG_FINGERPRINT --body "$FINGERPRINT"
gh secret set GPG_PRIVATE_KEY < qodex-release-private.asc
read -r -s PASSPHRASE
printf '%s' "$PASSPHRASE" | gh secret set GPG_PASSPHRASE
unset PASSPHRASE
gh secret list
~~~

GitHub does not let you retrieve secret values. If a value is uncertain, replace it.

Organization secrets must grant access to this repository. The current workflow reads repository secrets. Fork pull-request workflows should not receive release secrets; create release tags only in the canonical repository.

## 9. Validate the key before a release

Check that the private key exists locally:

~~~sh
gpg --list-secret-keys --keyid-format LONG "$FINGERPRINT"
~~~

Create and verify a detached test signature:

~~~sh
printf 'Qodex GPG signing test\n' > /tmp/qodex-signing-test.txt
gpg --batch --yes --local-user "$FINGERPRINT"   --output /tmp/qodex-signing-test.txt.sig   --detach-sign /tmp/qodex-signing-test.txt
gpg --verify /tmp/qodex-signing-test.txt.sig /tmp/qodex-signing-test.txt
~~~

Successful output should report a good signature from the expected UID and fingerprint.

Test the public key in an isolated temporary keyring:

~~~sh
TEST_GNUPGHOME="$(mktemp -d)"
chmod 700 "$TEST_GNUPGHOME"
GNUPGHOME="$TEST_GNUPGHOME" gpg --import qodex-release-public.asc
GNUPGHOME="$TEST_GNUPGHOME" gpg --list-keys --fingerprint
~~~

Do not remove your normal GPG home directory while cleaning up a test keyring.

## 10. Release artifacts and signatures

GoReleaser produces artifacts such as:

~~~text
qodex_linux_x86_64.tar.gz
qodex_linux_x86_64.tar.gz.sig
qodex_windows_amd64.zip
qodex_windows_amd64.zip.sig
checksums.txt
checksums.txt.sig
gpg-public-key.asc
~~~

Exact names depend on the release version and target matrix. The release workflow verifies checksums and detached signatures before publishing.

## 11. Verify a Qodex release

Download the artifact, its .sig file, checksums.txt, optionally checksums.txt.sig, and gpg-public-key.asc from the same GitHub Release.

Import and inspect the public key:

~~~sh
gpg --import gpg-public-key.asc
gpg --fingerprint
~~~

Compare the fingerprint with the project documentation or another trusted Qodex channel. Do not trust a good signature from an unexpected key.

Verify an artifact:

~~~sh
gpg --verify qodex_linux_x86_64.tar.gz.sig qodex_linux_x86_64.tar.gz
~~~

Verify the checksum file signature:

~~~sh
gpg --verify checksums.txt.sig checksums.txt
~~~

Verify checksums on Linux:

~~~sh
sha256sum --check checksums.txt --ignore-missing
~~~

Verify checksums on macOS:

~~~sh
shasum -a 256 -c checksums.txt
~~~

On PowerShell:

~~~powershell
Get-FileHash .\qodex_windows_amd64.zip -Algorithm SHA256
~~~

A successful signature normally includes:

~~~text
gpg: Good signature from "COSQ Network Private Limited <admin@cosq.net>"
~~~

A local trust warning is separate from signature validity. Confirm the full fingerprint independently.

Stop installation if GPG reports a bad signature, an unexpected fingerprint, or a checksum mismatch. Re-download all files from the same release and report suspected tampering.

## 12. Backup and revocation

Keep an offline encrypted backup of:

- the private primary key and signing subkeys;
- the revocation certificate;
- the full fingerprint and UID;
- the expiration and rotation policy;
- the passphrase in an approved password manager.

Create a revocation certificate soon after generating the key:

~~~sh
gpg --output qodex-release-revocation.asc   --gen-revoke "$FINGERPRINT"
~~~

Store it offline. Do not publish or import it unless the key is compromised, lost, or permanently retired.

Create an encrypted-storage backup:

~~~sh
gpg --armor --export-secret-keys "$FINGERPRINT" > qodex-release-private-backup.asc
~~~

Protect the backup and never store it in the repository, an unencrypted cloud drive, or a CI artifact.

## 13. Key rotation

Rotate a release key when it nears expiry, a maintainer leaves, the passphrase may be exposed, the private material is lost, or policy requires rotation.

Recommended sequence:

1. Generate a replacement key.
2. Verify it locally with a test signature.
3. Publish its public key and fingerprint through a trusted channel.
4. Update GPG_PRIVATE_KEY, GPG_FINGERPRINT, and GPG_PASSPHRASE together.
5. Run a controlled signing test.
6. Verify a new artifact with the new key.
7. Keep the old public key for historical release verification.
8. Revoke the old key only when it should no longer be trusted.

Never reuse an already released tag because a key changed. Publish a new patch release and document the transition.

## 14. Compromise response

If the private key or passphrase may be exposed:

1. Stop publishing releases.
2. Remove or disable the compromised GitHub secrets.
3. Revoke the key using the prepared certificate.
4. Generate a replacement key on a trusted machine.
5. Publish the new fingerprint independently.
6. Replace all three GitHub secrets.
7. Review releases, workflow runs, and repository audit logs.
8. Publish a security notice identifying affected releases and fingerprints.
9. Publish a new release signed by the replacement key.

Preserve evidence before deleting suspicious tags, releases, or workflow artifacts.

## 15. Security rules

- Use a unique passphrase for the release key.
- Do not pass the passphrase as a command-line argument in shared shell history.
- Do not write secrets to workflow logs, artifacts, or issue comments.
- Do not test release signing from untrusted fork code.
- Do not upload a private key as a normal artifact.
- Do not confuse a key ID with a full fingerprint.
- Do not use an empty passphrase for this repository's release key.
- Do not replace a public fingerprint without documenting the rotation.

## 16. Troubleshooting

### The fingerprint command prints nothing

Check the current keyring and GPG home:

~~~sh
gpgconf --list-dirs
gpg --list-secret-keys --keyid-format LONG
~~~

Use the exact key ID or email from the listing. If the key is in another keyring, set GNUPGHOME explicitly.

### GPG_PRIVATE_KEY is empty in Actions

Confirm the secret exists in the canonical repository and is named exactly GPG_PRIVATE_KEY. Fork and pull-request workflows may not receive secrets.

### GPG reports no secret key

The exported material may contain only a public key, the fingerprint may identify a different key, or the import failed. A private export must begin with:

~~~text
-----BEGIN PGP PRIVATE KEY BLOCK-----
~~~

Replace the secret with a fresh export rather than printing the key to inspect it.

### GPG reports a bad passphrase

Confirm GPG_PASSPHRASE exactly matches the passphrase used for the exported key. Avoid trailing newlines and accidental shell expansion. If uncertain, rotate the key.

### Fingerprint mismatch

GPG_FINGERPRINT must match the primary fingerprint of the imported private key. Re-export using the selected fingerprint and update the private key and fingerprint secrets together.

### The release has no signatures

Check that the GoReleaser signing step ran, GPG_FINGERPRINT was passed to the environment, and snapshot flags were not used. The protected tag workflow should fail before publishing when signing configuration is missing.

### Checksum verification fails

The artifact may be incomplete, modified, or paired with a checksum file from another release. Re-download all release files from the same tag and verify the checksum-file signature first.

## 17. Local development and snapshots

Local builds, CI tests, and GoReleaser snapshots do not require the release private key:

~~~sh
goreleaser release --snapshot --clean --skip=publish,sign
~~~

Never add a private key to a local .env file or use production release secrets for ordinary development.

## 18. Maintainer checklist

- [ ] GnuPG is installed on the key-management machine.
- [ ] A dedicated, passphrase-protected release key exists.
- [ ] The full primary fingerprint is recorded through a trusted channel.
- [ ] A revocation certificate is stored offline.
- [ ] An encrypted private-key backup exists.
- [ ] GPG_PRIVATE_KEY is configured as a repository secret.
- [ ] GPG_FINGERPRINT contains the full primary fingerprint.
- [ ] GPG_PASSPHRASE is configured separately.
- [ ] The public key is available for verification.
- [ ] A controlled signature verification succeeds locally.
- [ ] The first release contains signatures, checksums, and gpg-public-key.asc.

## 19. Reference commands

~~~sh
gpg --list-secret-keys --keyid-format LONG
gpg --list-secret-keys --with-colons KEY_ID |
  awk -F: '$1 == "fpr" { print $10; exit }'
gpg --armor --export-secret-keys FINGERPRINT > qodex-release-private.asc
gpg --armor --export FINGERPRINT > qodex-release-public.asc
gpg --show-keys --with-fingerprint qodex-release-public.asc
gpg --detach-sign --armor --local-user FINGERPRINT release-test.txt
gpg --verify release-test.txt.asc release-test.txt
gh secret set GPG_PRIVATE_KEY < qodex-release-private.asc
gh secret set GPG_FINGERPRINT --body "$FINGERPRINT"
read -r -s PASSPHRASE
printf '%s' "$PASSPHRASE" | gh secret set GPG_PASSPHRASE
unset PASSPHRASE
~~~

For release-pipeline permissions and Release Please configuration, continue with [GitHub Release Pipeline Setup](github-release-setup.md).
