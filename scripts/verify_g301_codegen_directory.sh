#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

fail() {
  echo "[g301-codegen-directory-contract] $*" >&2
  exit 1
}

go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 \
  -exclude-dir=examples \
  -exclude-dir=internal/testutil \
  -include=G301 \
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

issues = [issue for issue in data.get("Issues", []) if issue.get("rule_id") == "G301"]
if len(issues) != 1:
    print(f"G301 finding count changed: got {len(issues)}, want 1", file=sys.stderr)
    for issue in issues:
        print(f"  {issue.get('file')}:{issue.get('line')} {issue.get('details')}", file=sys.stderr)
    sys.exit(1)

issue = issues[0]
raw = str(issue.get("file", "")).replace("\\", "/")
expected = "cmd/axiomgen/internal/generate/generate.go"
if raw != expected and not raw.endswith("/" + expected):
    print(f"unexpected G301 path: {raw}; want {expected}", file=sys.stderr)
    sys.exit(1)

print("gosec scoped G301 codegen-directory contract: PASS")
PY

source="cmd/axiomgen/internal/generate/generate.go"
[[ "$(grep -Fc 'os.MkdirAll(plan.OutDir, 0o755)' "$source")" -eq 1 ]] || \
  fail "approved shareable codegen output-directory creation changed"
