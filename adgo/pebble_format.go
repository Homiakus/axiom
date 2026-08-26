package adgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	pebbledb "github.com/cockroachdb/pebble"
)

const (
	adgoStoreSchemaVersion = "1"
	adgoStoreFormatID      = "adgo-pebble-json-v1"
)

var (
	adgoStoreSchemaKey = []byte("meta/adgo-store-schema")
	adgoStoreFormatKey = []byte("meta/adgo-store-format")
)

// ensureStoreFormat validates or initializes the persisted ADGO Pebble store format.
func (s *PebbleStore) ensureStoreFormat() error {
	schema, schemaFound, err := s.getRawMetadata(adgoStoreSchemaKey)
	if err != nil {
		return err
	}
	format, formatFound, err := s.getRawMetadata(adgoStoreFormatKey)
	if err != nil {
		return err
	}

	if schemaFound || formatFound {
		if !schemaFound || !formatFound {
			return fmt.Errorf("adgo pebble: incomplete persisted format marker")
		}
		if string(schema) != adgoStoreSchemaVersion {
			return fmt.Errorf("adgo pebble: unsupported store schema %q; supported schema is %q", schema, adgoStoreSchemaVersion)
		}
		if string(format) != adgoStoreFormatID {
			return fmt.Errorf("adgo pebble: unsupported store format %q; supported format is %q", format, adgoStoreFormatID)
		}
		return nil
	}

	// Unmarked database: validate that any existing data matches expected ADGO schema.
	if _, err := s.validateLegacyData(); err != nil {
		return err
	}
	return s.writeStoreFormatMarker()
}

func (s *PebbleStore) getRawMetadata(key []byte) ([]byte, bool, error) {
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

func (s *PebbleStore) writeStoreFormatMarker() error {
	batch := s.db.NewBatch()
	defer batch.Close()
	if err := batch.Set(adgoStoreSchemaKey, []byte(adgoStoreSchemaVersion), pebbledb.NoSync); err != nil {
		return err
	}
	if err := batch.Set(adgoStoreFormatKey, []byte(adgoStoreFormatID), pebbledb.NoSync); err != nil {
		return err
	}
	// Persisted format marker is always synchronously committed to ensure durability.
	return batch.Commit(pebbledb.Sync)
}

func (s *PebbleStore) validateLegacyData() (bool, error) {
	iter, err := s.db.NewIter(nil)
	if err != nil {
		return false, err
	}
	defer iter.Close()

	hasData := false
	for iter.First(); iter.Valid(); iter.Next() {
		key := iter.Key()
		if bytes.Equal(key, adgoStoreSchemaKey) || bytes.Equal(key, adgoStoreFormatKey) {
			continue
		}
		hasData = true
		if bytes.HasPrefix(key, []byte("adgo/e/")) {
			if bytes.HasSuffix(key, []byte("/latest")) || bytes.Contains(key, []byte("/v/")) {
				var exec Execution
				if err := json.Unmarshal(iter.Value(), &exec); err != nil {
					return true, fmt.Errorf("adgo pebble: corrupted legacy execution data at key %q: %w", string(key), err)
				}
			} else if bytes.Contains(key, []byte("/inbox/")) {
				var ev Event
				if err := json.Unmarshal(iter.Value(), &ev); err != nil {
					return true, fmt.Errorf("adgo pebble: corrupted legacy inbox data at key %q: %w", string(key), err)
				}
			}
		} else if bytes.HasPrefix(key, []byte("adgo/c/")) {
			if len(iter.Value()) == 0 {
				return true, fmt.Errorf("adgo pebble: corrupted legacy catalog entry at key %q", string(key))
			}
		} else {
			return true, fmt.Errorf("adgo pebble: unrecognized key prefix %q in unmarked store", string(key))
		}
	}
	if err := iter.Error(); err != nil {
		return false, err
	}
	return hasData, nil
}
