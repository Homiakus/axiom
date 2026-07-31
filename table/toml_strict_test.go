package table

import "testing"

func TestParseRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte(`[workflow]
name = "Strict"
unknown = true
`))
	if err == nil {
		t.Fatal("Parse() accepted an unknown TOML field")
	}
}
