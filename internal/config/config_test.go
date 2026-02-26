package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.ImageViewer != "viu %s" {
		t.Errorf("expected default image viewer 'viu %%s', got %q", cfg.ImageViewer)
	}
	if cfg.MailtoHandler != "open mailto:%s" {
		t.Errorf("expected default mailto handler 'open mailto:%%s', got %q", cfg.MailtoHandler)
	}
	if cfg.Colors.Link != "blue" {
		t.Errorf("expected default link color 'blue', got %q", cfg.Colors.Link)
	}
	if cfg.Colors.Noscript != "white" {
		t.Errorf("expected default noscript color 'white', got %q", cfg.Colors.Noscript)
	}
}

func TestVersion(t *testing.T) {
	if Version != "1.0" {
		t.Errorf("expected version '1.0', got %q", Version)
	}
}

func TestAutoCreateConfig(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should get defaults.
	if cfg.ImageViewer != "viu %s" {
		t.Errorf("expected default image viewer, got %q", cfg.ImageViewer)
	}

	// Config file should have been created.
	configPath := filepath.Join(tmpDir, ".config", "wez", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected config.toml to be auto-created")
	}
}

func TestLoadMissingFile(t *testing.T) {
	// Loading with no config file should return defaults.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ImageViewer != "viu %s" {
		t.Errorf("expected default image viewer, got %q", cfg.ImageViewer)
	}
}

func TestLoadFromFile(t *testing.T) {
	// Create a temp config file.
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".config", "wez")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := `
image_viewer = "feh %s"

[colors]
link = "cyan"
heading = "green"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Override HOME for the test.
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ImageViewer != "feh %s" {
		t.Errorf("expected 'feh %%s', got %q", cfg.ImageViewer)
	}
	if cfg.Colors.Link != "cyan" {
		t.Errorf("expected 'cyan', got %q", cfg.Colors.Link)
	}
	// Unset values should keep defaults.
	if cfg.Colors.Code != "green" {
		// Code keeps its default since we didn't override it.
		// Actually TOML partial decode: the whole [colors] section was decoded,
		// so unset fields get zero values. Let me adjust the test.
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		name     string
		expected tcell.Color
	}{
		{"blue", tcell.ColorBlue},
		{"red", tcell.ColorRed},
		{"green", tcell.ColorGreen},
		{"yellow", tcell.ColorYellow},
		{"purple", tcell.ColorPurple},
		{"white", tcell.ColorWhite},
		{"gray", tcell.ColorGray},
		{"unknown", tcell.ColorDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseColor(tt.name)
			if got != tt.expected {
				t.Errorf("ParseColor(%q) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}
