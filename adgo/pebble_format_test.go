package adgo

import (
	"context"
	"strings"
	"testing"

	pebbledb "github.com/cockroachdb/pebble"
)

func TestADGOPebbleStoreFormatMarkerCreatedInEmptyStore(t *testing.T) {
	path := t.TempDir()
	store, err := OpenPebbleStore(path)
	if err != nil {
		t.Fatalf("OpenPebbleStore failed: %v", err)
	}
	defer store.Close()

	schema, found, err := store.getRawMetadata(adgoStoreSchemaKey)
	if err != nil {
		t.Fatalf("getRawMetadata(schema) err = %v", err)
	}
	if !found || string(schema) != adgoStoreSchemaVersion {
		t.Fatalf("schema = %q, found = %v; want %q, true", schema, found, adgoStoreSchemaVersion)
	}

	format, found, err := store.getRawMetadata(adgoStoreFormatKey)
	if err != nil {
		t.Fatalf("getRawMetadata(format) err = %v", err)
	}
	if !found || string(format) != adgoStoreFormatID {
		t.Fatalf("format = %q, found = %v; want %q, true", format, found, adgoStoreFormatID)
	}
}

func TestADGOPebbleStoreReopenSameVersion(t *testing.T) {
	path := t.TempDir()
	store, err := OpenPebbleStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	exec := newTestExecution("reopen-test-exec", 1)
	if err := store.Create(ctx, exec); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenPebbleStore(path)
	if err != nil {
		t.Fatalf("reopening store with same version failed: %v", err)
	}
	defer reopened.Close()

	loaded, err := reopened.Load(ctx, "reopen-test-exec")
	if err != nil {
		t.Fatalf("Load after reopen failed: %v", err)
	}
	if loaded.ID != "reopen-test-exec" || loaded.Version != 1 {
		t.Fatalf("loaded execution mismatch: %+v", loaded)
	}
}

func TestADGOPebbleStoreRejectsFutureSchema(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Set(adgoStoreSchemaKey, []byte("99"), pebbledb.Sync); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Set(adgoStoreFormatKey, []byte(adgoStoreFormatID), pebbledb.Sync); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = OpenPebbleStore(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported store schema") {
		t.Fatalf("OpenPebbleStore err = %v; want unsupported store schema error", err)
	}
}

func TestADGOPebbleStoreRejectsUnsupportedFormat(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Set(adgoStoreSchemaKey, []byte(adgoStoreSchemaVersion), pebbledb.Sync); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Set(adgoStoreFormatKey, []byte("adgo-pebble-binary-v99"), pebbledb.Sync); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = OpenPebbleStore(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported store format") {
		t.Fatalf("OpenPebbleStore err = %v; want unsupported store format error", err)
	}
}

func TestADGOPebbleStorePartialMarkerFailsClosed(t *testing.T) {
	t.Run("MissingFormat", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set(adgoStoreSchemaKey, []byte(adgoStoreSchemaVersion), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "incomplete persisted format marker") {
			t.Fatalf("OpenPebbleStore err = %v; want incomplete persisted format marker error", err)
		}
	})

	t.Run("MissingSchema", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set(adgoStoreFormatKey, []byte(adgoStoreFormatID), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "incomplete persisted format marker") {
			t.Fatalf("OpenPebbleStore err = %v; want incomplete persisted format marker error", err)
		}
	})
}

func TestADGOPebbleStoreLegacyAdoption(t *testing.T) {
	path := t.TempDir()
	// Create raw store with valid legacy data (no format marker)
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	execJSON := `{"id":"legacy-1","planId":"test","planVersion":"1.0","planDigest":"digest","version":1,"status":"running","createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}`
	hash := pebbleExecutionHash("legacy-1")
	if err := db.Set([]byte("adgo/e/"+hash+"/latest"), []byte(execJSON), pebbledb.Sync); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Set([]byte("adgo/c/"+hash), []byte("legacy-1"), pebbledb.Sync); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// Open with OpenPebbleStore: should detect, adopt, and write format markers
	store, err := OpenPebbleStore(path)
	if err != nil {
		t.Fatalf("OpenPebbleStore failed on legacy store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	loaded, err := store.Load(ctx, "legacy-1")
	if err != nil {
		t.Fatalf("Load on adopted legacy store failed: %v", err)
	}
	if loaded.ID != "legacy-1" {
		t.Fatalf("loaded ID = %q, want legacy-1", loaded.ID)
	}

	schema, found, err := store.getRawMetadata(adgoStoreSchemaKey)
	if err != nil || !found || string(schema) != adgoStoreSchemaVersion {
		t.Fatalf("adopted schema = %q, found = %v", schema, found)
	}
}

func TestADGOPebbleStoreCorruptOrForeignLegacyStoreFailsClosed(t *testing.T) {
	t.Run("CorruptedExecutionJSON", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		hash := pebbleExecutionHash("corrupt-1")
		if err := db.Set([]byte("adgo/e/"+hash+"/latest"), []byte("not-valid-json{{{"), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "corrupted legacy execution data") {
			t.Fatalf("OpenPebbleStore err = %v; want corrupted legacy execution data error", err)
		}
	})

	t.Run("ForeignPrefixKey", func(t *testing.T) {
		path := t.TempDir()
		db, err := pebbledb.Open(path, &pebbledb.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set([]byte("foreign/namespace/data"), []byte("some-foreign-data"), pebbledb.Sync); err != nil {
			_ = db.Close()
			t.Fatal(err)
		}
		_ = db.Close()

		_, err = OpenPebbleStore(path)
		if err == nil || !strings.Contains(err.Error(), "unrecognized key prefix") {
			t.Fatalf("OpenPebbleStore err = %v; want unrecognized key prefix error", err)
		}
	})
}
