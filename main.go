package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"

	"wez/browser"
	"wez/config"
	"wez/keymap"
)

type cliOptions struct {
	showVersion bool
	dumpMode    bool
	url         string
}

func main() {
	opts, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintf(os.Stderr, "usage: wez [--version|-v] [--dump] [URL]\n")
		os.Exit(2)
	}

	if opts.showVersion {
		fmt.Printf("wez %s\n", config.Version)
		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: config load error: %v\n", err)
	}

	if opts.dumpMode {
		dumped, dumpErr := browser.DumpURL(cfg, opts.url, outputWidth())
		if dumpErr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", dumpErr)
			os.Exit(1)
		}
		fmt.Println(dumped)
		return
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

	b.Run(opts.url)
}

func parseCLI(args []string) (cliOptions, error) {
	var opts cliOptions

	for i := 0; i < len(args); i++ {
		a := strings.TrimSpace(args[i])
		switch {
		case a == "--version" || a == "-v":
			opts.showVersion = true

		case a == "--dump":
			opts.dumpMode = true

		case strings.HasPrefix(a, "--dump="):
			opts.dumpMode = true
			v := strings.TrimSpace(strings.TrimPrefix(a, "--dump="))
			if v != "" {
				opts.url = v
			}

		case strings.HasPrefix(a, "-"):
			return cliOptions{}, fmt.Errorf("unknown option %q", a)

		default:
			if opts.url != "" {
				return cliOptions{}, fmt.Errorf("multiple URLs provided")
			}
			opts.url = a
		}
	}

	if opts.dumpMode && opts.url == "" {
		return cliOptions{}, fmt.Errorf("--dump requires a URL")
	}

	return opts, nil
}

func outputWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	if cols := strings.TrimSpace(os.Getenv("COLUMNS")); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 0 {
			return n
		}
	}
	return 80
}
