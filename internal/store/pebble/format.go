package pebble

import (
	"bytes"
	"errors"
	"fmt"

	pebbledb "github.com/cockroachdb/pebble"
)

const storeSchemaVersion = "1"

var (
	storeSchemaKey = []byte("meta/axiom-store-schema")
	storeCodecKey  = []byte("meta/axiom-store-codec")
)

// ensureStoreFormat validates or adopts the persisted Store format before the
// store is returned to callers. The marker is deliberately independent from
// the selected value codec so it can always be read before decoding records.
func (s *Store) ensureStoreFormat() error {
	schema, schemaFound, err := s.getRawMetadata(storeSchemaKey)
	if err != nil {
		return err
	}
	codec, codecFound, err := s.getRawMetadata(storeCodecKey)
	if err != nil {
		return err
	}

	if schemaFound || codecFound {
		if !schemaFound || !codecFound {
			return fmt.Errorf("axiom pebble: incomplete persisted format marker")
		}
		if string(schema) != storeSchemaVersion {
			return fmt.Errorf("axiom pebble: unsupported store schema %q; supported schema is %q", schema, storeSchemaVersion)
		}
		persistedCodec := codecKind(codec)
		if persistedCodec != codecJSON && persistedCodec != codecGob {
			return fmt.Errorf("axiom pebble: unsupported persisted codec %q", codec)
		}
		if persistedCodec != s.codec {
			return fmt.Errorf("axiom pebble: store codec mismatch: persisted=%s requested=%s", persistedCodec, s.codec)
		}
		return nil
	}

	legacyCodec, hasLegacyData, err := s.detectLegacyCodec()
	if err != nil {
		return err
	}
	if hasLegacyData && legacyCodec != s.codec {
		return fmt.Errorf("axiom pebble: legacy store codec mismatch: detected=%s requested=%s", legacyCodec, s.codec)
	}
	return s.writeStoreFormatMarker()
}

func (s *Store) getRawMetadata(key []byte) ([]byte, bool, error) {
	value, closer, err := s.db.Get(key)
	if errors.Is(err, pebbledb.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	out := append([]byte(nil), value...)
	if err := closer.Close(); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func (s *Store) writeStoreFormatMarker() error {
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(storeSchemaKey, []byte(storeSchemaVersion), pebbledb.NoSync); err != nil {
		return err
	}
	if err := batch.Set(storeCodecKey, []byte(s.codec), pebbledb.NoSync); err != nil {
		return err
	}
	// Persisted format identity is always synchronously committed even when the
	// caller selected a weaker runtime durability mode. Otherwise Open could
	// acknowledge a format that disappears across a crash.
	return batch.Commit(pebbledb.Sync)
}

func (s *Store) detectLegacyCodec() (codecKind, bool, error) {
	iter, err := s.db.NewIter(nil)
	if err != nil {
		return "", false, err
	}
	defer iter.Close()

	var detected codecKind
	hasData := false
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if bytes.Equal(key, storeSchemaKey) || bytes.Equal(key, storeCodecKey) {
			continue
		}
		hasData = true
		if !isCodecEncodedStoreKey(key) {
			continue
		}
		candidate := codecJSON
		value := iter.Value()
		if bytes.HasPrefix(value, []byte("gob:")) || bytes.HasPrefix(value, []byte("json:")) {
			candidate = codecGob
		}
		if detected != "" && detected != candidate {
			return "", true, fmt.Errorf("axiom pebble: legacy store contains mixed codecs (%s and %s)", detected, candidate)
		}
		detected = candidate
	}
	if err := iter.Error(); err != nil {
		return "", false, err
	}
	if !hasData {
		return s.codec, false, nil
	}
	if detected == "" {
		return "", true, fmt.Errorf("axiom pebble: cannot infer codec for unmarked non-empty legacy store")
	}
	return detected, true, nil
}

func isCodecEncodedStoreKey(key []byte) bool {
	for _, prefix := range [][]byte{
		[]byte("exec/"),
		[]byte("hist/"),
		[]byte("task/"),
		[]byte("taskid/"),
		[]byte("tstatus/"),
		[]byte("tdedup/"),
	} {
		if bytes.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
