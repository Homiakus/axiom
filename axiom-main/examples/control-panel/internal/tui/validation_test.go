package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateFileSuccessSummary(t *testing.T) {
	path := writeAXM(t, validSource)

	report := ValidateFile(path)

	if !report.OK() {
		t.Fatalf("ValidateFile() failed: diagnostics=%v raw=%q", report.Diagnostics, report.RawError)
	}
	if report.Path == "" || report.FileName != filepath.Base(path) {
		t.Fatalf("file metadata not populated: %#v", report)
	}
	if report.Size == 0 || report.Duration < 0 {
		t.Fatalf("timing/size not populated: size=%d duration=%s", report.Size, report.Duration)
	}
	if report.Summary.Domain != "ValidationOK" {
		t.Fatalf("domain = %q", report.Summary.Domain)
	}
	if report.Summary.Signals != 1 || report.Summary.Contexts != 1 || report.Summary.Computeds != 1 ||
		report.Summary.Facts != 1 || report.Summary.Activities != 1 || report.Summary.Rules != 1 ||
		report.Summary.Claims != 1 || report.Summary.Queries != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}
	for _, stage := range report.Stages {
		if stage.Status != StageOK {
			t.Fatalf("stage %q = %s", stage.Name, stage.Status)
		}
	}
}

func TestValidateFileMissingFile(t *testing.T) {
	report := ValidateFile(filepath.Join(t.TempDir(), "missing.axm"))

	if report.OK() {
		t.Fatalf("missing file unexpectedly succeeded")
	}
	if report.RawError == "" {
		t.Fatalf("missing file did not report raw error")
	}
	if report.Stages[0].Status != StageError {
		t.Fatalf("read stage = %s", report.Stages[0].Status)
	}
}

func TestValidateFileUnreadablePath(t *testing.T) {
	report := ValidateFile(t.TempDir())

	if report.OK() {
		t.Fatalf("directory path unexpectedly succeeded")
	}
	if report.RawError == "" {
		t.Fatalf("directory path did not report raw error")
	}
	if report.Stages[0].Status != StageError {
		t.Fatalf("read stage = %s", report.Stages[0].Status)
	}
}

func TestValidateFileSyntaxErrorIsRawError(t *testing.T) {
	path := writeAXM(t, "domain Bad\n  signal Nope\n")

	report := ValidateFile(path)

	if report.OK() {
		t.Fatalf("syntax error unexpectedly succeeded")
	}
	if report.RawError != "" {
		t.Fatalf("syntax error should be structured, raw error = %q", report.RawError)
	}
	if len(report.Diagnostics) != 1 {
		t.Fatalf("syntax error diagnostics = %#v", report.Diagnostics)
	}
	if report.Diagnostics[0].Code != "AX000" || !strings.Contains(report.Diagnostics[0].Message, "expected top-level declaration") {
		t.Fatalf("syntax diagnostic = %#v", report.Diagnostics[0])
	}
}

func TestValidateFileDiagnostics(t *testing.T) {
	path := writeAXM(t, `
domain Invalid

context User:
  id: String?

rule broken:
  on MissingSignal
  write:
    User.name = MissingValue
`)

	report := ValidateFile(path)

	if report.OK() {
		t.Fatalf("diagnostic source unexpectedly succeeded")
	}
	if len(report.Diagnostics) == 0 {
		t.Fatalf("expected structured diagnostics, raw=%q", report.RawError)
	}
	if report.RawError != "" {
		t.Fatalf("structured diagnostics should not be raw error: %q", report.RawError)
	}
}

func writeAXM(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.axm")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

const validSource = `
domain ValidationOK

signal Ping:
  id: String

context User:
  id: String?
  seen: Bool = false

computed hasUser: Bool =
  User.id exists

fact KnownUser when:
  hasUser
expose:
  id = User.id

policy localPolicy:
  retry: 0
  timeout: 1s
  concurrency: once
  idempotency: none

activity CheckLocal:
  output:
    ok: Bool
  effect: none
  policy: localPolicy

rule capture:
  on Ping
  write:
    User.id = signal.id
    User.seen = true

claim seenRequiresID:
  always:
    User.seen == false or User.id exists

query userView:
  return:
    id = User.id
    seen = User.seen
`
