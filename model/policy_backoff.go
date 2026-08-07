package model

import "time"

// Backoff configures a fixed delay between durable activity attempts.
func (p *PolicyBuilder) Backoff(value time.Duration) *PolicyBuilder {
	p.entry("backoff", Lit(value))
	return p
}

// ExponentialBackoff configures deterministic exponential retry delay using
// value as the base duration. Runtime delay is capped to prevent unbounded waits.
func (p *PolicyBuilder) ExponentialBackoff(value time.Duration) *PolicyBuilder {
	p.entry("backoff", Raw("exponential("+value.String()+")"))
	return p
}
