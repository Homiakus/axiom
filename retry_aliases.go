package axiom

import runtimepkg "github.com/Homiakus/axiom/internal/runtime"

// RetryScheduledError describes a durable activity retry checkpoint returned by
// the low-level Engine.RunUntilIdle API. The higher-level Run API handles this
// condition automatically.
type RetryScheduledError = runtimepkg.RetryScheduledError

// ErrRetryScheduled can be matched with errors.Is when low-level callers need
// to distinguish deferred retry work from terminal activity failure.
var ErrRetryScheduled = runtimepkg.ErrRetryScheduled
