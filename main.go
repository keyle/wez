package main

import (
	"fmt"
	"os"
	"path/filepath"

	"wez/internal/browser"
	"wez/internal/config"
	"wez/internal/keymap"
)

func main() {
	// Handle --version before anything else.
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("wez %s\n", config.Version)
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: config load error: %v\n", err)
	}

	keymapPath := filepath.Join(config.ConfigDir(), "keymap.toml")
	km, err := keymap.Load(keymapPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: keymap load error: %v\n", err)
		km = keymap.New()
	}

	b, err := browser.New(cfg, km)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// If a URL was provided on the command line, navigate to it.
	var initialURL string
	if len(os.Args) > 1 {
		initialURL = os.Args[1]
	}

	b.Run(initialURL)
}
