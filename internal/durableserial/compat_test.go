package durableserial

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Homiakus/axiom/adgo"
	"github.com/Homiakus/axiom/internal/runtime"
	pebblecore "github.com/Homiakus/axiom/internal/store/pebble"
)

func TestGoldenCompatibilityFixturesExistAndDecode(t *testing.T) {
	root := filepath.Join("..", "..")

	for _, entry := range Registry {
		t.Run(entry.SurfaceID, func(t *testing.T) {
			path := filepath.Join(root, entry.GoldenFixturePath)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read golden fixture at %s: %v", path, err)
			}
			if len(data) == 0 {
				t.Fatalf("golden fixture at %s is empty", path)
			}

			// Surface-specific decoding checks
			switch entry.SurfaceID {
			case "CORE-PEBBLE-EXECUTION":
				var exec runtime.Execution
				if err := json.Unmarshal(data, &exec); err != nil {
					t.Fatalf("failed to unmarshal Execution: %v", err)
				}
				if exec.ID == "" || exec.Version == 0 {
					t.Fatalf("invalid decoded Execution: %+v", exec)
				}

			case "CORE-PEBBLE-HISTORY":
				var hist runtime.HistoryEntry
				if err := json.Unmarshal(data, &hist); err != nil {
					t.Fatalf("failed to unmarshal HistoryEntry: %v", err)
				}
				if hist.Seq == 0 || hist.Type == "" {
					t.Fatalf("invalid decoded HistoryEntry: %+v", hist)
				}

			case "CORE-PEBBLE-TASK":
				var task runtime.ActivityTask
				if err := json.Unmarshal(data, &task); err != nil {
					t.Fatalf("failed to unmarshal ActivityTask: %v", err)
				}
				if task.ID == "" || task.ExecutionID == "" {
					t.Fatalf("invalid decoded ActivityTask: %+v", task)
				}

			case "ADGO-FILESTORE-COMMIT", "ADGO-PEBBLE-LATEST", "ADGO-PEBBLE-VERSION":
				var exec adgo.Execution
				if err := json.Unmarshal(data, &exec); err != nil {
					t.Fatalf("failed to unmarshal ADGO Execution: %v", err)
				}
				if exec.ID == "" || exec.Version == 0 {
					t.Fatalf("invalid decoded ADGO Execution: %+v", exec)
				}

			case "ADGO-FILESTORE-INBOX", "ADGO-PEBBLE-INBOX":
				var ev adgo.Event
				if err := json.Unmarshal(data, &ev); err != nil {
					t.Fatalf("failed to unmarshal ADGO Event: %v", err)
				}
				if ev.ID == "" || ev.Type == "" {
					t.Fatalf("invalid decoded ADGO Event: %+v", ev)
				}

			case "ADGO-SCHEDULE-STORE":
				var sched adgo.Schedule
				if err := json.Unmarshal(data, &sched); err != nil {
					t.Fatalf("failed to unmarshal ADGO Schedule: %v", err)
				}
				if sched.ID == "" || sched.Every == 0 {
					t.Fatalf("invalid decoded ADGO Schedule: %+v", sched)
				}

			default:
				if entry.Encoding == EncodingJSON || entry.Encoding == EncodingJSONOrGob {
					var raw any
					if err := json.Unmarshal(data, &raw); err != nil {
						t.Fatalf("invalid JSON in golden fixture %s: %v", path, err)
					}
				}
			}
		})
	}
}

func TestBackwardCompatibleFieldAdditions(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "testdata/compat/core_pebble_execution.json"))
	if err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	// Add unknown future fields
	m["FutureNewField"] = "future-value-42"
	m["FutureMetadata"] = map[string]any{"v": 2, "flags": []string{"beta", "experimental"}}

	augmentedJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var exec runtime.Execution
	if err := json.Unmarshal(augmentedJSON, &exec); err != nil {
		t.Fatalf("failed to unmarshal Execution with augmented future fields: %v", err)
	}
	if exec.ID != "exec-compat-001" || exec.Version != 3 {
		t.Fatalf("decoded Execution corrupted by additional fields: %+v", exec)
	}
}

func TestCorruptedRecordsTriggerDeterministicErrors(t *testing.T) {
	corruptedInputs := [][]byte{
		[]byte("{"),
		[]byte(`{"ID": "test", "Version": "not-an-int"}`),
		[]byte(`{"ID": 12345}`),
	}

	for i, corrupt := range corruptedInputs {
		var exec runtime.Execution
		if err := json.Unmarshal(corrupt, &exec); err == nil {
			t.Errorf("corrupt input #%d expected error, got nil (decoded=%+v)", i, exec)
		}
	}
}

func TestPebbleStoreReopenFromGoldenData(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "testdata/compat/core_pebble_execution.json"))
	if err != nil {
		t.Fatal(err)
	}

	var original runtime.Execution
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}

	tempDir := t.TempDir()
	store, err := pebblecore.Open(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.CreateExecution(ctx, &original); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen store and verify exact fidelity
	reopened, err := pebblecore.Open(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	loaded, err := reopened.GetExecution(ctx, original.ID)
	if err != nil {
		t.Fatalf("GetExecution failed after reopen: %v", err)
	}
	if loaded.ID != original.ID || loaded.Version != original.Version || loaded.Status != original.Status {
		t.Fatalf("loaded execution mutated across reopen: got %+v, want %+v", loaded, original)
	}
}
