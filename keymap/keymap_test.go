package keymap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestDefaultBindings(t *testing.T) {
	defaults := DefaultBindings()

	// Verify core actions exist.
	required := []string{
		Quit, ScrollDown, ScrollUp, CursorLeft, CursorRight,
		PageDown, PageUp, HalfPageDown, HalfPageUp,
		GoTop, GoBottom, Back, Forward,
		OpenURL, OpenURLEdit, FollowLink, NextLink, PrevLink,
		Search, SearchWeb, SearchNext, SearchPrev, Reload, OpenImage, YankURL, YankLinkURL,
		VisualMode, VisualLine,
	}

	for _, action := range required {
		keys, ok := defaults[action]
		if !ok {
			t.Errorf("default bindings missing action %q", action)
		}
		if len(keys) == 0 {
			t.Errorf("action %q has no key bindings", action)
		}
	}
}

func TestBackIncludesBKeys(t *testing.T) {
	defaults := DefaultBindings()
	backKeys := defaults[Back]

	has := func(key string) bool {
		for _, k := range backKeys {
			if k == key {
				return true
			}
		}
		return false
	}

	if !has("b") {
		t.Error("expected 'b' in back bindings")
	}
	if !has("B") {
		t.Error("expected 'B' in back bindings")
	}
	if !has("H") {
		t.Error("expected 'H' still in back bindings")
	}
	if !has("Backspace") {
		t.Error("expected 'Backspace' in back bindings")
	}
}

func TestNewDefaultKeymap(t *testing.T) {
	km := New()

	// Single key: q -> quit.
	action, pending := km.Resolve("", "q")
	if action != Quit {
		t.Errorf("expected 'q' -> quit, got %q", action)
	}
	if pending != "" {
		t.Errorf("expected no pending, got %q", pending)
	}

	// Single key: j -> scroll_down.
	action, _ = km.Resolve("", "j")
	if action != ScrollDown {
		t.Errorf("expected 'j' -> scroll_down, got %q", action)
	}

	// b -> back.
	action, _ = km.Resolve("", "b")
	if action != Back {
		t.Errorf("expected 'b' -> back, got %q", action)
	}

	// B -> back.
	action, _ = km.Resolve("", "B")
	if action != Back {
		t.Errorf("expected 'B' -> back, got %q", action)
	}
}

func TestSequenceGG(t *testing.T) {
	km := New()

	// First 'g' should start a sequence.
	action, pending := km.Resolve("", "g")
	if action != "" {
		t.Errorf("expected no action on first 'g', got %q", action)
	}
	if pending != "g" {
		t.Errorf("expected pending 'g', got %q", pending)
	}

	// Second 'g' completes the "gg" sequence.
	action, pending = km.Resolve("g", "g")
	if action != GoTop {
		t.Errorf("expected 'gg' -> go_top, got %q", action)
	}
	if pending != "" {
		t.Errorf("expected no pending after 'gg', got %q", pending)
	}
}

func TestSequenceNotCompleted(t *testing.T) {
	km := New()

	// First 'g' starts sequence.
	_, pending := km.Resolve("", "g")
	if pending != "g" {
		t.Fatalf("expected pending 'g', got %q", pending)
	}

	// Pressing 'j' instead of 'g' should not match sequence,
	// but should match 'j' as scroll_down.
	action, pending := km.Resolve("g", "j")
	if action != ScrollDown {
		t.Errorf("expected abandoned sequence + 'j' -> scroll_down, got %q", action)
	}
	if pending != "" {
		t.Errorf("expected no pending, got %q", pending)
	}
}

func TestSpecialKeyBindings(t *testing.T) {
	km := New()

	// Enter -> follow_link.
	action, _ := km.Resolve("", "Enter")
	if action != FollowLink {
		t.Errorf("expected Enter -> follow_link, got %q", action)
	}

	// Tab -> next_link.
	action, _ = km.Resolve("", "Tab")
	if action != NextLink {
		t.Errorf("expected Tab -> next_link, got %q", action)
	}

	// Ctrl-F -> page_down.
	action, _ = km.Resolve("", "Ctrl-F")
	if action != PageDown {
		t.Errorf("expected Ctrl-F -> page_down, got %q", action)
	}

	// Ctrl-O -> search_web.
	action, _ = km.Resolve("", "Ctrl-O")
	if action != SearchWeb {
		t.Errorf("expected Ctrl-O -> search_web, got %q", action)
	}

	// Space -> page_down.
	action, _ = km.Resolve("", "Space")
	if action != PageDown {
		t.Errorf("expected Space -> page_down, got %q", action)
	}

	// Backspace -> back.
	action, _ = km.Resolve("", "Backspace")
	if action != Back {
		t.Errorf("expected Backspace -> back, got %q", action)
	}

	// R -> reload (alias for r).
	action, _ = km.Resolve("", "R")
	if action != Reload {
		t.Errorf("expected R -> reload, got %q", action)
	}

	// Y -> yank_link_url.
	action, _ = km.Resolve("", "Y")
	if action != YankLinkURL {
		t.Errorf("expected Y -> yank_link_url, got %q", action)
	}
}

