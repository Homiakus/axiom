#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <validate|manual> <version> [remote]" >&2
}

mode="${1:-}"
version="${2:-}"
remote="${3:-origin}"

if [[ "$mode" != "validate" && "$mode" != "manual" ]]; then
  usage
  exit 64
fi
if [[ -z "$version" ]]; then
  usage
  exit 64
fi

# Strict SemVer-shaped Git tag. Validate before interpolating version into any
# refspec so workflow input cannot become an arbitrary refspec.
semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?(\+([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?$'
if [[ ! "$version" =~ $semver_re ]]; then
  echo "release metadata: invalid SemVer tag: $version" >&2
  exit 65
fi

without_build="${version%%+*}"
prerelease="false"
if [[ "$without_build" == *-* ]]; then
  prerelease="true"
  prerelease_ids="${without_build#*-}"
  IFS='.' read -r -a ids <<< "$prerelease_ids"
  for id in "${ids[@]}"; do
    if [[ "$id" =~ ^[0-9]+$ && ${#id} -gt 1 && "$id" == 0* ]]; then
      echo "release metadata: numeric prerelease identifiers must not contain leading zeroes: $version" >&2
      exit 65
    fi
  done
fi

printf 'version=%s\n' "$version"
printf 'semver_prerelease=%s\n' "$prerelease"

if [[ "$mode" == "validate" ]]; then
  exit 0
fi

if ! git rev-parse --git-dir >/dev/null 2>&1; then
  echo "release metadata: must run inside a git repository" >&2
  exit 66
fi
if ! git remote get-url "$remote" >/dev/null 2>&1; then
  echo "release metadata: git remote not found: $remote" >&2
  exit 67
fi

main_ref="refs/remotes/${remote}/main"
release_ref="refs/remotes/${remote}/release/${version}"

# Fetch exactly the refs participating in the release decision instead of
# trusting whatever branch refs happen to exist in the workflow checkout.
git fetch --quiet --no-tags "$remote" \
  "+refs/heads/main:${main_ref}" \
  "+refs/heads/release/${version}:${release_ref}"

main_sha="$(git rev-parse --verify "${main_ref}^{commit}")"
target_sha="$(git rev-parse --verify "${release_ref}^{commit}")"

if ! git merge-base --is-ancestor "$target_sha" "$main_sha"; then
  echo "release metadata: ${release_ref} (${target_sha}) is not an ancestor of ${main_ref} (${main_sha})" >&2
  exit 68
fi

set +e
tag_lookup="$(git ls-remote --exit-code --tags "$remote" "refs/tags/${version}" 2>&1)"
tag_status=$?
set -e
case "$tag_status" in
  0)
    echo "release metadata: tag already exists on remote: ${version}" >&2
    exit 69
    ;;
  2)
    ;;
  *)
    echo "release metadata: unable to verify remote tag ${version}: ${tag_lookup}" >&2
    exit 70
    ;;
esac

printf 'target_sha=%s\n' "$target_sha"
