package markers

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateMarker(t *testing.T) {
	tempDir := t.TempDir()
	m := New("", tempDir)

	err := m.CreateMarker("test.marker")
	if err != nil {
		t.Fatalf("CreateMarker failed: %v", err)
	}

	path := filepath.Join(tempDir, "test.marker")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("marker file was not created")
	}

	// Test idempotent creation (already exists)
	err = m.CreateMarker("test.marker")
	if err != nil {
		t.Errorf("CreateMarker returned error on existing marker: %v", err)
	}

	// Test directory creation
	tempDir2 := t.TempDir()
	subDir := filepath.Join(tempDir2, "sub")
	m2 := New("", subDir)
	err = m2.CreateMarker("test2.marker")
	if err != nil {
		t.Fatalf("CreateMarker failed to create subdir: %v", err)
	}

	// Test read-only dir
	readOnlyDir := filepath.Join(tempDir2, "readonly")
	if err := os.Mkdir(readOnlyDir, 0555); err != nil {
		t.Fatalf("failed to create readonly dir: %v", err)
	}
	m3 := New("", readOnlyDir)
	err = m3.CreateMarker("test3.marker")
	if err == nil {
		t.Error("expected error creating marker in readonly dir, got nil")
	}
}

func TestDeleteAllMarkers(t *testing.T) {
	tempDir := t.TempDir()
	m := New("", tempDir)

	// Setup markers
	m.CreateMarker("m1.marker")
	m.CreateMarker("m2.marker")

	err := m.DeleteAllMarkers()
	if err != nil {
		t.Fatalf("DeleteAllMarkers failed: %v", err)
	}

	entries, _ := os.ReadDir(tempDir)
	if len(entries) != 0 {
		t.Errorf("expected 0 markers left, got %d", len(entries))
	}

	// Test non-existent dir
	m2 := New("", filepath.Join(tempDir, "nonexistent"))
	err = m2.DeleteAllMarkers()
	if err != nil {
		t.Errorf("expected nil error for non-existent dir, got %v", err)
	}

	// Setup a subdir to ensure it's ignored
	os.Mkdir(filepath.Join(tempDir, "subdir"), 0755)
	err = m.DeleteAllMarkers()
	if err != nil {
		t.Errorf("expected no error when deleting with subdir present, got %v", err)
	}
	entries, _ = os.ReadDir(tempDir)
	if len(entries) != 1 || !entries[0].IsDir() {
		t.Error("subdir should not have been deleted")
	}
}

func TestHasAnyMarker(t *testing.T) {
	tempDir := t.TempDir()
	m := New("", tempDir)

	has, err := m.HasAnyMarker()
	if err != nil {
		t.Fatalf("HasAnyMarker failed: %v", err)
	}
	if has {
		t.Error("expected false for empty dir, got true")
	}

	m.CreateMarker("m1.marker")
	has, err = m.HasAnyMarker()
	if err != nil {
		t.Fatalf("HasAnyMarker failed: %v", err)
	}
	if !has {
		t.Error("expected true, got false")
	}

	// Test non-existent dir
	m2 := New("", filepath.Join(tempDir, "nonexistent"))
	has, err = m2.HasAnyMarker()
	if err != nil {
		t.Errorf("expected nil error for non-existent dir, got %v", err)
	}
	if has {
		t.Error("expected false for non-existent dir")
	}

	// Test with only subdir
	tempDir2 := t.TempDir()
	os.Mkdir(filepath.Join(tempDir2, "subdir"), 0755)
	m3 := New("", tempDir2)
	has, err = m3.HasAnyMarker()
	if err != nil {
		t.Fatalf("HasAnyMarker failed: %v", err)
	}
	if has {
		t.Error("expected false with only subdir, got true")
	}
}

func TestHasMarker(t *testing.T) {
	tempDir := t.TempDir()
	m := New("", tempDir)

	// Test missing marker
	has, err := m.HasMarker("test.marker")
	if err != nil {
		t.Fatalf("HasMarker failed: %v", err)
	}
	if has {
		t.Error("expected false for missing marker, got true")
	}

	// Create marker and test
	m.CreateMarker("test.marker")
	has, err = m.HasMarker("test.marker")
	if err != nil {
		t.Fatalf("HasMarker failed: %v", err)
	}
	if !has {
		t.Error("expected true for existing marker, got false")
	}

	// Test different name
	has, err = m.HasMarker("other.marker")
	if err != nil {
		t.Fatalf("HasMarker failed: %v", err)
	}
	if has {
		t.Error("expected false for different marker name, got true")
	}

	// Test non-existent dir
	m2 := New("", filepath.Join(tempDir, "nonexistent"))
	has, err = m2.HasMarker("test.marker")
	if err != nil {
		t.Fatalf("HasMarker failed: %v", err)
	}
	if has {
		t.Error("expected false for non-existent dir, got true")
	}
}

