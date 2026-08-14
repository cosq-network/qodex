#!/usr/bin/env bash
set -euo pipefail

require_command() {
  local command_name="$1"
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "required external tool not found: ${command_name}" >&2
    exit 1
  fi
}

for command_name in git gpg go gopls; do
  require_command "$command_name"
done

git --version
gpg --version | sed -n '1p'
go version
gopls version

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

# Exercise Git with a path containing spaces and verify the checkout remains
# clean after a real commit.
git -C "$work_dir" init -q
git -C "$work_dir" config user.name "Qodex CI"
git -C "$work_dir" config user.email "qodex-ci@example.invalid"
mkdir -p "$work_dir/path with spaces"
printf 'line one\nline two\n' > "$work_dir/path with spaces/sample.txt"
git -C "$work_dir" add .
git -C "$work_dir" commit -q -m "external tool smoke test"
test -z "$(git -C "$work_dir" status --porcelain)"

# Use an isolated keyring. This validates that CI can create, use, and verify
# a detached signature without depending on maintainer keys.
export GNUPGHOME="$work_dir/gnupg"
mkdir -m 700 "$GNUPGHOME"
gpg --batch --pinentry-mode loopback --passphrase "ci-passphrase" \
  --quick-generate-key "Qodex CI <qodex-ci@example.invalid>" rsa2048 sign 1d
printf 'qodex external-tool signature smoke test\n' > "$work_dir/sign-me.txt"
gpg --batch --pinentry-mode loopback --passphrase "ci-passphrase" \
  --armor --detach-sign --local-user "qodex-ci@example.invalid" \
  --output "$work_dir/sign-me.txt.asc" "$work_dir/sign-me.txt"
gpg --batch --verify "$work_dir/sign-me.txt.asc" "$work_dir/sign-me.txt"
