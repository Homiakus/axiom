package pebble

import (
	"context"
	"strings"
	"testing"

	"github.com/Homiakus/axiom/internal/runtime"
	pebbledb "github.com/cockroachdb/pebble"
)

func TestStoreFormatMarkerRejectsCodecMismatch(t *testing.T) {
	path := t.TempDir()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateExecution(context.Background(), &runtime.Execution{ID: "json"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path, WithGobCodec()); err == nil || !strings.Contains(err.Error(), "store codec mismatch") {
		t.Fatalf("Open(Gob) error = %v, want codec mismatch", err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatalf("matching JSON reopen failed after rejected mismatch: %v", err)
	}
	defer store.Close()
	if _, err := store.GetExecution(context.Background(), "json"); err != nil {
		t.Fatalf("GetExecution() after matching reopen: %v", err)
	}
}

func TestStoreFormatMarkerPinsCodecEvenWhenDatabaseIsEmpty(t *testing.T) {
	path := t.TempDir()
	store, err := Open(path, WithGobCodec())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "store codec mismatch") {
		t.Fatalf("Open(default JSON) error = %v, want codec mismatch", err)
	}
	store, err = Open(path, WithGobCodec())
	if err != nil {
		t.Fatalf("matching Gob reopen failed: %v", err)
	}
	_ = store.Close()
}

func TestLegacyJSONStoreIsAdoptedAndMarked(t *testing.T) {
	path := t.TempDir()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateExecution(context.Background(), &runtime.Execution{ID: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	removeFormatMarker(t, path)

	store, err = Open(path)
	if err != nil {
		t.Fatalf("Open(legacy JSON) error = %v", err)
	}
	defer store.Close()
	if _, err := store.GetExecution(context.Background(), "legacy"); err != nil {
		t.Fatalf("legacy execution not readable after adoption: %v", err)
	}
	schema, schemaFound, err := store.getRawMetadata(storeSchemaKey)
	if err != nil {
		t.Fatal(err)
	}
	codec, codecFound, err := store.getRawMetadata(storeCodecKey)
	if err != nil {
		t.Fatal(err)
	}
	if !schemaFound || string(schema) != storeSchemaVersion || !codecFound || string(codec) != string(codecJSON) {
		t.Fatalf("adopted marker: schema=%q found=%v codec=%q found=%v", schema, schemaFound, codec, codecFound)
	}
}

func TestLegacyStoreRejectsWrongCodecBeforeAdoption(t *testing.T) {
	path := t.TempDir()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateExecution(context.Background(), &runtime.Execution{ID: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	removeFormatMarker(t, path)

	if _, err := Open(path, WithGobCodec()); err == nil || !strings.Contains(err.Error(), "legacy store codec mismatch") {
		t.Fatalf("Open(legacy JSON as Gob) error = %v, want legacy codec mismatch", err)
	}

	// A rejected open must not write the requested wrong marker.
	store, err = Open(path)
	if err != nil {
		t.Fatalf("Open(legacy JSON) after rejected Gob adoption: %v", err)
	}
	_ = store.Close()
}

func TestLegacyStoreRejectsMixedCodecs(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	batch := db.NewBatch()
	if err := batch.Set([]byte(execKey("json")), []byte(`{"ID":"json"}`), pebbledb.NoSync); err != nil {
		t.Fatal(err)
	}
	if err := batch.Set([]byte(taskIDKey("gob")), []byte("gob:not-a-real-record"), pebbledb.NoSync); err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(pebbledb.Sync); err != nil {
		t.Fatal(err)
	}
	_ = batch.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "mixed codecs") {
		t.Fatalf("Open(mixed legacy store) error = %v, want mixed codec error", err)
	}
}

func TestLegacyStoreRejectsAmbiguousNonEmptyFormat(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Set([]byte(historySeqKey("orphan")), []byte("1"), pebbledb.Sync); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "cannot infer codec") {
		t.Fatalf("Open(ambiguous legacy store) error = %v, want inference error", err)
	}
}

func TestStoreFormatMarkerRejectsFutureSchema(t *testing.T) {
	path := t.TempDir()
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Set(storeSchemaKey, []byte("999"), pebbledb.Sync); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "unsupported store schema") {
		t.Fatalf("Open(future schema) error = %v, want unsupported schema error", err)
	}
}

func TestStoreFormatMarkerRejectsPartialMarker(t *testing.T) {
	path := t.TempDir()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Set(storeSchemaKey, []byte(storeSchemaVersion), pebbledb.Sync); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "incomplete persisted format marker") {
		t.Fatalf("Open(partial marker) error = %v, want incomplete marker error", err)
	}
}

func removeFormatMarker(t *testing.T, path string) {
	t.Helper()
	db, err := pebbledb.Open(path, &pebbledb.Options{})
	if err != nil {
		t.Fatal(err)
	}
	batch := db.NewBatch()
	if err := batch.Delete(storeSchemaKey, pebbledb.NoSync); err != nil {
		t.Fatal(err)
	}
	if err := batch.Delete(storeCodecKey, pebbledb.NoSync); err != nil {
		t.Fatal(err)
	}
	if err := batch.Commit(pebbledb.Sync); err != nil {
		t.Fatal(err)
	}
	_ = batch.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
