package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gdamore/tcell/v2"
)

const Version = "1.0"

type Config struct {
	ImageViewer   string      `toml:"image_viewer"`
	MailtoHandler string      `toml:"mailto_handler"`
	DownloadDir   string      `toml:"download_dir"`
	SearchEngine  string      `toml:"search_engine"`
	SearchURLTmpl string      `toml:"search_url_template"`
	Colors        ColorConfig `toml:"colors"`
}

type ColorConfig struct {
	Link        string `toml:"link"`
	VisitedLink string `toml:"visited_link"`
	Heading     string `toml:"heading"`
	Code        string `toml:"code"`
	Noscript    string `toml:"noscript"`
	NoscriptBg  string `toml:"noscript_bg"`
	BlockQuote  string `toml:"blockquote"`
	Image       string `toml:"image"`
	HRule       string `toml:"hrule"`
	TopBar      string `toml:"top_bar"`
	TopBarBg    string `toml:"top_bar_bg"`
	StatusBar   string `toml:"status_bar"`
	StatusBarBg string `toml:"status_bar_bg"`
}

func Default() Config {
	return Config{
		ImageViewer:   "viu %s",
		MailtoHandler: "open mailto:%s",
		DownloadDir:   defaultDownloadDir(),
		SearchEngine:  "duckduckgo",
		SearchURLTmpl: "",
		Colors: ColorConfig{
			Link:        "blue",
			VisitedLink: "purple",
			Heading:     "yellow",
			Code:        "green",
			Noscript:    "white",
			NoscriptBg:  "red",
			BlockQuote:  "gray",
			Image:       "yellow",
			HRule:       "gray",
			TopBar:      "black",
			TopBarBg:    "white",
			StatusBar:   "white",
			StatusBarBg: "darkgray",
		},
	}
}

func Load() (Config, error) {
	cfg := Default()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return cfg, nil
	}

	configPath := filepath.Join(homeDir, ".config", "wez", "config.toml")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Auto-create config with defaults.
		if writeErr := ensureConfigFile(configPath); writeErr != nil {
			// Non-fatal, just use defaults.
		}
		return cfg, nil
	}

	_, err = toml.DecodeFile(configPath, &cfg)
	if err != nil {
		return cfg, err
	}
	applyColorDefaults(&cfg.Colors, Default().Colors)

	if cfg.DownloadDir == "" {
		cfg.DownloadDir = defaultDownloadDir()
	}
	cfg.DownloadDir = expandHomePath(cfg.DownloadDir)

	if cfg.SearchEngine == "" {
		cfg.SearchEngine = "duckduckgo"
	}
	if cfg.SearchURLTmpl == "" {
		cfg.SearchURLTmpl = searchTemplateForEngine(cfg.SearchEngine)
	}
	if cfg.SearchURLTmpl == "" {
		cfg.SearchURLTmpl = searchTemplateForEngine("duckduckgo")
	}

	return cfg, nil
}

func applyColorDefaults(dst *ColorConfig, def ColorConfig) {
	if dst.Link == "" {
		dst.Link = def.Link
	}
	if dst.VisitedLink == "" {
		dst.VisitedLink = def.VisitedLink
	}
	if dst.Heading == "" {
		dst.Heading = def.Heading
	}
	if dst.Code == "" {
		dst.Code = def.Code
	}
	if dst.Noscript == "" {
		dst.Noscript = def.Noscript
	}
	if dst.NoscriptBg == "" {
		dst.NoscriptBg = def.NoscriptBg
	}
	if dst.BlockQuote == "" {
		dst.BlockQuote = def.BlockQuote
	}
	if dst.Image == "" {
		dst.Image = def.Image
	}
	if dst.HRule == "" {
		dst.HRule = def.HRule
	}
	if dst.TopBar == "" {
		dst.TopBar = def.TopBar
	}
	if dst.TopBarBg == "" {
		dst.TopBarBg = def.TopBarBg
	}
	if dst.StatusBar == "" {
		dst.StatusBar = def.StatusBar
	}
	if dst.StatusBarBg == "" {
		dst.StatusBarBg = def.StatusBarBg
	}
}

func (c Config) SearchURL(query string) string {
	q := url.QueryEscape(strings.TrimSpace(query))
	tmpl := c.SearchURLTmpl
	if tmpl == "" {
		tmpl = searchTemplateForEngine(c.SearchEngine)
	}
	if tmpl == "" {
		tmpl = searchTemplateForEngine("duckduckgo")
	}

	if strings.Contains(tmpl, "%s") {
		return strings.Replace(tmpl, "%s", q, 1)
	}

	if strings.Contains(tmpl, "?") {
		return tmpl + "&q=" + q
	}
	return tmpl + "?q=" + q
}

func searchTemplateForEngine(engine string) string {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "ddg", "duckduckgo", "duck":
		return "https://duckduckgo.com/?q=%s"
	case "google", "goog":
		return "https://www.google.com/search?q=%s"
	case "bing", "bling":
		return "https://www.bing.com/search?q=%s"
	case "yahoo":
		return "https://search.yahoo.com/search?p=%s"
	case "brave":
		return "https://search.brave.com/search?q=%s"
	case "ecosia":
		return "https://www.ecosia.org/search?q=%s"
	case "startpage":
		return "https://www.startpage.com/sp/search?query=%s"
	case "qwant":
		return "https://www.qwant.com/?q=%s"
	default:
		return ""
	}
}

func ConfigDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".config", "wez")
}

func CacheDir() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".cache", "wez")
}

// ParseColor converts a color name string to a tcell.Color.
func ParseColor(name string) tcell.Color {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, "#") {
		if c, ok := parseHexColor(n); ok {
			return c
		}
	}
	if strings.HasPrefix(n, "color") {
		if idx, err := strconv.Atoi(strings.TrimPrefix(n, "color")); err == nil && idx >= 0 && idx <= 255 {
			return tcell.PaletteColor(idx)
		}
	}

	switch n {
	case "black":
		return tcell.ColorBlack
	case "red":
		return tcell.ColorRed
	case "green":
		return tcell.ColorGreen
	case "yellow":
		return tcell.ColorYellow
	case "blue":
		return tcell.ColorBlue
	case "purple", "magenta":
		return tcell.ColorPurple
	case "cyan":
		return tcell.ColorTeal
	case "white":
		return tcell.ColorWhite
	case "gray", "grey":
		return tcell.ColorGray
	case "darkgray", "darkgrey":
		return tcell.ColorDarkGray
	case "darkred":
		return tcell.ColorDarkRed
	case "darkgreen":
		return tcell.ColorDarkGreen
	case "darkyellow", "olive":
		return tcell.ColorOlive
	case "darkblue", "navy":
		return tcell.ColorNavy
	case "darkmagenta":
		return tcell.ColorDarkMagenta
	case "darkcyan", "teal":
		return tcell.ColorTeal
	case "orange":
		return tcell.ColorOrange
	default:
		return tcell.ColorDefault
	}
}

func parseHexColor(s string) (tcell.Color, bool) {
	if len(s) == 7 {
		r, errR := strconv.ParseInt(s[1:3], 16, 32)
		g, errG := strconv.ParseInt(s[3:5], 16, 32)
		b, errB := strconv.ParseInt(s[5:7], 16, 32)
		if errR != nil || errG != nil || errB != nil {
			return tcell.ColorDefault, false
		}
		return tcell.NewRGBColor(int32(r), int32(g), int32(b)), true
	}

	if len(s) == 4 {
		r, errR := strconv.ParseInt(strings.Repeat(string(s[1]), 2), 16, 32)
		g, errG := strconv.ParseInt(strings.Repeat(string(s[2]), 2), 16, 32)
		b, errB := strconv.ParseInt(strings.Repeat(string(s[3]), 2), 16, 32)
		if errR != nil || errG != nil || errB != nil {
			return tcell.ColorDefault, false
		}
		return tcell.NewRGBColor(int32(r), int32(g), int32(b)), true
	}

	return tcell.ColorDefault, false
}

func ensureConfigFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfigTOML(Default())), 0o644)
}

func defaultDownloadDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return "."
	}

	downloads := filepath.Join(homeDir, "Downloads")
	if info, err := os.Stat(downloads); err == nil && info.IsDir() {
		return downloads
	}

	return homeDir
}

func expandHomePath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return path
	}

	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}

	return path
}

func shortHome(path string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return path
	}
	if path == homeDir {
		return "~"
	}
	if strings.HasPrefix(path, homeDir+string(os.PathSeparator)) {
		return "~" + path[len(homeDir):]
	}
	return path
}

func defaultConfigTOML(cfg Config) string {
	return fmt.Sprintf(`# wez terminal browser configuration

# External image viewer command. %%s is replaced with the image file path.
image_viewer = "viu %%s"

# Command to handle mailto: links. %%s is replaced with the email address.
# macOS:  "open mailto:%%s"
# Linux:  "xdg-open mailto:%%s"
mailto_handler = "open mailto:%%s"

# Directory where downloaded binary files (e.g. PDF, ZIP) are saved.
download_dir = %q

# Search engine used by the web-search prompt (Ctrl-O by default).
# Built-in options: ddg, duckduckgo, google, bing, yahoo, brave,
# ecosia, startpage, qwant
search_engine = %q

# Optional custom search URL template. %%s is replaced with URL-escaped query text.
# Example: "https://duckduckgo.com/?q=%%s"
search_url_template = %q

[colors]
# Available colors: black, red, green, yellow, blue, purple, cyan, white,
# gray, darkgray, darkred, darkgreen, orange, navy, teal
# Also supported: #RRGGBB, #RGB, and 256-color palette values like "color208"
link        = "blue"
visited_link = "purple"
heading     = "yellow"
code        = "green"
noscript    = "white"
noscript_bg = "red"
blockquote  = "gray"
image       = "yellow"
hrule       = "gray"
top_bar     = "black"
top_bar_bg  = "white"
status_bar  = "white"
status_bar_bg = "darkgray"
`, shortHome(cfg.DownloadDir), cfg.SearchEngine, cfg.SearchURLTmpl)
}
