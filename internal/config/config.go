package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/gdamore/tcell/v2"
)

const Version = "1.0"

type Config struct {
	ImageViewer   string      `toml:"image_viewer"`
	MailtoHandler string      `toml:"mailto_handler"`
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
	URLBar      string `toml:"url_bar"`
	StatusBar   string `toml:"status_bar"`
}

func Default() Config {
	return Config{
		ImageViewer:   "viu %s",
		MailtoHandler: "open mailto:%s",
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
			URLBar:      "white",
			StatusBar:   "white",
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

	return cfg, nil
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
	switch name {
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

func ensureConfigFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfigTOML), 0o644)
}

const defaultConfigTOML = `# wez terminal browser configuration

# External image viewer command. %s is replaced with the image file path.
image_viewer = "viu %s"

# Command to handle mailto: links. %s is replaced with the email address.
# macOS:  "open mailto:%s"
# Linux:  "xdg-open mailto:%s"
mailto_handler = "open mailto:%s"

[colors]
# Available colors: black, red, green, yellow, blue, purple, cyan, white,
# gray, darkred, darkgreen, orange, navy, teal
link        = "blue"
visited_link = "purple"
heading     = "yellow"
code        = "green"
noscript    = "white"
noscript_bg = "red"
blockquote  = "gray"
image       = "yellow"
hrule       = "gray"
url_bar     = "white"
status_bar  = "white"
`
