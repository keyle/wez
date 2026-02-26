WEZ(1)

# NAME

wez - modern terminal web browser

# SYNOPSIS

*wez* [_URL_]

*wez* *--version*

*wez* *-v*

*wez* *--dump* _URL_

# DESCRIPTION

wez is a modern, keyboard-driven terminal web browser.

It focuses on readable output and fast navigation: basic HTML rendering,
vim-style movement, link following, in-page search, external image viewing,
downloads for binary responses, clipboard yanking, simple HTML form interaction,
favorites with categories,
and configurable keymaps.

JavaScript is not executed.
Search engines that require JavaScript (for example Google Search) are not supported.

Form support includes text/password fields, textarea, select (single/multiple),
checkboxes/radios, and submit buttons. Text-like controls are edited in the action bar.

Cookies are persisted by default in `~/.cache/wez/cookies.json` so logins can survive restarts.

# OPTIONS

*--version*, *-v*
	Print version and exit.

*--dump* _URL_
	Fetch and render _URL_ to stdout without starting the TUI.
	Output is plain text in the same rendered layout style used in wez.

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

Dump rendered page content to stdout:

	wez --dump https://example.com

# KEY BINDINGS

Default normal-mode bindings (subset):

- `o` / `O` - open action bar for URL input (empty / prefilled)
- `Ctrl-O` - open web-search prompt
- `j` / `k` - scroll down / up
- `h` / `l` - move cursor left / right
- `gg` / `G` - jump top / bottom
- `Ctrl-F` / `PgUp` - page down / up
- `d` / `u` - half-page down / up
- `Tab` / `Shift-Tab` - next / previous focus target (link or form control)
- `Enter` - activate link/control under cursor
- `b` / `B` / `Backspace` - back
- `Ctrl-W` - open welcome page
- `Ctrl-H` - open history view (Esc to return)
- `Ctrl-B` - open bookmarks view (Esc to return)
- `Ctrl-U` - show page source (Esc to return)
- `L` - forward
- `r` / `R` - reload
- `za` / `zd` - add / remove favorite
- `zc` / `zh` - clear cache / clear history
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

- `~/.config/wez/fav.json`
	Persisted favorites with 1-level categories.

- `~/.cache/wez/history`
	Persisted browsing history.

- `~/.cache/wez/seen_urls`
	Persisted set of visited URLs for visited-link coloring.

- `~/.cache/wez/cookies.json`
	Persisted cookie jar (enabled by default).

# AUTHORS

keyle, 2026

# LICENSE

Apache-2.0
