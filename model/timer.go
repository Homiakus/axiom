package model

import "time"

// OnTimerAt creates a wall-clock trigger for an absolute time expression.
// Prefer this over stringly-typed OnTimer when the deadline is a model field.
func OnTimerAt(at Expr) Trigger {
	return OnTimer(at.String())
}

// OnTimerAfter creates a wall-clock trigger that fires delay after a time
// expression. The duration is rendered using Go's canonical duration syntax.
func OnTimerAfter(delay time.Duration, after Expr) Trigger {
	return OnTimer(delay.String() + " after " + after.String())
}
