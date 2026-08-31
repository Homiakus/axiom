#!/usr/bin/env bash
set -euo pipefail

release_workflow="${1:-.github/workflows/release.yml}"
ci_workflow="${2:-.github/workflows/ci.yml}"
security_workflow="${3:-.github/workflows/security.yml}"

fail() {
  echo "release workflow contract: FAIL: $*" >&2
  exit 1
}

require() {
  local file="$1" pattern="$2" message="$3"
  grep -F -- "$pattern" "$file" >/dev/null || fail "$message"
}

forbid() {
  local file="$1" pattern="$2" message="$3"
  if grep -F -- "$pattern" "$file" >/dev/null; then
    fail "$message"
  fi
}

[[ -f "$release_workflow" ]] || fail "missing release workflow: $release_workflow"
[[ -f "$ci_workflow" ]] || fail "missing CI workflow: $ci_workflow"
[[ -f "$security_workflow" ]] || fail "missing security workflow: $security_workflow"

if grep -Eq '^[[:space:]]{2}push:$' "$release_workflow"; then
  fail "release publication must have a single workflow_dispatch entrypoint"
fi
require "$release_workflow" "workflow_dispatch:" "release publication must use workflow_dispatch"
require "$ci_workflow" "workflow_call:" "CI workflow must be reusable"
require "$security_workflow" "workflow_call:" "security workflow must be reusable"
require "$release_workflow" "uses: ./.github/workflows/ci.yml" "release must reuse the normal CI DAG"
require "$release_workflow" "uses: ./.github/workflows/security.yml" "release must reuse the security DAG"
require "$release_workflow" 'docs/releases/${VERSION}.md' "release notes must be required from the frozen candidate"
require "$release_workflow" 'releases/tags/${ver}' "existing releases must be checked before publication"
require "$release_workflow" 'gh release create "$VERSION"' "release publication must create exactly one release"
require "$release_workflow" '--target "$TARGET_SHA"' "release creation must explicitly target the frozen SHA"
require "$release_workflow" 'resolved_tag_sha=' "published tag must be resolved after creation"
require "$release_workflow" 'if [[ "$resolved_tag_sha" != "$TARGET_SHA" ]]' "published tag SHA must be compared with frozen SHA"
forbid "$release_workflow" '--generate-notes' "generated release notes are forbidden by release policy"
forbid "$release_workflow" 'gh release upload' "implicit create-or-upload recovery is forbidden"
forbid "$release_workflow" '--clobber' "release assets must not be silently overwritten"

# Every checkout in reusable verification workflows must honor the requested
# candidate ref. This makes a future new job fail the contract test if it
# accidentally verifies the caller HEAD instead of the frozen release SHA.
check_checkout_refs() {
  local file="$1"
  awk '
    /uses: actions\/checkout@/ {
      found = 0
      for (i = 0; i < 8; i++) {
        if ((getline line) <= 0) break
        if (line ~ /ref:.*inputs\.ref.*github\.sha/) { found = 1; break }
        if (line ~ /^[[:space:]]*-[[:space:]]+name:/) break
      }
      if (!found) exit 1
    }
  ' "$file" || fail "every checkout in $file must select inputs.ref or github.sha"
}

check_checkout_refs "$ci_workflow"
check_checkout_refs "$security_workflow"

echo "release workflow contract: PASS"
