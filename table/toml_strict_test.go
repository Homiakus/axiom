package table

import (
	"strings"
	"testing"
)

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`[workflow]
name = "Strict"
unknown = true
`))
	if err == nil {
		t.Fatal("Parse() accepted an unknown TOML field")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("Parse() error = %v, want unknown-field diagnostic", err)
	}
}