func TestUnboundKey(t *testing.T) {
	km := New()

	action, pending := km.Resolve("", "z")
	if action != "" {
		t.Errorf("expected no action for unbound 'z', got %q", action)
	}
	if pending != "" {
		t.Errorf("expected no pending for unbound 'z', got %q", pending)
	}
}

func TestLoadFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	keymapPath := filepath.Join(tmpDir, "keymap.toml")

	// Write a custom keymap that overrides quit and adds a new binding.
	content := `
quit = ["x", "Q"]
back = ["Backspace"]
`
	if err := os.WriteFile(keymapPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	km, err := Load(keymapPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 'x' should now be quit.
	action, _ := km.Resolve("", "x")
	if action != Quit {
		t.Errorf("expected 'x' -> quit, got %q", action)
	}

	// 'Q' should also be quit.
	action, _ = km.Resolve("", "Q")
	if action != Quit {
		t.Errorf("expected 'Q' -> quit, got %q", action)
	}

	// 'q' should no longer be quit (overridden).
	action, _ = km.Resolve("", "q")
	if action == Quit {
		t.Error("expected 'q' to no longer be quit after override")
	}

	// Back should only be Backspace now (user removed b, B, H).
	action, _ = km.Resolve("", "Backspace")
	if action != Back {
		t.Errorf("expected Backspace -> back, got %q", action)
	}
	action, _ = km.Resolve("", "b")
	if action == Back {
		t.Error("expected 'b' to no longer be back after override")
	}

	// Non-overridden actions should keep defaults.
	action, _ = km.Resolve("", "j")
	if action != ScrollDown {
		t.Errorf("expected 'j' -> scroll_down (default), got %q", action)
	}
}

func TestLoadMissingFileCreatesDefault(t *testing.T) {
	tmpDir := t.TempDir()
	keymapPath := filepath.Join(tmpDir, "wez", "keymap.toml")

	km, err := Load(keymapPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Default bindings should work.
	action, _ := km.Resolve("", "q")
	if action != Quit {
		t.Errorf("expected default 'q' -> quit, got %q", action)
	}

	// File should have been created.
	if _, err := os.Stat(keymapPath); os.IsNotExist(err) {
		t.Error("expected keymap.toml to be auto-created")
	}
}

func TestEventToKeyString(t *testing.T) {
	tests := []struct {
		name     string
		key      tcell.Key
		rune     rune
		expected string
	}{
		{"rune q", tcell.KeyRune, 'q', "q"},
		{"rune G", tcell.KeyRune, 'G', "G"},
		{"rune /", tcell.KeyRune, '/', "/"},
		{"space", tcell.KeyRune, ' ', "Space"},
		{"enter", tcell.KeyEnter, 0, "Enter"},
		{"tab", tcell.KeyTab, 0, "Tab"},
		{"backtab", tcell.KeyBacktab, 0, "Shift-Tab"},
		{"backspace", tcell.KeyBackspace2, 0, "Backspace"},
		{"escape", tcell.KeyEscape, 0, "Escape"},
		{"up", tcell.KeyUp, 0, "Up"},
		{"down", tcell.KeyDown, 0, "Down"},
		{"left", tcell.KeyLeft, 0, "Left"},
		{"right", tcell.KeyRight, 0, "Right"},
		{"pgup", tcell.KeyPgUp, 0, "PgUp"},
		{"pgdn", tcell.KeyPgDn, 0, "PgDn"},
		{"ctrl-f", tcell.KeyCtrlF, 0, "Ctrl-F"},
		{"ctrl-o", tcell.KeyCtrlO, 0, "Ctrl-O"},
		{"ctrl-b", tcell.KeyCtrlB, 0, "Ctrl-B"},
		{"ctrl-d", tcell.KeyCtrlD, 0, "Ctrl-D"},
		{"ctrl-u", tcell.KeyCtrlU, 0, "Ctrl-U"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := tcell.NewEventKey(tt.key, tt.rune, tcell.ModNone)
			got := EventToKeyString(ev)
			if got != tt.expected {
				t.Errorf("EventToKeyString() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIsSequence(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"q", false},
		{"gg", true},
		{"gj", true},
		{"Enter", false},
		{"Ctrl-F", false},
		{"Shift-Tab", false},
		{"Space", false},
		{"PgDn", false},
		{"abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := isSequence(tt.key)
			if got != tt.expected {
				t.Errorf("isSequence(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestEnsureFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "sub", "keymap.toml")

	if err := EnsureFile(path); err != nil {
		t.Fatal(err)
	}

	// File should exist and be loadable.
	km, err := Load(path)
	if err != nil {
		t.Fatalf("failed to load created file: %v", err)
	}

	// Should have default bindings.
	action, _ := km.Resolve("", "q")
	if action != Quit {
		t.Errorf("expected default 'q' -> quit from created file, got %q", action)
	}
}
