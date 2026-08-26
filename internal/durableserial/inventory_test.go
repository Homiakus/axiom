package durableserial

import (
	"testing"
)

func TestSerializedSurfaceRegistryValid(t *testing.T) {
	if len(Registry) == 0 {
		t.Fatal("expected non-empty SerializedSurface Registry")
	}

	validCategories := map[SurfaceCategory]bool{
		CategoryCoreExecution:    true,
		CategoryCoreTaskHistory:  true,
		CategoryADGOExecution:    true,
		CategoryADGOInbox:        true,
		CategoryADGOLocking:      true,
		CategoryFlowStateOutbox:  true,
		CategoryScheduleRouter:   true,
		CategoryAdmissionControl: true,
		CategoryRetentionRepair:  true,
		CategoryArtifactManifest: true,
	}

	ids := make(map[string]bool)
	for i, entry := range Registry {
		if entry.SurfaceID == "" {
			t.Errorf("entry #%d missing SurfaceID", i)
		}
		if ids[entry.SurfaceID] {
			t.Errorf("duplicate SurfaceID %q at entry #%d", entry.SurfaceID, i)
		}
		ids[entry.SurfaceID] = true

		if entry.Name == "" {
			t.Errorf("entry %s missing Name", entry.SurfaceID)
		}
		if entry.OwnerPackage == "" {
			t.Errorf("entry %s missing OwnerPackage", entry.SurfaceID)
		}
		if !validCategories[entry.Category] {
			t.Errorf("entry %s has invalid category %q", entry.SurfaceID, entry.Category)
		}
		if entry.StorageMedium == "" {
			t.Errorf("entry %s missing StorageMedium", entry.SurfaceID)
		}
		if entry.KeyPattern == "" {
			t.Errorf("entry %s missing KeyPattern", entry.SurfaceID)
		}
		if entry.Encoding == "" {
			t.Errorf("entry %s missing Encoding", entry.SurfaceID)
		}
		if entry.SchemaVersionField == "" {
			t.Errorf("entry %s missing SchemaVersionField", entry.SurfaceID)
		}
		if entry.CompatibilityPromise == "" {
			t.Errorf("entry %s missing CompatibilityPromise", entry.SurfaceID)
		}
		if entry.MigrationPath == "" {
			t.Errorf("entry %s missing MigrationPath", entry.SurfaceID)
		}
		if entry.GoldenFixturePath == "" {
			t.Errorf("entry %s missing GoldenFixturePath", entry.SurfaceID)
		}
	}
}
