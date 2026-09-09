// Package postgres exposes the durable PostgreSQL-backed Axiom store for multi-host deployments.
//
// # Multi-Host Concurrency & Fencing
//
// The PostgreSQL store supports multi-host cluster deployments where multiple coordinators
// and workers operate concurrently against a shared database:
//   - Optimistic concurrency control (CAS) serializes mutations per execution.
//   - Row-level locks (SELECT ... FOR UPDATE) guard atomic commit transitions.
//   - Task polling uses PostgreSQL's FOR UPDATE SKIP LOCKED to prevent lock convoying and worker contention.
//   - Lease heartbeat and fencing guarantee that stale workers cannot commit results after lease expiration.
//   - Automatic forward migrations are coordinated across hosts using PostgreSQL advisory locks.
package postgres

import internal "github.com/Homiakus/axiom/internal/store/postgres"

type Store = internal.Store
type Option = internal.Option

var (
	Open            = internal.Open
	OpenFromDB      = internal.OpenFromDB
	WithLeaseTTL    = internal.WithLeaseTTL
	WithLeaseOwner  = internal.WithLeaseOwner
	WithAutoMigrate = internal.WithAutoMigrate
	Migrate         = internal.Migrate
)
