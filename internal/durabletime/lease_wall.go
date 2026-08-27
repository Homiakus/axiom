package durabletime

import "time"

func init() {
	Registry = append(Registry, ClockUsageEntry{
		Package:                "durabletime",
		File:                   "lease_wall.go",
		Function:               "LeaseWallNow",
		CallType:               "time.Now",
		Category:               CategoryLeaseFencing,
		RequiresClockInjection: false,
		Rationale:              "Operational worker lease/fencing time intentionally follows the persisted store wall-clock domain and is independent of semantic workflow Clock simulation.",
	})
}

// LeaseWallNow returns the UTC wall clock used for operational worker lease
// fencing. It is intentionally distinct from the semantic workflow Clock:
// deterministic simulation may advance semantic time arbitrarily, while
// process/store lease ownership must remain anchored to real elapsed time.
func LeaseWallNow() time.Time { return time.Now().UTC() }
