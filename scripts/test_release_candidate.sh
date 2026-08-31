#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
resolver="${script_dir}/resolve_release_candidate.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
remote="$tmp/remote.git"
work="$tmp/work"

git init --bare -q "$remote"
git init -q -b main "$work"
git -C "$work" config user.name "Axiom Release Test"
git -C "$work" config user.email "release-test@example.invalid"
git -C "$work" remote add origin "$remote"

echo base > "$work/state.txt"
git -C "$work" add state.txt
git -C "$work" commit -q -m base
base_sha="$(git -C "$work" rev-parse HEAD)"
git -C "$work" branch release/v0.1.0 "$base_sha"

echo main >> "$work/state.txt"
git -C "$work" commit -qam main
main_sha="$(git -C "$work" rev-parse HEAD)"
git -C "$work" push -q origin main release/v0.1.0

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

expect_failure() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    fail "$label unexpectedly succeeded"
  fi
}

output="$(cd "$work" && "$resolver" manual v0.1.0 origin)"
grep -Fx "version=v0.1.0" <<<"$output" >/dev/null || fail "version output mismatch"
grep -Fx "semver_prerelease=false" <<<"$output" >/dev/null || fail "stable version classified as prerelease"
grep -Fx "target_sha=$base_sha" <<<"$output" >/dev/null || fail "resolver did not select frozen release commit"
[[ "$base_sha" != "$main_sha" ]] || fail "fixture must distinguish release candidate from main HEAD"

expect_failure "malformed core version" bash -c "cd '$work' && '$resolver' manual 'v01.1.0' origin"
expect_failure "leading-zero prerelease" bash -c "cd '$work' && '$resolver' validate 'v1.0.0-01'"
expect_failure "missing release branch" bash -c "cd '$work' && '$resolver' manual 'v0.2.0' origin"

# A divergent candidate must fail the ancestor contract.
git -C "$work" checkout -q --orphan divergent
git -C "$work" rm -q -rf .
echo divergent > "$work/divergent.txt"
git -C "$work" add divergent.txt
git -C "$work" commit -q -m divergent
git -C "$work" branch release/v0.3.0
git -C "$work" push -q origin release/v0.3.0
expect_failure "non-ancestor release candidate" bash -c "cd '$work' && '$resolver' manual 'v0.3.0' origin"

# Duplicate remote tags are rejected before publication.
git -C "$work" tag v0.1.0 "$base_sha"
git -C "$work" push -q origin refs/tags/v0.1.0
expect_failure "existing remote tag" bash -c "cd '$work' && '$resolver' manual 'v0.1.0' origin"

# Tag-event metadata derives prerelease state from SemVer, not from the leading v.
output="$(cd "$work" && "$resolver" validate v2.0.0)"
grep -Fx "semver_prerelease=false" <<<"$output" >/dev/null || fail "stable tag classified as prerelease"
output="$(cd "$work" && "$resolver" validate v2.0.0-rc.1+build.7)"
grep -Fx "semver_prerelease=true" <<<"$output" >/dev/null || fail "prerelease tag classified as stable"

# Pre-release frozen branches remain supported.
git -C "$work" checkout -q main
git -C "$work" branch release/v0.4.0-rc.1 "$main_sha"
git -C "$work" push -q origin release/v0.4.0-rc.1
output="$(cd "$work" && "$resolver" manual v0.4.0-rc.1 origin)"
grep -Fx "target_sha=$main_sha" <<<"$output" >/dev/null || fail "pre-release candidate mismatch"

echo "release candidate contract: PASS"
