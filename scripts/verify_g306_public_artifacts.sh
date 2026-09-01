#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

fail() {
  echo "[g306-public-artifact-contract] $*" >&2
  exit 1
}

go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 \
  -exclude-dir=examples \
  -exclude-dir=internal/testutil \
  -include=G306 \
  -nosec-require-rules \
  -nosec-require-justification \
  -no-fail \
  -fmt=json \
  ./... >"$tmp"

python3 - "$tmp" <<'PY'
import collections
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)

issues = [issue for issue in data.get("Issues", []) if issue.get("rule_id") == "G306"]
expected = {
    "cmd/axiombench/main.go": 2,
    "cmd/axiomgen/internal/generate/generate.go": 2,
}

paths = []
for issue in issues:
    raw = str(issue.get("file", "")).replace("\\", "/")
    matched = next((path for path in expected if raw == path or raw.endswith("/" + path)), None)
    if matched is None:
        print(f"unexpected G306 path: {raw}", file=sys.stderr)
        sys.exit(1)
    paths.append(matched)

counts = collections.Counter(paths)
if len(issues) != 4 or dict(counts) != expected:
    print(f"G306 finding multiset changed: got {dict(counts)} / total={len(issues)}, want {expected} / total=4", file=sys.stderr)
    for issue in issues:
        print(f"  {issue.get('file')}:{issue.get('line')} {issue.get('details')}", file=sys.stderr)
    sys.exit(1)

print("gosec scoped G306 public-artifact contract: PASS")
PY

bench="cmd/axiombench/main.go"
gen="cmd/axiomgen/internal/generate/generate.go"

[[ "$(grep -Fc "os.WriteFile(jsonPath, append(data, '\\n'), 0o644)" "$bench")" -eq 1 ]] || \
  fail "benchmark JSON report permission contract changed"
[[ "$(grep -Fc 'return os.WriteFile(markdownPath, []byte(renderMarkdown(value)), 0o644)' "$bench")" -eq 1 ]] || \
  fail "benchmark Markdown report permission contract changed"
[[ "$(grep -Fc 'os.WriteFile(target, file.Content, 0o644)' "$gen")" -eq 1 ]] || \
  fail "generated Go source permission contract changed"
[[ "$(grep -Fc 'return false, os.WriteFile(target, newStubs, 0o644)' "$gen")" -eq 1 ]] || \
  fail "generated activity-stub overwrite permission contract changed"
