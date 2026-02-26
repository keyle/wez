package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryAddAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	h := &History{
		path: filepath.Join(tmpDir, "history"),
	}

	// Add entries.
	if err := h.Add("http://example.com", "Example"); err != nil {
		t.Fatal(err)
	}
	if err := h.Add("http://golang.org", "Go"); err != nil {
		t.Fatal(err)
	}

	entries := h.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].URL != "http://example.com" {
		t.Errorf("expected first URL 'http://example.com', got %q", entries[0].URL)
	}
	if entries[1].Title != "Go" {
		t.Errorf("expected second title 'Go', got %q", entries[1].Title)
	}

	// Create a new History instance and load from file.
	h2 := &History{
		path: filepath.Join(tmpDir, "history"),
	}
	if err := h2.Load(); err != nil {
		t.Fatal(err)
	}
	entries2 := h2.Entries()
	if len(entries2) != 2 {
		t.Fatalf("expected 2 loaded entries, got %d", len(entries2))
	}
}

func TestHistoryContains(t *testing.T) {
	tmpDir := t.TempDir()
	h := &History{
		path: filepath.Join(tmpDir, "history"),
	}

	_ = h.Add("http://example.com", "Example")

	if !h.Contains("http://example.com") {
		t.Error("expected Contains to return true")
	}
	if h.Contains("http://other.com") {
		t.Error("expected Contains to return false for unknown URL")
	}
}

func TestHistoryLoadMissingFile(t *testing.T) {
	h := &History{
		path: filepath.Join(t.TempDir(), "nonexistent"),
	}
	err := h.Load()
	if err != nil {
		t.Errorf("loading missing file should not error, got: %v", err)
	}
	if len(h.Entries()) != 0 {
		t.Error("expected empty entries for missing file")
	}
}

func TestHistoryCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	h := &History{
		path: filepath.Join(tmpDir, "subdir", "history"),
	}

	if err := h.Add("http://example.com", "Test"); err != nil {
		t.Fatal(err)
	}

	// Check directory was created.
	if _, err := os.Stat(filepath.Join(tmpDir, "subdir")); os.IsNotExist(err) {
		t.Error("expected directory to be created")
	}
}

func TestHistoryClear(t *testing.T) {
	tmpDir := t.TempDir()
	h := &History{path: filepath.Join(tmpDir, "history")}
	if err := h.Add("http://example.com", "Example"); err != nil {
		t.Fatal(err)
	}
	if err := h.Clear(); err != nil {
		t.Fatal(err)
	}
	if len(h.Entries()) != 0 {
		t.Fatal("expected in-memory history to be empty after clear")
	}
}
