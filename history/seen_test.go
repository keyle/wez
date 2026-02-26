package history

import (
	"path/filepath"
	"testing"
)

func TestSeenAddLoadClear(t *testing.T) {
	tmp := t.TempDir()
	s := &Seen{path: filepath.Join(tmp, "seen_urls"), set: make(map[string]struct{})}

	if err := s.Add("https://example.com"); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if err := s.Add("https://example.com"); err != nil {
		t.Fatalf("add duplicate failed: %v", err)
	}
	if !s.Contains("https://example.com") {
		t.Fatal("expected seen set to contain URL")
	}

	s2 := &Seen{path: filepath.Join(tmp, "seen_urls"), set: make(map[string]struct{})}
	if err := s2.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if !s2.Contains("https://example.com") {
		t.Fatal("expected loaded seen set to contain URL")
	}

	if err := s2.Clear(); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if s2.Contains("https://example.com") {
		t.Fatal("expected seen set to be empty after clear")
	}
}
