package keymap

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/gdamore/tcell/v2"
)

// Action name constants used throughout the application.
const (
	Quit         = "quit"
	ScrollDown   = "scroll_down"
	ScrollUp     = "scroll_up"
	CursorLeft   = "cursor_left"
	CursorRight  = "cursor_right"
	PageDown     = "page_down"
	PageUp       = "page_up"
	HalfPageDown = "half_page_down"
	HalfPageUp   = "half_page_up"
	GoTop        = "go_top"
	GoBottom     = "go_bottom"
	Back         = "back"
	Forward      = "forward"
	OpenURL      = "open_url"
	OpenURLEdit  = "open_url_edit"
	FollowLink   = "follow_link"
	NextLink     = "next_link"
	PrevLink     = "prev_link"
	Search       = "search"
	SearchWeb    = "search_web"
	SearchNext   = "search_next"
	SearchPrev   = "search_prev"
	Reload       = "reload"
	OpenImage    = "open_image"
	YankURL      = "yank_url"
	VisualMode   = "visual_mode"
	VisualLine   = "visual_line_mode"
	OpenMailto   = "open_mailto"
)

// Keymap maps key presses to action names, supporting single keys and
// multi-key sequences (e.g. "gg").
type Keymap struct {
	singleKeys map[string]string // key string -> action
	sequences  map[string]string // multi-char sequence -> action
	seqStarts  map[string]bool   // first chars of known sequences
}

// DefaultBindings returns the built-in key bindings.
func DefaultBindings() map[string][]string {
	return map[string][]string{
		Quit:         {"q"},
		ScrollDown:   {"j", "Down"},
		ScrollUp:     {"k", "Up"},
		CursorLeft:   {"h", "Left"},
		CursorRight:  {"l", "Right"},
		PageDown:     {"Ctrl-F", "PgDn", "Space"},
		PageUp:       {"Ctrl-B", "PgUp"},
		HalfPageDown: {"Ctrl-D"},
		HalfPageUp:   {"Ctrl-U"},
		GoTop:        {"gg"},
		GoBottom:     {"G"},
		Back:         {"b", "B", "H", "Backspace"},
		Forward:      {"L"},
		OpenURL:      {"o"},
		OpenURLEdit:  {"O"},
		FollowLink:   {"Enter"},
		NextLink:     {"Tab"},
		PrevLink:     {"Shift-Tab"},
		Search:       {"/"},
		SearchWeb:    {"Ctrl-O"},
		SearchNext:   {"n"},
		SearchPrev:   {"N"},
		Reload:       {"r", "R"},
		OpenImage:    {"i"},
		YankURL:      {"y"},
		VisualMode:   {"v"},
		VisualLine:   {"V"},
	}
}

// New creates a Keymap with the default bindings.
func New() *Keymap {
	return buildKeymap(DefaultBindings())
}

// Load reads a keymap TOML file and merges it with the defaults.
// Missing actions in the user file keep their default bindings.
// If the file does not exist, it is created with defaults.
func Load(path string) (*Keymap, error) {
	defaults := DefaultBindings()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if writeErr := EnsureFile(path); writeErr != nil {
			// Non-fatal: proceed with defaults.
		}
		return buildKeymap(defaults), nil
	}

	var userBindings map[string][]string
	if _, err := toml.DecodeFile(path, &userBindings); err != nil {
		return buildKeymap(defaults), err
	}

	// Merge: user entries override defaults for their actions.
	for action, keys := range userBindings {
		defaults[action] = keys
	}

	return buildKeymap(defaults), nil
}

// Resolve determines the action for a key press given any pending sequence.
// Returns (actionName, newPendingState).
// Empty actionName means no action matched.
func (km *Keymap) Resolve(pending string, currentKey string) (action string, newPending string) {
	if pending != "" {
		seq := pending + currentKey
		if act, ok := km.sequences[seq]; ok {
			return act, ""
		}
		// Sequence didn't complete. Discard pending and fall through
		// to try current key as a standalone press.
	}

	// Check if current key starts a sequence.
	if km.seqStarts[currentKey] {
		return "", currentKey
	}

	// Check single key binding.
	if act, ok := km.singleKeys[currentKey]; ok {
		return act, ""
	}

	return "", ""
}

