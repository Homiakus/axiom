// Package pebble exposes the durable Pebble-backed Axiom store.
//
// # Persisted Format & Codecs
//
// By default, Open uses JSON encoding (WithJSONCodec). Gob encoding is available
// as an opt-in alternative via WithGobCodec.
//
// When a store is opened, an explicit format marker (schema version "1" and the
// active codec) is recorded in store metadata. If an existing store is reopened
// with a conflicting codec or an unsupported schema version, Open fails fast.
// Legacy unmarked stores are automatically detected and adopted on open.
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
