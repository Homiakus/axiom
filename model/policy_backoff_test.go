package model

import (
	"strings"
	"testing"
	"time"
)

func TestPolicyBackoffRendersFixedDuration(t *testing.T) {
	definition := New("FixedRetry")
	definition.Policy("resilient").Retry(2).Backoff(250 * time.Millisecond)

	source := definition.Source()
	if !strings.Contains(source, "backoff: 250ms") {
		t.Fatalf("Source() = %q, want fixed backoff", source)
	}
	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}

func TestPolicyExponentialBackoffRendersCall(t *testing.T) {
	definition := New("ExponentialRetry")
	definition.Policy("resilient").Retry(3).ExponentialBackoff(100 * time.Millisecond)

	source := definition.Source()
	if !strings.Contains(source, "backoff: exponential(100ms)") {
		t.Fatalf("Source() = %q, want exponential backoff", source)
	}
	if _, err := definition.Compile(); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}
