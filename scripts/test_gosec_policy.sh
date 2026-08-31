#!/usr/bin/env bash
set -euo pipefail

workflow="${1:-.github/workflows/security.yml}"
runtime="${2:-adgo/runtime.go}"

fail() {
  echo "[gosec-policy] $*" >&2
  exit 1
}

[[ -f "$workflow" ]] || fail "missing workflow: $workflow"
[[ -f "$runtime" ]] || fail "missing runtime source: $runtime"

for rule in G101 G404; do
  if grep -Eq -- "-exclude=[^[:space:]]*${rule}" "$workflow"; then
    fail "${rule} must not be globally excluded"
  fi
done

grep -Fq -- "-exclude-rules='adgo/runtime\\.go:G404'" "$workflow" || \
  fail "expected path-scoped G404 exception for adgo/runtime.go"
grep -Fq -- '-nosec-require-rules' "$workflow" || \
  fail "gosec must require rule IDs for inline suppressions"
grep -Fq -- '-nosec-require-justification' "$workflow" || \
  fail "gosec must require justifications for inline suppressions"

[[ "$(grep -Fc '"math/rand"' "$runtime")" -eq 1 ]] || \
  fail "adgo/runtime.go must have exactly one math/rand import"
grep -Fq 'h := sha256.Sum256([]byte(seed + fmt.Sprint(attempt)))' "$runtime" || \
  fail "approved deterministic retry-jitter seed changed; re-review G404 exception"
grep -Fq 'src := rand.New(rand.NewSource(' "$runtime" || \
  fail "approved deterministic retry-jitter RNG construction changed; re-review G404 exception"

mapfile -t rng_refs < <(grep -oE 'rand\.[A-Za-z][A-Za-z0-9_]*' "$runtime" || true)
if [[ "${#rng_refs[@]}" -ne 2 ]]; then
  fail "adgo/runtime.go gained or lost math/rand API calls; expected exactly 2, got ${#rng_refs[@]}"
fi
mapfile -t sorted_refs < <(printf '%s\n' "${rng_refs[@]}" | sort)
if [[ "${sorted_refs[0]}" != 'rand.New' || "${sorted_refs[1]}" != 'rand.NewSource' ]]; then
  fail "unexpected math/rand APIs in adgo/runtime.go: ${sorted_refs[*]}"
fi

echo "gosec suppression policy: PASS"
