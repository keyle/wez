package render

import "github.com/mattn/go-runewidth"

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
	Text     string
	Style    SpanStyle
	LinkIdx  int    // -1 if not a link, otherwise index into Document.Links
	ImageURL string // non-empty for image placeholders
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

// Document is the fully rendered page ready for display.
type Document struct {
	Title string
	URL   string
	Lines []Line
	Links []Link
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
		if col >= x && col < x+w && span.LinkIdx >= 0 {
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
