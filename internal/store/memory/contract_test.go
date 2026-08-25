package memory

import (
	"testing"

	"github.com/Homiakus/axiom/internal/runtime"
	"github.com/Homiakus/axiom/internal/testutil"
)

func TestMemoryStoreContract(t *testing.T) {
	testutil.RunStoreContract(t, func(t *testing.T) runtime.Store {
		t.Helper()
		return NewStore()
	})
}