// EventToKeyString converts a tcell key event to the canonical key string
// used in keymap definitions.
func EventToKeyString(ev *tcell.EventKey) string {
	// Named special keys first — these overlap with some Ctrl codes
	// (e.g. KeyEnter == KeyCtrlM) so they must be checked before the
	// generic Ctrl-letter range.
	switch ev.Key() {
	case tcell.KeyEnter:
		return "Enter"
	case tcell.KeyTab:
		return "Tab"
	case tcell.KeyBacktab:
		return "Shift-Tab"
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return "Backspace"
	case tcell.KeyEscape:
		return "Escape"
	case tcell.KeyUp:
		return "Up"
	case tcell.KeyDown:
		return "Down"
	case tcell.KeyLeft:
		return "Left"
	case tcell.KeyRight:
		return "Right"
	case tcell.KeyPgUp:
		return "PgUp"
	case tcell.KeyPgDn:
		return "PgDn"
	case tcell.KeyHome:
		return "Home"
	case tcell.KeyEnd:
		return "End"
	case tcell.KeyDelete:
		return "Delete"
	case tcell.KeyInsert:
		return "Insert"
	}

	// Printable runes.
	if ev.Key() == tcell.KeyRune {
		r := ev.Rune()
		if r == ' ' {
			return "Space"
		}
		return string(r)
	}

	// Ctrl-A through Ctrl-Z (values 1–26), excluding aliases already
	// handled above (H=Backspace, I=Tab, M=Enter).
	if ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		ch := 'A' + rune(ev.Key()-tcell.KeyCtrlA)
		return "Ctrl-" + string(ch)
	}

	return ""
}

// EnsureFile creates the keymap file at path with default contents
// if it does not already exist.
func EnsureFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(defaultKeymapTOML), 0o644)
}

// --- internal ---

func buildKeymap(bindings map[string][]string) *Keymap {
	km := &Keymap{
		singleKeys: make(map[string]string),
		sequences:  make(map[string]string),
		seqStarts:  make(map[string]bool),
	}

	for action, keys := range bindings {
		for _, key := range keys {
			if isSequence(key) {
				km.sequences[key] = action
				first := string([]rune(key)[0])
				km.seqStarts[first] = true
			} else {
				km.singleKeys[key] = action
			}
		}
	}

	return km
}

// isSequence returns true if the key descriptor represents a multi-key
// sequence (e.g. "gg") rather than a single key or named key.
func isSequence(key string) bool {
	if len(key) <= 1 {
		return false
	}
	// Named keys and modifier combos are not sequences.
	named := map[string]bool{
		"Enter": true, "Tab": true, "Shift-Tab": true,
		"Space": true, "Backspace": true, "Escape": true,
		"Up": true, "Down": true, "Left": true, "Right": true,
		"PgUp": true, "PgDn": true, "Home": true, "End": true,
		"Delete": true, "Insert": true,
	}
	if named[key] {
		return false
	}
	if strings.HasPrefix(key, "Ctrl-") || strings.HasPrefix(key, "Shift-") {
		return false
	}
	return true
}

const defaultKeymapTOML = `# wez keymap configuration
#
# Each action maps to one or more key bindings.
# Missing entries fall back to built-in defaults.
#
# Key notation:
#   Single characters : "q", "j", "G", "/"
#   Special keys      : "Enter", "Tab", "Space", "Backspace", "Escape"
#   Arrow keys        : "Up", "Down", "Left", "Right"
#   Page keys         : "PgUp", "PgDn"
#   Modified keys     : "Ctrl-F", "Ctrl-B", "Shift-Tab"
#   Sequences         : "gg" (press g twice)

quit           = ["q"]
scroll_down    = ["j", "Down"]
scroll_up      = ["k", "Up"]
cursor_left    = ["h", "Left"]
cursor_right   = ["l", "Right"]
page_down      = ["Ctrl-F", "PgDn", "Space"]
page_up        = ["Ctrl-B", "PgUp"]
half_page_down = ["Ctrl-D"]
half_page_up   = ["Ctrl-U"]
go_top         = ["gg"]
go_bottom      = ["G"]
back           = ["b", "B", "H", "Backspace"]
forward        = ["L"]
open_url       = ["o"]
open_url_edit  = ["O"]
follow_link    = ["Enter"]
next_link      = ["Tab"]
prev_link      = ["Shift-Tab"]
search         = ["/"]
search_web     = ["Ctrl-O"]
search_next    = ["n"]
search_prev    = ["N"]
reload         = ["r", "R"]
open_image     = ["i"]
yank_url       = ["y"]
visual_mode    = ["v"]
visual_line_mode = ["V"]
`
