#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

fail() {
  echo "[g703-path-provenance-contract] $*" >&2
  exit 1
}

go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 \
  -exclude-dir=examples \
  -exclude-dir=internal/testutil \
  -include=G703 \
  -nosec=true \
  -no-fail \
  -fmt=json \
  ./... >"$tmp"

python3 - "$tmp" <<'PY'
import collections
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = json.load(fh)

issues = [issue for issue in data.get("Issues", []) if issue.get("rule_id") == "G703"]
expected = {
    "adgo/catalog.go": 1,
    "adgo/file_lock.go": 5,
    "adgo/file_lock_heartbeat.go": 1,
    "adgo/store.go": 7,
}

paths = []
for issue in issues:
    raw = str(issue.get("file", "")).replace("\\", "/")
    matched = next((path for path in expected if raw == path or raw.endswith("/" + path)), None)
    if matched is None:
        print(f"unexpected G703 path: {raw}", file=sys.stderr)
        sys.exit(1)
    paths.append(matched)

counts = collections.Counter(paths)
if len(issues) != 14 or dict(counts) != expected:
    print(
        f"G703 finding multiset changed: got {dict(counts)} / total={len(issues)}, "
        f"want {expected} / total=14",
        file=sys.stderr,
    )
    for issue in issues:
        print(f"  {issue.get('file')}:{issue.get('line')} {issue.get('details')}", file=sys.stderr)
    sys.exit(1)

print("gosec scoped G703 finding multiset: PASS")
PY

[[ "$(grep -Fc 'f.Type()&os.ModeSymlink != 0' adgo/catalog.go)" -eq 1 ]] || \
  fail "execution catalog must reject symlink commit records"
[[ "$(grep -Fc 'ent.Type()&os.ModeSymlink != 0' adgo/store.go)" -eq 2 ]] || \
  fail "file store must reject symlink commit and inbox records"
grep -Fq 'filepath.Base(filename) != filename || strings.ContainsAny(filename, `/\\`)' adgo/file_lock.go || \
  fail "owned lock helper must reject non-leaf lock filenames"
grep -Fq 'if !IsContainedPath(locksDir, path) {' adgo/store.go || \
  fail "execution lock path must be checked against the private lock root"
grep -Fq 'func TestWithOwnedFileLockRejectsTraversalFilename' adgo/path_security_test.go || \
  fail "missing lock traversal regression test"
grep -Fq 'func TestFileStoreIgnoresSymlinkedCommitAndInboxRecords' adgo/path_security_test.go || \
  fail "missing symlink state-record regression test"
grep -Fq 'func TestFileStoreExecutionLockContainsTraversalLikeID' adgo/path_security_test.go || \
  fail "missing encoded execution-lock containment regression test"

echo "gosec scoped G703 path-provenance contract: PASS"
