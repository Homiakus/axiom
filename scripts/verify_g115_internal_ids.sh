#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

fail() {
  echo "[g115-internal-id-contract] $*" >&2
  exit 1
}

go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 \
  -exclude-dir=examples \
  -exclude-dir=internal/testutil \
  -include=G115 \
  -nosec-require-rules \
  -nosec-require-justification \
  -no-fail \
  -fmt=json \
  ./... >"$tmp"

python3 - "$tmp" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    data = json.load(fh)

expected = {
    "internal/runtime/fast_vm.go": 5,
    "internal/runtime/expr_vm.go": 4,
    "internal/runtime/engine.go": 3,
    "internal/compiler/module.go": 4,
}
counts = {key: 0 for key in expected}
unexpected = []
issues = [issue for issue in data.get("Issues", []) if issue.get("rule_id") == "G115"]

for issue in issues:
    raw = str(issue.get("file", "")).replace("\\", "/")
    matched = next((name for name in expected if raw == name or raw.endswith("/" + name)), None)
    if matched is None:
        unexpected.append((raw, issue.get("line"), issue.get("details")))
        continue
    counts[matched] += 1

if unexpected:
    print("unexpected G115 findings:", file=sys.stderr)
    for item in unexpected:
        print(f"  {item[0]}:{item[1]} {item[2]}", file=sys.stderr)
    sys.exit(1)

if counts != expected:
    print(f"G115 finding multiset changed: got {counts}, want {expected}", file=sys.stderr)
    sys.exit(1)

if len(issues) != sum(expected.values()):
    print(f"G115 finding count changed: got {len(issues)}, want {sum(expected.values())}", file=sys.stderr)
    sys.exit(1)

print("gosec scoped G115 internal-ID contract: PASS")
PY

[[ "$(grep -Fc 'uint32(atom.id)' internal/runtime/fast_vm.go)" -eq 5 ]] || \
  fail "fast_vm.go approved atom-ID conversions changed"
[[ "$(grep -Fc 'uint32(id)' internal/runtime/expr_vm.go)" -eq 3 ]] || \
  fail "expr_vm.go approved generic-ID conversions changed"
[[ "$(grep -Fc 'uint32(fieldID)' internal/runtime/expr_vm.go)" -eq 1 ]] || \
  fail "expr_vm.go approved field-ID conversion changed"
[[ "$(grep -Fc 'uint32(fieldID)' internal/runtime/engine.go)" -eq 3 ]] || \
  fail "engine.go approved field-ID conversions changed"

for pattern in \
  'FieldID(len(ids.Fields))' \
  'SignalID(len(ids.Signals))' \
  'RuleID(len(ids.Rules))' \
  'ActivityID(len(ids.Activities))'
do
  [[ "$(grep -Fc "$pattern" internal/compiler/module.go)" -eq 1 ]] || \
    fail "compiler approved ID conversion changed: $pattern"
done
