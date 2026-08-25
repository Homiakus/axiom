package pebble

import (
	"context"
	"testing"

	"github.com/Homiakus/axiom/internal/runtime"
	"github.com/Homiakus/axiom/internal/testutil"
)

func TestPebbleStoreContract(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		testutil.RunStoreContract(t, func(t *testing.T) runtime.Store {
			t.Helper()
			store, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		})
	})

	t.Run("transaction", func(t *testing.T) {
		testutil.RunStoreContract(t, func(t *testing.T) runtime.Store {
			t.Helper()
			store, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			tx, err := store.BeginTransaction(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tx.Rollback() })
			return tx
		})
	})
}
