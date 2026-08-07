package axiom

import runtimepkg "github.com/Homiakus/axiom/internal/runtime"

// ActivityErrorCoder can be implemented by custom application errors that
// expose a stable domain code used by policy catch mappings.
type ActivityErrorCoder = runtimepkg.ActivityErrorCoder

// CodedActivityError is the default coded error returned by FailActivity.
type CodedActivityError = runtimepkg.CodedActivityError

// FailActivity wraps an activity failure with a stable domain code.
//
// Example:
//
//	return nil, axiom.FailActivity("PaymentDeclined", err)
//
// A policy catch mapping such as `PaymentDeclined -> PaymentDeclinedReceived`
// is evaluated only after the activity retry budget is exhausted.
func FailActivity(code string, err error) error {
	return runtimepkg.NewCodedActivityError(code, err)
}
