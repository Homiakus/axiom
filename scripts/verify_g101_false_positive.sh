#!/usr/bin/env bash
set -euo pipefail

target="${1:-./adgo}"

fail() {
  echo "[gosec-g101-exception] $*" >&2
  exit 1
}

if [[ -n "${GOSEC_BIN:-}" ]]; then
  gosec_cmd=("$GOSEC_BIN")
else
  gosec_cmd=(go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0)
fi

set +e
output="$("${gosec_cmd[@]}" -include=G101 -fmt=text "$target" 2>&1)"
status=$?
set -e

[[ "$status" -eq 1 ]] || fail "expected exactly the reviewed G101 false positive; scanner exit status was $status"

mapfile -t findings < <(printf '%s\n' "$output" | grep -E '\] - G101 \(CWE-798\): Potential hardcoded credentials' || true)
[[ "${#findings[@]}" -eq 1 ]] || {
  printf '%s\n' "$output" >&2
  fail "expected exactly one G101 finding, got ${#findings[@]}"
}

printf '%s\n' "$output" | grep -Eq 'adgo/http_worker\.go:[0-9]+\] - G101 ' || {
  printf '%s\n' "$output" >&2
  fail "the only allowed G101 finding must be in adgo/http_worker.go"
}
printf '%s\n' "$output" | grep -Fq 'HTTPWorkerProtocolVersion' || {
  printf '%s\n' "$output" >&2
  fail "the allowed G101 finding must reference HTTPWorkerProtocolVersion"
}

echo "gosec scoped G101 false-positive contract: PASS"
