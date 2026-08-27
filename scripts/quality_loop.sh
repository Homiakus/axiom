#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
mkdir -p test-artifacts

mode="${1:-fast}"

plan_contract() {
  go run ./cmd/qualityloop -mode validate
  go run ./cmd/qualityloop -mode next -json | tee test-artifacts/quality-next-task.json
}

sentinels() {
  go test -shuffle=on -count=5 ./adgo ./internal/store/... -run '(Identity|Execution|Prefix|Path|Catalog|Reopen|Legacy|Schema|Codec)'
  go test -shuffle=on -count=5 ./... -run '(NaN|Inf|Float|Budget|Overflow|Numeric|RoundTrip|Clone|Commit)'
  go test -race -shuffle=on -count=5 ./adgo ./internal/runtime/... -run '(Clock|Deadline|Retry|Lease|Schedule|Admission|Cancel|Heartbeat|Stale)'
  go test -shuffle=on -count=5 ./... -run '(Crash|Recovery|Failpoint|Outbox|Durable|Reopen|Rollback|Migration|Corrupt)'
  go test -race -shuffle=on -count=5 ./adgo ./internal/runtime/... ./internal/store/... -run '(Concurrent|Conflict|Order|Determin|Tie|Race|CAS|Owner)'
}

fast() {
  plan_contract
  go test -shuffle=on -count=3 ./adgo ./internal/runtime/... ./internal/store/...
  sentinels
}

deep() {
  fast
  go test -shuffle=on -count=5 ./...
  go test -race -shuffle=on -count=3 ./adgo ./internal/runtime/... ./internal/store/...
}

mutation_diff() {
  local ref="${QUALITY_MUTATION_BASE:-HEAD^}"
  if ! command -v gremlins >/dev/null 2>&1; then
    echo "gremlins is required for mutation-diff (pinned CI version: v0.6.0)" >&2
    exit 2
  fi
  if git diff --quiet "$ref"...HEAD -- '*.go'; then
    echo "No Go changes relative to $ref; mutation diff skipped."
    return 0
  fi
  gremlins unleash \
    --diff "$ref" \
    --output test-artifacts/mutation-diff.json \
    --threshold-efficacy "${QUALITY_MUTATION_EFFICACY:-80}" \
    --threshold-mcover "${QUALITY_MUTATION_COVERAGE:-70}"
}

autofix() {
  # Controlled auto-fix deliberately permits only semantics-preserving formatting.
  # Logic repairs remain agent-controlled and must follow docs/QUALITY_LOOP.md.
  mapfile -t files < <(git diff --name-only --diff-filter=ACMR HEAD -- '*.go')
  if ((${#files[@]} == 0)); then
    echo "No changed Go files to format."
    return 0
  fi
  printf 'Formatting %d changed Go file(s):\n' "${#files[@]}"
  printf '  %s\n' "${files[@]}"
  gofmt -w -- "${files[@]}"
  go test ${QUALITY_PACKAGES:-./...}
}

case "$mode" in
  plan) plan_contract ;;
  sentinels) sentinels ;;
  fast) fast ;;
  deep) deep ;;
  mutation-diff) mutation_diff ;;
  autofix) autofix ;;
  *)
    echo "usage: $0 {plan|sentinels|fast|deep|mutation-diff|autofix}" >&2
    exit 2
    ;;
esac
