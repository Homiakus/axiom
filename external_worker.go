package axiom

import runtimepkg "github.com/Homiakus/axiom/internal/runtime"

// ExternalActivityToken is the fencing token for one leased AXM activity attempt.
type ExternalActivityToken = runtimepkg.ExternalActivityToken

// ExternalActivityClaim is a transport-safe leased AXM activity work item.
type ExternalActivityClaim = runtimepkg.ExternalActivityClaim

var (
	ErrExternalActivityClaimInvalid = runtimepkg.ErrExternalActivityClaimInvalid
	ErrExternalActivityClaimStale   = runtimepkg.ErrExternalActivityClaimStale
)
