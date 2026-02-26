package render

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

// SpanStyle describes visual attributes for a span of text.
type SpanStyle struct {
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Color     string // color name from config, e.g. "blue"
	BgColor   string
}

// Span is a run of text with uniform styling.
type Span struct {
	Text       string
	Style      SpanStyle
	LinkIdx    int    // -1 if not a link, otherwise index into Document.Links
	ControlIdx int    // -1 if not a form control, otherwise index into Document.Controls
	ImageURL   string // non-empty for image placeholders
}

// Line is a single rendered line composed of styled spans.
type Line struct {
	Spans []Span
}

// Width returns the display width of the line.
func (l Line) Width() int {
	w := 0
	for _, s := range l.Spans {
		w += runewidth.StringWidth(s.Text)
	}
	return w
}

// Link represents a navigable hyperlink in the document.
type Link struct {
	URL  string
	Line int // first line where link appears
	Col  int // column offset on that line
}

// Form describes a single HTML form.
type Form struct {
	Method   string
	Action   string
	Enctype  string
	Controls []int // indices into Document.Controls
}

// ControlOption describes one selectable option in a select control.
type ControlOption struct {
	Value    string
	Label    string
	Selected bool
	Disabled bool
}

// Control describes a single HTML form control.
type Control struct {
	Kind        string // input, textarea, select, button
	Type        string // text, password, checkbox, radio, submit, ...
	FormIdx     int
	Name        string
	Value       string
	Checked     bool
	Disabled    bool
	ReadOnly    bool
	Multiple    bool
	Options     []ControlOption
	DisplaySize int // visible field width hint

	Line  int // rendered line, -1 for non-visible controls
	Col   int // rendered column
	Width int // rendered width in cells
}

// Document is the fully rendered page ready for display.
type Document struct {
	Title    string
	URL      string
	Lines    []Line
	Links    []Link
	Forms    []Form
	Controls []Control
}

// LinkAt returns the link index and URL at the given document position.
// Returns (-1, "", false) if no link at that position.
func (d *Document) LinkAt(line, col int) (int, string, bool) {
	if line < 0 || line >= len(d.Lines) {
		return -1, "", false
	}
	x := 0
	for _, span := range d.Lines[line].Spans {
		w := runewidth.StringWidth(span.Text)
		if col >= x && col < x+w && span.LinkIdx >= 0 && span.LinkIdx < len(d.Links) {
			return span.LinkIdx, d.Links[span.LinkIdx].URL, true
		}
		x += w
	}
	return -1, "", false
}

// ImageAt returns the image URL at the given document position.
func (d *Document) ImageAt(line, col int) (string, bool) {
	if line < 0 || line >= len(d.Lines) {
		return "", false
	}
	x := 0
	for _, span := range d.Lines[line].Spans {
		w := runewidth.StringWidth(span.Text)
		if col >= x && col < x+w && span.ImageURL != "" {
			return span.ImageURL, true
		}
		x += w
	}
	return "", false
}

// ControlAt returns the form control index at the given document position.
func (d *Document) ControlAt(line, col int) (int, bool) {
	if line < 0 || line >= len(d.Lines) {
		return -1, false
	}
	x := 0
	for _, span := range d.Lines[line].Spans {
		w := runewidth.StringWidth(span.Text)
		if col >= x && col < x+w && span.ControlIdx >= 0 && span.ControlIdx < len(d.Controls) {
			return span.ControlIdx, true
		}
		x += w
	}
	return -1, false
}

// NextLink returns the index and position of the next link after the given position.
func (d *Document) NextLink(fromLine, fromCol int) (idx, line, col int, ok bool) {
	// Search from current position forward.
	for i, link := range d.Links {
		if link.Line > fromLine || (link.Line == fromLine && link.Col > fromCol) {
			return i, link.Line, link.Col, true
		}
	}
	// Wrap around to first link.
	if len(d.Links) > 0 {
		l := d.Links[0]
		return 0, l.Line, l.Col, true
	}
	return -1, 0, 0, false
}

// PrevLink returns the index and position of the previous link before the given position.
func (d *Document) PrevLink(fromLine, fromCol int) (idx, line, col int, ok bool) {
	for i := len(d.Links) - 1; i >= 0; i-- {
		link := d.Links[i]
		if link.Line < fromLine || (link.Line == fromLine && link.Col < fromCol) {
			return i, link.Line, link.Col, true
		}
	}
	// Wrap around to last link.
	if len(d.Links) > 0 {
		l := d.Links[len(d.Links)-1]
		return len(d.Links) - 1, l.Line, l.Col, true
	}
	return -1, 0, 0, false
}

// ControlDisplayText returns a terminal-friendly display label for a form control.
func ControlDisplayText(c Control) string {
	size := c.DisplaySize
	if size <= 0 {
		size = 20
	}

	switch c.Kind {
	case "textarea":
		return "[" + padOrTrim(c.Value, size) + "]"

	case "select":
		label := selectedOptionLabel(c)
		if c.Multiple {
			return "[select*: " + padOrTrim(label, size) + "]"
		}
		return "[select: " + padOrTrim(label, size) + "]"

	case "button":
		label := strings.TrimSpace(c.Value)
		if label == "" {
			label = "button"
		}
		return "[ " + label + " ]"

	case "input":
		switch c.Type {
		case "hidden":
			return ""
		case "checkbox":
			if c.Checked {
				return "[x]"
			}
			return "[ ]"
		case "radio":
			if c.Checked {
				return "(*)"
			}
			return "( )"
		case "submit", "button", "reset":
			label := strings.TrimSpace(c.Value)
			if label == "" {
				if c.Type == "submit" {
					label = "submit"
				} else {
					label = c.Type
				}
			}
			return "[ " + label + " ]"
		case "password":
			return "[" + padOrTrim(strings.Repeat("*", len([]rune(c.Value))), size) + "]"
		default:
			return "[" + padOrTrim(c.Value, size) + "]"
		}
	}

	return fmt.Sprintf("[%s]", padOrTrim(c.Value, size))
}

func padOrTrim(s string, width int) string {
	if width <= 0 {
		return s
	}
	w := runewidth.StringWidth(s)
	if w == width {
		return s
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return truncateToWidth(s, width)
}

func selectedOptionLabel(c Control) string {
	if len(c.Options) == 0 {
		if strings.TrimSpace(c.Value) != "" {
			return c.Value
		}
		return ""
	}

	labels := make([]string, 0, len(c.Options))
	for _, opt := range c.Options {
		if opt.Selected {
			label := strings.TrimSpace(opt.Label)
			if label == "" {
				label = strings.TrimSpace(opt.Value)
			}
			if label == "" {
				label = "(empty)"
			}
			labels = append(labels, label)
		}
	}
	if len(labels) == 0 {
		return ""
	}
	return strings.Join(labels, ",")
}
