#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

fail() {
  echo "[g304-path-contract] $*" >&2
  exit 1
}

go run github.com/securego/gosec/v2/cmd/gosec@v2.28.0 \
  -exclude-dir=examples \
  -exclude-dir=internal/testutil \
  -include=G304 \
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

issues = [issue for issue in data.get("Issues", []) if issue.get("rule_id") == "G304"]
expected = {
    "table/toml.go": 1,
    "cmd/qualityloop/main.go": 2,
    "cmd/axiomgen/internal/generate/generate.go": 3,
    "axm/axm.go": 1,
    "axiom.go": 1,
    "adgo/store.go": 2,
    "adgo/schedule.go": 1,
    "adgo/router_store.go": 1,
    "adgo/replay.go": 1,
    "adgo/file_lock.go": 2,
    "adgo/catalog.go": 1,
    "adgo/cache.go": 1,
}

paths = []
for issue in issues:
    raw = str(issue.get("file", "")).replace("\\", "/")
    matched = next((path for path in expected if raw == path or raw.endswith("/" + path)), None)
    if matched is None:
        print(f"unexpected G304 path: {raw}", file=sys.stderr)
        sys.exit(1)
    paths.append(matched)

counts = collections.Counter(paths)
if len(issues) != 17 or dict(counts) != expected:
    print(
        f"G304 finding multiset changed: got {dict(counts)} / total={len(issues)}, "
        f"want {expected} / total=17",
        file=sys.stderr,
    )
    for issue in issues:
        print(f"  {issue.get('file')}:{issue.get('line')} {issue.get('details')}", file=sys.stderr)
    sys.exit(1)

print("gosec scoped G304 finding multiset: PASS")
PY

# Caller-selected paths are intentional API/CLI contracts. The exact calls are
# pinned so a broad file exception cannot silently cover a second path source.
grep -Fq 'source, err := os.ReadFile(path)' axiom.go || fail "axiom.Load path contract changed"
grep -Fq 'data, err := os.ReadFile(path)' axm/axm.go || fail "axm.Load path contract changed"
grep -Fq 'data, err := os.ReadFile(path)' table/toml.go || fail "table.Load path contract changed"
[[ "$(grep -Fc 'f, err := os.Open(path)' cmd/qualityloop/main.go)" -eq 1 ]] || fail "qualityloop plan path contract changed"
[[ "$(grep -Fc 'data, err := os.ReadFile(path)' cmd/qualityloop/main.go)" -eq 1 ]] || fail "qualityloop edge-space path contract changed"
grep -Fq 'data, err := os.ReadFile(genPath)' cmd/axiomgen/internal/generate/generate.go || fail "axiomgen generated-source read contract changed"
grep -Fq 'existing, err := os.ReadFile(target)' cmd/axiomgen/internal/generate/generate.go || fail "axiomgen merge read contract changed"
grep -Fq 'os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0)' cmd/axiomgen/internal/generate/generate.go || fail "axiomgen append contract changed"

# Internal durable paths are derived from private roots plus encoded/hashed or
# enumerated leaf names. Keep the concrete source/sink shapes pinned.
[[ "$(grep -Fc 'os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)' adgo/store.go)" -eq 1 ]] || fail "file-store lock sink changed"
[[ "$(grep -Fc 'f, err := os.Open(dir)' adgo/store.go)" -eq 1 ]] || fail "syncDir sink changed"
[[ "$(grep -Fc 'os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)' adgo/schedule.go)" -eq 1 ]] || fail "schedule lock sink changed"
[[ "$(grep -Fc 'os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)' adgo/router_store.go)" -eq 1 ]] || fail "provider-health lock sink changed"
[[ "$(grep -Fc 'os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)' adgo/cache.go)" -eq 1 ]] || fail "cache lock sink changed"
[[ "$(grep -Fc 'data, err := os.ReadFile(path)' adgo/file_lock.go)" -eq 1 ]] || fail "file-lock read sink changed"
[[ "$(grep -Fc 'os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, privateLockFileMode)' adgo/file_lock.go)" -eq 1 ]] || fail "file-lock create sink changed"
grep -Fq 'os.ReadFile(filepath.Join(s.commitsDir(id), name))' adgo/replay.go || fail "replay commit read contract changed"
grep -Fq 'os.ReadFile(filepath.Join(commits, names[len(names)-1]))' adgo/catalog.go || fail "catalog commit read contract changed"

# The former real G304 defect must stay fixed rather than joining the exception set.
grep -Fq 'decoded, err := hex.DecodeString(digest)' adgo/artifact.go || fail "artifact digest must be decoded as hex"
grep -Fq 'strings.ToLower(digest) != digest' adgo/artifact.go || fail "artifact digest must remain canonical lowercase"
grep -Fq 'os.OpenInRoot(filepath.Join(s.root, "sha256"), rel)' adgo/artifact.go || fail "artifact Open must stay rooted"
grep -Fq 'os.OpenRoot(filepath.Join(s.root, "sha256"))' adgo/artifact.go || fail "artifact Exists must stay rooted"
if grep -Fq 'return os.Open(path)' adgo/artifact.go; then
  fail "artifact Open regressed to an unscoped path"
fi
grep -Fq 'func TestContentAddressedStoreRejectsNonCanonicalDigest' adgo/artifact_security_test.go || fail "missing non-canonical digest regression"
grep -Fq 'func TestContentAddressedStoreCanonicalDigestRoundTrip' adgo/artifact_security_test.go || fail "missing canonical round-trip regression"
grep -Fq 'func TestContentAddressedStoreRejectsSymlinkEscape' adgo/artifact_security_test.go || fail "missing artifact symlink escape regression"

echo "gosec scoped G304 path contract: PASS"
