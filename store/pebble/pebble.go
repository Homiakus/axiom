// Package pebble exposes the durable Pebble-backed Axiom store.
package pebble

import internal "github.com/Homiakus/axiom/internal/store/pebble"

type Store = internal.Store
type Option = internal.Option

var (
	Open          = internal.Open
	WithNoSync    = internal.WithNoSync
	WithSyncEvery = internal.WithSyncEvery
	WithJSONCodec = internal.WithJSONCodec
	WithGobCodec  = internal.WithGobCodec
)
