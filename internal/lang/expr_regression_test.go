package lang

import "testing"

func TestParseExprRejectsTruncatedMapWithoutPanic(t *testing.T) {
	for _, source := range []string{
		"{",
		"{key:",
		"{key: 1,",
	} {
		t.Run(source, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("ParseExpr(%q) panicked: %v", source, recovered)
				}
			}()
			if _, err := ParseExpr(source); err == nil {
				t.Fatalf("ParseExpr(%q) succeeded; want parse error", source)
			}
		})
	}
}
