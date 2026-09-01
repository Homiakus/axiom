#!/usr/bin/env bash
set -euo pipefail

workflow="${1:-.github/workflows/security.yml}"
runtime="${2:-adgo/runtime.go}"
g101_guard="${3:-scripts/verify_g101_false_positive.sh}"
g115_guard="${4:-scripts/verify_g115_internal_ids.sh}"
g301_guard="${5:-scripts/verify_g301_codegen_directory.sh}"
g306_guard="${6:-scripts/verify_g306_public_artifacts.sh}"

fail() {
  echo "[gosec-policy] $*" >&2
  exit 1
}

[[ -f "$workflow" ]] || fail "missing workflow: $workflow"
[[ -f "$runtime" ]] || fail "missing runtime source: $runtime"
[[ -f "$g101_guard" ]] || fail "missing G101 exception guard: $g101_guard"
[[ -f "$g115_guard" ]] || fail "missing G115 internal-ID guard: $g115_guard"
[[ -f "$g301_guard" ]] || fail "missing G301 codegen-directory guard: $g301_guard"
[[ -f "$g306_guard" ]] || fail "missing G306 public-artifact guard: $g306_guard"

for rule in G101 G104 G115 G301 G302 G306 G404; do
  if grep -Eq -- "-exclude=[^[:space:]]*${rule}" "$workflow"; then
    fail "${rule} must not be globally excluded"
  fi
done

expected_exceptions="-exclude-rules='adgo/runtime\\.go:G404;adgo/http_worker\\.go:G101;internal/runtime/fast_vm\\.go:G115;internal/runtime/expr_vm\\.go:G115;internal/runtime/engine\\.go:G115;internal/compiler/module\\.go:G115;cmd/axiomgen/internal/generate/generate\\.go:G301;cmd/axiombench/main\\.go:G306;cmd/axiomgen/internal/generate/generate\\.go:G306'"
grep -Fq -- "$expected_exceptions" "$workflow" || \
  fail "expected only reviewed path-scoped G404/G101/G115/G301/G306 exceptions"
grep -Fq -- 'run: bash scripts/verify_g101_false_positive.sh' "$workflow" || \
  fail "expected dedicated G101 false-positive verification step"
grep -Fq -- 'run: bash scripts/verify_g115_internal_ids.sh' "$workflow" || \
  fail "expected dedicated G115 internal-ID verification step"
grep -Fq -- 'run: bash scripts/verify_g301_codegen_directory.sh' "$workflow" || \
  fail "expected dedicated G301 codegen-directory verification step"
grep -Fq -- 'run: bash scripts/verify_g306_public_artifacts.sh' "$workflow" || \
  fail "expected dedicated G306 public-artifact verification step"
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

bash -n "$g101_guard" || fail "G101 exception guard has invalid shell syntax"
bash -n "$g115_guard" || fail "G115 internal-ID guard has invalid shell syntax"
bash -n "$g301_guard" || fail "G301 codegen-directory guard has invalid shell syntax"
bash -n "$g306_guard" || fail "G306 public-artifact guard has invalid shell syntax"

echo "gosec suppression policy: PASS"
