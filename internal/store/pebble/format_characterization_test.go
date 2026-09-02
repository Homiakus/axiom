package pebble

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Homiakus/axiom/internal/runtime"
	pebbledb "github.com/cockroachdb/pebble"
)

// TestCoreFormatMarkerCharacterization absent, valid, partial, wrong expected value,
// and reopen boundaries for Core Pebble stores.
func TestCoreFormatMarkerCharacterization(t *testing.T) {
	ctx := context.Background()

	t.Run("absent marker in empty store writes matching markers synchronously", func(t *testing.T) {
		path := t.TempDir()
		store, err := Open(path, WithJSONCodec())
		if err != nil {
			t.Fatalf("Open(empty store) error = %v", err)
		}
		defer store.Close()

		schema, schemaFound, err := store.getRawMetadata(storeSchemaKey)
		if err != nil || !schemaFound || string(schema) != storeSchemaVersion {
			t.Fatalf("schema marker = %q (found=%v, err=%v), want %q", schema, schemaFound, err, storeSchemaVersion)
		}
		codec, codecFound, err := store.getRawMetadata(storeCodecKey)
		if err != nil || !codecFound || string(codec) != string(codecJSON) {
			t.Fatalf("codec marker = %q (found=%v, err=%v), want %q", codec, codecFound, err, string(codecJSON))
		}
	})

	t.Run("absent marker in Gob store writes Gob marker", func(t *testing.T) {
		path := t.TempDir()
		store, err := Open(path, WithGobCodec())
		if err != nil {
			t.Fatalf("Open(Gob store) error = %v", err)
		}
		defer store.Close()

		codec, codecFound, err := store.getRawMetadata(storeCodecKey)
		if err != nil || !codecFound || string(codec) != string(codecGob) {
			t.Fatalf("codec marker = %q (found=%v, err=%v), want %q", codec, codecFound, err, string(codecGob))
		}
	})

	t.Run("valid matching marker allows reopen and data retrieval", func(t *testing.T) {
		path := t.TempDir()
		store, err := Open(path, WithJSONCodec())
		if err != nil {
			t.Fatalf("first Open failed: %v", err)
		}
		exec := &runtime.Execution{ID: "valid-exec", Version: 1}
		if err := store.CreateExecution(ctx, exec); err != nil {
			t.Fatalf("CreateExecution failed: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := Open(path, WithJSONCodec())
		if err != nil {
			t.Fatalf("reopen with matching codec failed: %v", err)
		}
		defer reopened.Close()

		got, err := reopened.GetExecution(ctx, "valid-exec")
		if err != nil {
			t.Fatalf("GetExecution after valid reopen failed: %v", err)
		}
		if got.ID != "valid-exec" || got.Version != 1 {
			t.Fatalf("retrieved execution = %+v, want ID=valid-exec Version=1", got)
		}
	})

	t.Run("partial marker with schema only fails closed", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set(storeSchemaKey, []byte(storeSchemaVersion), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = Open(path)
		if err == nil || !strings.Contains(err.Error(), "incomplete persisted format marker") {
			t.Fatalf("Open(partial schema-only) err = %v, want incomplete marker error", err)
		}
	})

	t.Run("partial marker with codec only fails closed", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set(storeCodecKey, []byte(codecJSON), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = Open(path)
		if err == nil || !strings.Contains(err.Error(), "incomplete persisted format marker") {
			t.Fatalf("Open(partial codec-only) err = %v, want incomplete marker error", err)
		}
	})

	t.Run("unsupported future schema versions fail closed", func(t *testing.T) {
		for _, futureSchema := range []string{"2", "99", "v1.0", "beta"} {
			path := t.TempDir()
			db, err := pebbledb.Open(path, &pebbledb.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Set(storeSchemaKey, []byte(futureSchema), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			if err := db.Set(storeCodecKey, []byte(codecJSON), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			_ = db.Close()

			_, err = Open(path)
			if err == nil || !strings.Contains(err.Error(), "unsupported store schema") {
				t.Fatalf("Open(schema=%q) err = %v, want unsupported store schema error", futureSchema, err)
			}
		}
	})

	t.Run("unsupported unknown codec fails closed", func(t *testing.T) {
		for _, badCodec := range []string{"proto", "yaml", "msgpack", "cbor", ""} {
			path := t.TempDir()
			db, err := pebbledb.Open(path, &pebbledb.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Set(storeSchemaKey, []byte(storeSchemaVersion), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			if err := db.Set(storeCodecKey, []byte(badCodec), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			_ = db.Close()

			_, err = Open(path)
			if err == nil || !strings.Contains(err.Error(), "unsupported persisted codec") {
				t.Fatalf("Open(codec=%q) err = %v, want unsupported persisted codec error", badCodec, err)
			}
		}
	})

	t.Run("reopen with mismatched codec fails closed without overwriting stored marker", func(t *testing.T) {
		path := t.TempDir()
		store, err := Open(path, WithJSONCodec())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}

		// Attempt opening JSON store with Gob codec
		_, err = Open(path, WithGobCodec())
		if err == nil || !strings.Contains(err.Error(), "store codec mismatch") {
			t.Fatalf("Open(Gob on JSON store) err = %v, want store codec mismatch", err)
		}

		// Reopen with matching JSON codec must still succeed and find unaltered JSON marker
		reopened, err := Open(path, WithJSONCodec())
		if err != nil {
			t.Fatalf("reopening with JSON codec failed after rejected mismatch: %v", err)
		}
		defer reopened.Close()

		codec, found, err := reopened.getRawMetadata(storeCodecKey)
		if err != nil || !found || string(codec) != string(codecJSON) {
			t.Fatalf("marker mutated after rejected open: codec=%q found=%v", codec, found)
		}
	})

	t.Run("unmarked legacy store with valid JSON data is safely adopted", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		// Write valid legacy JSON execution record without meta/ markers
		if err := db.Set([]byte(execKey("legacy-exec-1")), []byte(`{"ID":"legacy-exec-1","Version":1}`), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		store, err := Open(path, WithJSONCodec())
		if err != nil {
			t.Fatalf("Open on legacy JSON store failed: %v", err)
		}
		defer store.Close()

		exec, err := store.GetExecution(ctx, "legacy-exec-1")
		if err != nil {
			t.Fatalf("GetExecution on adopted legacy store failed: %v", err)
		}
		if exec.ID != "legacy-exec-1" {
			t.Fatalf("exec.ID = %q, want legacy-exec-1", exec.ID)
		}

		// Verify markers were written
		schema, _, _ := store.getRawMetadata(storeSchemaKey)
		codec, _, _ := store.getRawMetadata(storeCodecKey)
		if string(schema) != storeSchemaVersion || string(codec) != string(codecJSON) {
			t.Fatalf("adopted markers mismatch: schema=%q codec=%q", schema, codec)
		}
	})

	t.Run("unmarked legacy store with mixed codecs fails closed without writing markers", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set([]byte(execKey("json-rec")), []byte(`{"ID":"json-rec"}`), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if err := db.Set([]byte(taskIDKey("gob-rec")), []byte("gob:raw-payload"), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = Open(path)
		if err == nil || !strings.Contains(err.Error(), "mixed codecs") {
			t.Fatalf("Open(mixed legacy store) err = %v, want mixed codecs error", err)
		}

		// Verify no format markers were written to the corrupted/mixed store
		verifyDB, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer verifyDB.Close()
		_, closer, err := verifyDB.Get(storeSchemaKey)
		if err == nil {
			_ = closer.Close()
			t.Fatal("storeSchemaKey was unexpectedly written to invalid legacy store")
		}
	})
}

// TestCoreFormatMarkerMetadataIsolation confirms format markers are isolated from execution namespaces.
func TestCoreFormatMarkerMetadataIsolation(t *testing.T) {
	for _, key := range [][]byte{storeSchemaKey, storeCodecKey} {
		if !bytes.HasPrefix(key, []byte("meta/")) {
			t.Fatalf("format marker key %q does not use meta/ namespace", string(key))
		}
		if isCodecEncodedStoreKey(key) {
			t.Fatalf("format marker key %q is falsely classified as codec encoded payload", string(key))
		}
	}
}
