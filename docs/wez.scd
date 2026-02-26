WEZ(1)

# NAME

wez - modern terminal web browser

# SYNOPSIS

*wez* [_URL_]

*wez* *--version*

*wez* *-v*

# DESCRIPTION

wez is a modern, keyboard-driven terminal web browser.

It focuses on readable output and fast navigation: basic HTML rendering,
vim-style movement, link following, in-page search, external image viewing,
downloads for binary responses, clipboard yanking, and configurable keymaps.

JavaScript is not executed.
Search engines that require JavaScript (for example Google Search) are not supported.

# OPTIONS

*--version*, *-v*
	Print version and exit.

URL
	Optional URL (or hostname) to open at startup.

# INSTALLATION

Build from source:

	go build -o wez .

Optional system install:

	install -m755 wez /usr/local/bin/wez

Run with:

	wez

# REQUIREMENTS

Required:

- A terminal with ANSI/UTF-8 support
- Network access

Optional helpers:

- External image viewer command (default: `viu %s`)
- Clipboard helpers:
	- macOS: `pbcopy`
	- Linux: `wl-copy`, `xclip`, or `xsel`

# EXAMPLES

Open the welcome page:

	wez

Open a site:

	wez https://news.ycombinator.com

Open with implicit https:

	wez example.com

Print version:

	wez --version

# KEY BINDINGS

Default normal-mode bindings (subset):

- `o` / `O` - open URL bar (empty / prefilled)
- `Ctrl-O` - open web-search prompt
- `j` / `k` - scroll down / up
- `h` / `l` - move cursor left / right
- `gg` / `G` - jump top / bottom
- `Ctrl-F` / `Ctrl-B` - page down / up
- `Ctrl-D` / `Ctrl-U` - half-page down / up
- `Tab` / `Shift-Tab` - next / previous link
- `Enter` - follow link under cursor
- `b` / `B` / `H` / `Backspace` - back
- `L` - forward
- `r` / `R` - reload
- `/` then `n` / `N` - search, next / previous match
- `i` - open image under cursor (external viewer)
- `v` / `V` - visual / visual-line selection
- `y` - yank selection (or current line)
- `Y` - yank link URL under cursor
- `q` - quit

All bindings are remappable in `~/.config/wez/keymap.toml`.

# FILES

- `~/.config/wez/config.toml`
	Main configuration file (auto-created with defaults).

- `~/.config/wez/keymap.toml`
	Key bindings (auto-created with defaults).

- `~/.cache/wez/history`
	Persisted browsing history.

# AUTHORS

keyle, 2026

# LICENSE

Apache-2.0
