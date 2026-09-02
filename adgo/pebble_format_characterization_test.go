package adgo

import (
	"bytes"
	"context"
	"strings"
	"testing"

	pebbledb "github.com/cockroachdb/pebble"
)

// TestADGOFormatMarkerCharacterization characterises absent, valid, partial,
// wrong expected value, and reopen boundaries for ADGO Pebble stores.
func TestADGOFormatMarkerCharacterization(t *testing.T) {
	ctx := context.Background()

	t.Run("absent marker in empty store writes matching markers synchronously", func(t *testing.T) {
		path := t.TempDir()
		store, err := OpenPebbleStore(path)
		if err != nil {
			t.Fatalf("OpenPebbleStore(empty store) error = %v", err)
		}
		defer store.Close()

		schema, schemaFound, err := store.getRawMetadata(adgoStoreSchemaKey)
		if err != nil || !schemaFound || string(schema) != adgoStoreSchemaVersion {
			t.Fatalf("schema marker = %q (found=%v, err=%v), want %q", schema, schemaFound, err, adgoStoreSchemaVersion)
		}
		format, formatFound, err := store.getRawMetadata(adgoStoreFormatKey)
		if err != nil || !formatFound || string(format) != adgoStoreFormatID {
			t.Fatalf("format marker = %q (found=%v, err=%v), want %q", format, formatFound, err, adgoStoreFormatID)
		}
	})

	t.Run("valid matching marker allows reopen and data retrieval", func(t *testing.T) {
		path := t.TempDir()
		store, err := OpenPebbleStore(path)
		if err != nil {
			t.Fatalf("first OpenPebbleStore failed: %v", err)
		}
		exec := newTestExecution("adgo-valid-exec", 1)
		if err := store.Create(ctx, exec); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := OpenPebbleStore(path)
		if err != nil {
			t.Fatalf("reopen failed: %v", err)
		}
		defer reopened.Close()

		got, err := reopened.Load(ctx, "adgo-valid-exec")
		if err != nil {
			t.Fatalf("Load after valid reopen failed: %v", err)
		}
		if got.ID != "adgo-valid-exec" || got.Version != 1 {
			t.Fatalf("retrieved execution = %+v, want ID=adgo-valid-exec Version=1", got)
		}
	})

	t.Run("partial marker with schema only fails closed", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set(adgoStoreSchemaKey, []byte(adgoStoreSchemaVersion), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "incomplete persisted format marker") {
			t.Fatalf("OpenPebbleStore(partial schema-only) err = %v, want incomplete marker error", err)
		}
	})

	t.Run("partial marker with format only fails closed", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set(adgoStoreFormatKey, []byte(adgoStoreFormatID), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "incomplete persisted format marker") {
			t.Fatalf("OpenPebbleStore(partial format-only) err = %v, want incomplete marker error", err)
		}
	})

	t.Run("unsupported future schema versions fail closed", func(t *testing.T) {
		for _, futureSchema := range []string{"2", "99", "v1.0", "beta"} {
			path := t.TempDir()
			db, err := pebbledb.Open(path, &pebbledb.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Set(adgoStoreSchemaKey, []byte(futureSchema), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			if err := db.Set(adgoStoreFormatKey, []byte(adgoStoreFormatID), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			_ = db.Close()

			_, err = OpenPebbleStore(path)
			if err == nil || !strings.Contains(err.Error(), "unsupported store schema") {
				t.Fatalf("OpenPebbleStore(schema=%q) err = %v, want unsupported store schema error", futureSchema, err)
			}
		}
	})

	t.Run("unsupported format identifier fails closed", func(t *testing.T) {
		for _, badFormat := range []string{"adgo-pebble-gob-v1", "adgo-pebble-json-v2", "custom-binary", ""} {
			path := t.TempDir()
			db, err := pebbledb.Open(path, &pebbledb.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Set(adgoStoreSchemaKey, []byte(adgoStoreSchemaVersion), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			if err := db.Set(adgoStoreFormatKey, []byte(badFormat), pebbledb.Sync); err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			_ = db.Close()

			_, err = OpenPebbleStore(path)
			if err == nil || !strings.Contains(err.Error(), "unsupported store format") {
				t.Fatalf("OpenPebbleStore(format=%q) err = %v, want unsupported store format error", badFormat, err)
			}
		}
	})

	t.Run("unmarked legacy store with valid ADGO data is safely adopted", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		execJSON := `{"id":"legacy-adgo-1","planId":"p","planVersion":"1.0","planDigest":"d","version":1,"status":"running","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}`
		hash := pebbleExecutionHash("legacy-adgo-1")
		if err := db.Set([]byte("adgo/e/"+hash+"/latest"), []byte(execJSON), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if err := db.Set([]byte("adgo/c/"+hash), []byte("legacy-adgo-1"), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		store, err := OpenPebbleStore(path)
		if err != nil {
			t.Fatalf("OpenPebbleStore on legacy store failed: %v", err)
		}
		defer store.Close()

		exec, err := store.Load(ctx, "legacy-adgo-1")
		if err != nil {
			t.Fatalf("Load on adopted legacy store failed: %v", err)
		}
		if exec.ID != "legacy-adgo-1" {
			t.Fatalf("exec.ID = %q, want legacy-adgo-1", exec.ID)
		}

		schema, _, _ := store.getRawMetadata(adgoStoreSchemaKey)
		format, _, _ := store.getRawMetadata(adgoStoreFormatKey)
		if string(schema) != adgoStoreSchemaVersion || string(format) != adgoStoreFormatID {
			t.Fatalf("adopted markers mismatch: schema=%q format=%q", schema, format)
		}
	})

	t.Run("unmarked legacy store with corrupt JSON fails closed without writing markers", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		hash := pebbleExecutionHash("corrupted-exec")
		if err := db.Set([]byte("adgo/e/"+hash+"/latest"), []byte("{corrupt-json"), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "corrupted legacy execution data") {
			t.Fatalf("OpenPebbleStore(corrupt legacy data) err = %v, want corrupted legacy data error", err)
		}

		// Verify no format markers were written
		verifyDB, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer verifyDB.Close()
		_, closer, err := verifyDB.Get(adgoStoreSchemaKey)
		if err == nil {
			_ = closer.Close()
			t.Fatal("adgoStoreSchemaKey was unexpectedly written to invalid legacy store")
		}
	})

	t.Run("unmarked legacy store with foreign key prefix fails closed without writing markers", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set([]byte("unrecognized/prefix/entry"), []byte("foreign-value"), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "unrecognized key prefix") {
			t.Fatalf("OpenPebbleStore(foreign key prefix) err = %v, want unrecognized key prefix error", err)
		}
	})
}

// TestADGOFormatMarkerMetadataIsolation confirms format markers use meta/ namespace.
func TestADGOFormatMarkerMetadataIsolation(t *testing.T) {
	for _, key := range [][]byte{adgoStoreSchemaKey, adgoStoreFormatKey} {
		if !bytes.HasPrefix(key, []byte("meta/")) {
			t.Fatalf("ADGO format marker key %q does not use meta/ namespace", string(key))
		}
		if bytes.HasPrefix(key, []byte("adgo/e/")) || bytes.HasPrefix(key, []byte("adgo/c/")) {
			t.Fatalf("ADGO format marker key %q overlaps with data namespace", string(key))
		}
	}
}
