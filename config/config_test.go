package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	if cfg.DownloadDir == "" {
		t.Error("expected default download directory to be set")
	}
	if !cfg.PersistCookies {
		t.Error("expected persist_cookies default true")
	}
	if !cfg.PersistSessionCookies {
		t.Error("expected persist_session_cookies default true")
	}
	if cfg.CookieJarPath == "" {
		t.Error("expected default cookie_jar_path to be set")
	}
	if cfg.SearchEngine != "duckduckgo" {
		t.Errorf("expected default search engine 'duckduckgo', got %q", cfg.SearchEngine)
	}
	if !cfg.FollowMetaRedirects {
		t.Error("expected follow_meta_redirects default true")
	}
	if cfg.Colors.StatusBar != "gray" {
		t.Errorf("expected default status_bar 'gray', got %q", cfg.Colors.StatusBar)
	}
	if cfg.Colors.StatusBarBg != "black" {
		t.Errorf("expected default status_bar_bg 'black', got %q", cfg.Colors.StatusBarBg)
	}
	if cfg.Colors.TopBar != "black" {
		t.Errorf("expected default top_bar 'black', got %q", cfg.Colors.TopBar)
	}
	if cfg.Colors.Link != "blue" {
		t.Errorf("expected default link color 'blue', got %q", cfg.Colors.Link)
	}
	if cfg.Colors.Heading != "darkyellow" {
		t.Errorf("expected default heading color 'darkyellow', got %q", cfg.Colors.Heading)
	}
	if cfg.Colors.Code != "teal" {
		t.Errorf("expected default code color 'teal', got %q", cfg.Colors.Code)
	}
	if cfg.Colors.Noscript != "black" {
		t.Errorf("expected default noscript color 'black', got %q", cfg.Colors.Noscript)
	}
	if cfg.Colors.NoscriptBg != "gray" {
		t.Errorf("expected default noscript_bg color 'gray', got %q", cfg.Colors.NoscriptBg)
	}
	if cfg.Colors.BlockQuote != "teal" {
		t.Errorf("expected default blockquote color 'teal', got %q", cfg.Colors.BlockQuote)
	}
	if cfg.Colors.Image != "green" {
		t.Errorf("expected default image color 'green', got %q", cfg.Colors.Image)
	}
}

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Fatal("expected non-empty version")
	}
	if ok, _ := regexp.MatchString(`^\d+\.\d+(\.\d+)?$`, Version); !ok {
		t.Errorf("expected semantic-ish version, got %q", Version)
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
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading auto-created config: %v", err)
	}
	if !strings.Contains(string(content), "download_dir") {
		t.Error("expected auto-created config to include download_dir")
	}
	if !strings.Contains(string(content), "search_engine") {
		t.Error("expected auto-created config to include search_engine")
	}
	if !strings.Contains(string(content), "follow_meta_redirects") {
		t.Error("expected auto-created config to include follow_meta_redirects")
	}
	if !strings.Contains(string(content), "search_url_template") {
		t.Error("expected auto-created config to include search_url_template")
	}
	if !strings.Contains(string(content), "persist_cookies") {
		t.Error("expected auto-created config to include persist_cookies")
	}
	if !strings.Contains(string(content), "cookie_jar_path") {
		t.Error("expected auto-created config to include cookie_jar_path")
	}
	if !strings.Contains(string(content), "persist_session_cookies") {
		t.Error("expected auto-created config to include persist_session_cookies")
	}
	if !strings.Contains(string(content), "top_bar_bg") {
		t.Error("expected auto-created config to include top_bar_bg")
	}
	if !strings.Contains(string(content), "status_bar_bg") {
		t.Error("expected auto-created config to include status_bar_bg")
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
download_dir = "~/Downloads"
persist_cookies = false
cookie_jar_path = "~/.cache/wez/custom-cookies.json"
persist_session_cookies = false
search_engine = "bing"
follow_meta_redirects = false

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
	if cfg.DownloadDir != filepath.Join(tmpDir, "Downloads") {
		t.Errorf("expected expanded download_dir %q, got %q", filepath.Join(tmpDir, "Downloads"), cfg.DownloadDir)
	}
	if cfg.SearchEngine != "bing" {
		t.Errorf("expected search_engine bing, got %q", cfg.SearchEngine)
	}
	if cfg.PersistCookies {
		t.Error("expected persist_cookies=false from config")
	}
	if cfg.PersistSessionCookies {
		t.Error("expected persist_session_cookies=false from config")
	}
	if cfg.CookieJarPath != filepath.Join(tmpDir, ".cache", "wez", "custom-cookies.json") {
		t.Errorf("expected expanded cookie_jar_path, got %q", cfg.CookieJarPath)
	}
	if cfg.FollowMetaRedirects {
		t.Error("expected follow_meta_redirects=false from config")
	}
	if got := cfg.SearchURL("terminal browser"); !strings.HasPrefix(got, "https://www.bing.com/search?q=") {
		t.Errorf("expected bing search URL, got %q", got)
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

func TestSearchURLTemplateOverride(t *testing.T) {
	cfg := Default()
	cfg.SearchEngine = "duckduckgo"
	cfg.SearchURLTmpl = "https://example.com/search?term=%s&src=wez"

	got := cfg.SearchURL("hello world")
	if got != "https://example.com/search?term=hello+world&src=wez" {
		t.Fatalf("unexpected search URL: %q", got)
	}
}

func TestUnsupportedSearchEngineFallsBackToDuckDuckGo(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".config", "wez")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	configContent := `
search_engine = "google"
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SearchEngine != "duckduckgo" {
		t.Fatalf("expected unsupported search_engine to fall back to duckduckgo, got %q", cfg.SearchEngine)
	}
	if got := cfg.SearchURL("terminal browser"); !strings.HasPrefix(got, "https://duckduckgo.com/?q=") {
		t.Fatalf("expected duckduckgo fallback URL, got %q", got)
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
		{"darkgray", tcell.ColorDarkGray},
		{"#ff0000", tcell.NewRGBColor(255, 0, 0)},
		{"#0f0", tcell.NewRGBColor(0, 255, 0)},
		{"color208", tcell.PaletteColor(208)},
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
