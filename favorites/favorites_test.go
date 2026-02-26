package favorites

import (
	"path/filepath"
	"testing"
)

func TestAddLoadRemove(t *testing.T) {
	tmp := t.TempDir()
	s := &Store{path: filepath.Join(tmp, "fav.json")}

	if err := s.Add("https://example.com", "Example", "work"); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := s.Add("https://example.com", "Example 2", "personal"); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	item, ok := s.Get("https://example.com")
	if !ok {
		t.Fatal("expected stored favorite")
	}
	if item.Category != "personal" {
		t.Fatalf("expected updated category personal, got %q", item.Category)
	}

	s2 := &Store{path: filepath.Join(tmp, "fav.json")}
	if err := s2.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if _, ok := s2.Get("https://example.com"); !ok {
		t.Fatal("expected favorite after reload")
	}

	removed, err := s2.Remove("https://example.com")
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if !removed {
		t.Fatal("expected remove=true")
	}
	if _, ok := s2.Get("https://example.com"); ok {
		t.Fatal("did not expect favorite after remove")
	}
}

func TestDefaultCategory(t *testing.T) {
	tmp := t.TempDir()
	s := &Store{path: filepath.Join(tmp, "fav.json")}

	if err := s.Add("https://example.com/a", "A", ""); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	item, ok := s.Get("https://example.com/a")
	if !ok {
		t.Fatal("expected favorite")
	}
	if item.Category != "general" {
		t.Fatalf("expected general category, got %q", item.Category)
	}
}
