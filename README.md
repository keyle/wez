# wez

`wez` is a keyboard-first terminal web browser.

It aims to be fast and readable in a text UI, with practical browsing features and minimal setup.

## Highlights

- Readable HTML rendering in TUI (based on tcell) 
- vim-like bindings, search, link/form activation, and key remapping
- downloads for binary responses
- history view
- bookmarks with categories
- cache, persistent cookies, visited links
- favourites
- view source
- no JavaScript runtime
- form handling, basic
- yanking (copy), v/V visual support, mouse support
- images preview with external tool support, e.g. `viu`

## Build

```bash
make all
make install
```

## Usage

```bash
wez
wez news.ycombinator.com
wez --dump https://example.com
```

## Config

these files are auto created on first start.

- config: `~/.config/wez/config.toml`
- keymap: `~/.config/wez/keymap.toml`
- cache: `~/.cache/wez/`

the config and keymap files are self-documentating, if not, let me know.

## Method

Much of this repo was built using agentic coding, with `Opencode`. First pass with `Opus 4.6` on `high`, then `Codex 5.3` on `high/xhigh`. 

I could hand-write it all but it does it much faster than me and never gets tired. I _do_ read and take ownership of the code. If you see slop, it's my fault!

## License

Apache-2.0.
