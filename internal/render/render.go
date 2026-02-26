package render

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Render parses raw HTML and produces a Document ready for terminal display.
func Render(htmlBytes []byte, pageURL string, width int) *Document {
	doc, err := html.Parse(strings.NewReader(string(htmlBytes)))
	if err != nil {
		return &Document{
			Title: "Parse Error",
			URL:   pageURL,
			Lines: []Line{{Spans: []Span{{Text: "Error parsing HTML: " + err.Error()}}}},
		}
	}

	r := &renderer{
		width:      width,
		curLinkIdx: -1,
		pageURL:    pageURL,
	}

	// Extract title.
	r.title = findText(doc, "title")

	// Find body; fall back to whole document.
	body := findElement(doc, "body")
	if body == nil {
		body = doc
	}

	r.walkChildren(body)
	r.flushLine()

	return &Document{
		Title: r.title,
		URL:   pageURL,
		Lines: r.lines,
		Links: r.links,
	}
}

// RenderPlainText produces a Document from plain text content.
func RenderPlainText(text []byte, pageURL string, width int) *Document {
	lines := strings.Split(string(text), "\n")
	var docLines []Line
	for _, line := range lines {
		docLines = append(docLines, Line{
			Spans: []Span{{Text: line, LinkIdx: -1}},
		})
	}
	return &Document{
		Title: pageURL,
		URL:   pageURL,
		Lines: docLines,
	}
}

type listCtx struct {
	ordered bool
	counter int
}

type renderer struct {
	// Output
	lines []Line
	links []Link
	title string

	// Current line being built
	curSpans []Span
	curCol   int

	// Layout
	width  int
	indent int

	// Whitespace
	pendingSpace bool
	preformatted bool

	// Style context
	bold      bool
	italic    bool
	underline bool
	strike    bool
	color     string
	bgColor   string

	// Link context
	curLinkIdx int // -1 if not in a link

	// List context
	listStack []listCtx

	// Page URL for resolving relative links.
	pageURL string
}

func (r *renderer) currentStyle() SpanStyle {
	return SpanStyle{
		Bold:      r.bold,
		Italic:    r.italic,
		Underline: r.underline,
		Strike:    r.strike,
		Color:     r.color,
		BgColor:   r.bgColor,
	}
}

func (r *renderer) appendToLine(text string) {
	if text == "" {
		return
	}
	style := r.currentStyle()
	linkIdx := r.curLinkIdx
	imageURL := ""

	// Merge with last span if same attributes.
	if len(r.curSpans) > 0 {
		last := &r.curSpans[len(r.curSpans)-1]
		if last.Style == style && last.LinkIdx == linkIdx && last.ImageURL == "" {
			last.Text += text
			r.curCol += runewidth.StringWidth(text)
			return
		}
	}

	r.curSpans = append(r.curSpans, Span{
		Text:     text,
		Style:    style,
		LinkIdx:  linkIdx,
		ImageURL: imageURL,
	})
	r.curCol += runewidth.StringWidth(text)
}

func (r *renderer) flushLine() {
	if len(r.curSpans) > 0 {
		r.lines = append(r.lines, Line{Spans: r.curSpans})
	}
	r.curSpans = nil
	r.curCol = r.indent
	r.pendingSpace = false
}

func (r *renderer) ensureBlankLine() {
	r.flushLine()
	// Add blank line if last line is not already blank.
	if len(r.lines) > 0 {
		last := r.lines[len(r.lines)-1]
		if len(last.Spans) > 0 {
			r.lines = append(r.lines, Line{})
		}
	}
}

func (r *renderer) addText(text string) {
	if r.preformatted {
		r.addPreText(text)
		return
	}

	// Collapse whitespace: replace runs of whitespace with single space.
	hasLeadingSpace := len(text) > 0 && isWhitespace(rune(text[0]))
	hasTrailingSpace := len(text) > 0 && isWhitespace(rune(text[len(text)-1]))

	words := strings.Fields(text)
	if len(words) == 0 {
		// All whitespace.
		if hasLeadingSpace && r.curCol > r.indent {
			r.pendingSpace = true
		}
		return
	}

	for i, word := range words {
		needSpace := false
		if i == 0 {
			needSpace = (hasLeadingSpace || r.pendingSpace) && r.curCol > r.indent
		} else {
			needSpace = true
		}

		ww := runewidth.StringWidth(word)
		spaceW := 0
		if needSpace {
			spaceW = 1
		}

		// Wrap if the word won't fit.
		if r.curCol+spaceW+ww > r.width && r.curCol > r.indent {
			r.flushLine()
			// Apply indent.
			if r.indent > 0 {
				r.addIndent()
			}
			needSpace = false
		}

		if needSpace {
			r.appendToLine(" ")
		}

		// If word is wider than available width, break it.
		avail := r.width - r.curCol
		if ww > avail && avail > 0 {
			r.addLongWord(word)
		} else {
			r.appendToLine(word)
		}
	}

	r.pendingSpace = hasTrailingSpace
}

func (r *renderer) addPreText(text string) {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i > 0 {
			r.flushLine()
			if r.indent > 0 {
				r.addIndent()
			}
		}
		if line != "" {
			r.appendToLine(line)
		}
	}
}

func (r *renderer) addLongWord(word string) {
	runes := []rune(word)
	for len(runes) > 0 {
		avail := r.width - r.curCol
		if avail <= 0 {
			r.flushLine()
			if r.indent > 0 {
				r.addIndent()
			}
			avail = r.width - r.curCol
		}
		// Take as many runes as fit.
		take := 0
		w := 0
		for _, ru := range runes {
			rw := runewidth.RuneWidth(ru)
			if w+rw > avail {
				break
			}
			w += rw
			take++
		}
		if take == 0 {
			take = 1 // Always take at least one rune.
		}
		r.appendToLine(string(runes[:take]))
		runes = runes[take:]
		if len(runes) > 0 {
			r.flushLine()
			if r.indent > 0 {
				r.addIndent()
			}
		}
	}
}

func (r *renderer) addIndent() {
	if r.indent > 0 {
		r.appendToLine(strings.Repeat(" ", r.indent))
	}
}

func (r *renderer) walkChildren(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.walk(c)
	}
}

func (r *renderer) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		r.addText(n.Data)
	case html.ElementNode:
		r.handleElement(n)
	case html.DocumentNode:
		r.walkChildren(n)
	}
}

func (r *renderer) handleElement(n *html.Node) {
	tag := n.DataAtom
	tagName := n.Data

	switch tag {
	case atom.Script, atom.Style:
		return // Skip entirely.

	case atom.Head:
		return // Skip head, we already extracted the title.

	case atom.Noscript:
		r.handleNoscript(n)
		return

	case atom.Br:
		r.flushLine()
		if r.indent > 0 {
			r.addIndent()
		}

	case atom.Hr:
		r.ensureBlankLine()
		oldColor := r.color
		r.color = "hrule"
		avail := r.width - r.indent
		if avail < 1 {
			avail = 1
		}
		r.appendToLine(strings.Repeat("─", avail))
		r.color = oldColor
		r.ensureBlankLine()

	case atom.P, atom.Div, atom.Section, atom.Article, atom.Main,
		atom.Header, atom.Footer, atom.Nav, atom.Form, atom.Fieldset,
		atom.Figure, atom.Figcaption, atom.Details, atom.Summary,
		atom.Address:
		r.ensureBlankLine()
		if r.indent > 0 {
			r.addIndent()
		}
		r.walkChildren(n)
		r.ensureBlankLine()

	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		r.handleHeading(n, tagName)

	case atom.A:
		r.handleLink(n)

	case atom.Strong, atom.B:
		old := r.bold
		r.bold = true
		r.walkChildren(n)
		r.bold = old

	case atom.Em, atom.I:
		old := r.italic
		r.italic = true
		r.walkChildren(n)
		r.italic = old

	case atom.U, atom.Ins:
		old := r.underline
		r.underline = true
		r.walkChildren(n)
		r.underline = old

	case atom.S, atom.Del, atom.Strike:
		old := r.strike
		r.strike = true
		r.walkChildren(n)
		r.strike = old

	case atom.Code:
		old := r.color
		r.color = "code"
		r.walkChildren(n)
		r.color = old

	case atom.Pre:
		r.ensureBlankLine()
		oldPre := r.preformatted
		oldColor := r.color
		r.preformatted = true
		r.color = "code"
		if r.indent > 0 {
			r.addIndent()
		}
		r.walkChildren(n)
		r.preformatted = oldPre
		r.color = oldColor
		r.ensureBlankLine()

	case atom.Blockquote:
		r.handleBlockquote(n)

	case atom.Ul:
		r.handleList(n, false)

	case atom.Ol:
		r.handleList(n, true)

	case atom.Li:
		r.handleListItem(n)

	case atom.Dl:
		r.ensureBlankLine()
		r.walkChildren(n)
		r.ensureBlankLine()

	case atom.Dt:
		r.flushLine()
		old := r.bold
		r.bold = true
		if r.indent > 0 {
			r.addIndent()
		}
		r.walkChildren(n)
		r.bold = old
		r.flushLine()

	case atom.Dd:
		r.flushLine()
		r.indent += 4
		r.addIndent()
		r.walkChildren(n)
		r.indent -= 4
		r.flushLine()

	case atom.Table:
		r.handleTable(n)

	case atom.Img:
		r.handleImage(n)

	case atom.Sup:
		r.appendToLine("^")
		r.walkChildren(n)

	case atom.Sub:
		r.appendToLine("_")
		r.walkChildren(n)

	case atom.Abbr:
		r.walkChildren(n)
		title := getAttr(n, "title")
		if title != "" {
			r.appendToLine(" (" + title + ")")
		}

	default:
		// Unknown element: just process children.
		r.walkChildren(n)
	}
}

func (r *renderer) handleHeading(n *html.Node, tagName string) {
	r.ensureBlankLine()

	level := int(tagName[1] - '0')
	oldBold := r.bold
	oldColor := r.color
	r.bold = true
	r.color = "heading"

	prefix := strings.Repeat("#", level) + " "
	if r.indent > 0 {
		r.addIndent()
	}
	r.appendToLine(prefix)
	r.walkChildren(n)

	r.bold = oldBold
	r.color = oldColor
	r.ensureBlankLine()
}

func (r *renderer) handleLink(n *html.Node) {
	href := getAttr(n, "href")
	if href == "" {
		r.walkChildren(n)
		return
	}

	// Resolve relative URL.
	resolved := r.resolveURL(href)

	// Register link.
	linkIdx := len(r.links)
	r.links = append(r.links, Link{
		URL:  resolved,
		Line: len(r.lines),
		Col:  r.curCol,
	})

	oldColor := r.color
	oldUnderline := r.underline
	oldLinkIdx := r.curLinkIdx
	r.color = "link"
	r.underline = true
	r.curLinkIdx = linkIdx

	r.walkChildren(n)

	r.color = oldColor
	r.underline = oldUnderline
	r.curLinkIdx = oldLinkIdx
}

func (r *renderer) handleNoscript(n *html.Node) {
	r.ensureBlankLine()

	oldColor := r.color
	oldBg := r.bgColor
	oldBold := r.bold
	r.color = "noscript"
	r.bgColor = "noscript_bg"
	r.bold = true

	r.appendToLine(" [NOSCRIPT] ")
	r.walkChildren(n)
	r.appendToLine(" ")

	r.color = oldColor
	r.bgColor = oldBg
	r.bold = oldBold
	r.ensureBlankLine()
}

func (r *renderer) handleBlockquote(n *html.Node) {
	r.ensureBlankLine()
	oldIndent := r.indent
	oldColor := r.color
	r.indent += 4
	r.color = "blockquote"

	// Add quote marker.
	r.appendToLine(strings.Repeat(" ", oldIndent) + "  | ")
	r.walkChildren(n)

	r.indent = oldIndent
	r.color = oldColor
	r.ensureBlankLine()
}

func (r *renderer) handleList(n *html.Node, ordered bool) {
	r.ensureBlankLine()
	r.listStack = append(r.listStack, listCtx{ordered: ordered, counter: 0})
	r.walkChildren(n)
	r.listStack = r.listStack[:len(r.listStack)-1]
	r.flushLine()
}

func (r *renderer) handleListItem(n *html.Node) {
	r.flushLine()

	var bullet string
	if len(r.listStack) > 0 {
		ctx := &r.listStack[len(r.listStack)-1]
		if ctx.ordered {
			ctx.counter++
			bullet = strings.Repeat(" ", r.indent) + padRight(intToStr(ctx.counter)+".", 4)
		} else {
			bullet = strings.Repeat(" ", r.indent) + "  * "
		}
	} else {
		bullet = strings.Repeat(" ", r.indent) + "  * "
	}

	r.appendToLine(bullet)
	oldIndent := r.indent
	r.indent += 4
	r.walkChildren(n)
	r.indent = oldIndent
	r.flushLine()
}

func (r *renderer) handleImage(n *html.Node) {
	alt := getAttr(n, "alt")
	src := getAttr(n, "src")

	resolved := r.resolveURL(src)

	oldColor := r.color
	r.color = "image"

	label := "[IMG"
	if alt != "" {
		label += ": " + alt
	}
	label += "]"

	startCol := r.curCol
	span := Span{
		Text:     label,
		Style:    r.currentStyle(),
		LinkIdx:  r.curLinkIdx,
		ImageURL: resolved,
	}
	r.curSpans = append(r.curSpans, span)
	r.curCol += runewidth.StringWidth(label)
	_ = startCol

	r.color = oldColor
}

// handleTable renders a table with simple column alignment.
func (r *renderer) handleTable(n *html.Node) {
	r.ensureBlankLine()

	rows := r.extractTableRows(n)
	if len(rows) == 0 {
		return
	}

	// Find number of columns.
	numCols := 0
	for _, row := range rows {
		if len(row.cells) > numCols {
			numCols = len(row.cells)
		}
	}
	if numCols == 0 {
		return
	}

	// Calculate column widths.
	colWidths := make([]int, numCols)
	for _, row := range rows {
		for i, cell := range row.cells {
			w := runewidth.StringWidth(cell.text)
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// Constrain to available width.
	separatorWidth := (numCols - 1) * 3 // " | "
	available := r.width - r.indent - separatorWidth
	if available < numCols {
		available = numCols
	}
	totalWidth := 0
	for _, w := range colWidths {
		totalWidth += w
	}
	if totalWidth > available {
		for i := range colWidths {
			colWidths[i] = max(3, colWidths[i]*available/totalWidth)
		}
	}

	// Render rows.
	for i, row := range rows {
		if r.indent > 0 {
			r.addIndent()
		}
		for j, cell := range row.cells {
			if j >= numCols {
				break
			}
			text := cell.text
			tw := runewidth.StringWidth(text)
			if tw > colWidths[j] {
				text = truncateToWidth(text, colWidths[j])
				tw = colWidths[j]
			}
			if cell.isHeader {
				old := r.bold
				r.bold = true
				r.appendToLine(text)
				r.bold = old
			} else {
				r.appendToLine(text)
			}
			// Pad.
			pad := colWidths[j] - tw
			if pad > 0 {
				r.appendToLine(strings.Repeat(" ", pad))
			}
			if j < numCols-1 {
				r.appendToLine(" | ")
			}
		}
		r.flushLine()

		// Separator after header row.
		if i == 0 && row.isHeader {
			if r.indent > 0 {
				r.addIndent()
			}
			for j := range numCols {
				r.appendToLine(strings.Repeat("─", colWidths[j]))
				if j < numCols-1 {
					r.appendToLine("─┼─")
				}
			}
			r.flushLine()
		}
	}
	r.ensureBlankLine()
}

type tableRow struct {
	cells    []tableCell
	isHeader bool
}

type tableCell struct {
	text     string
	isHeader bool
}

func (r *renderer) extractTableRows(n *html.Node) []tableRow {
	var rows []tableRow
	r.findTableRows(n, &rows)
	return rows
}

func (r *renderer) findTableRows(n *html.Node, rows *[]tableRow) {
	if n.Type == html.ElementNode && n.DataAtom == atom.Tr {
		row := tableRow{}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && (c.DataAtom == atom.Td || c.DataAtom == atom.Th) {
				isHeader := c.DataAtom == atom.Th
				text := strings.TrimSpace(textContent(c))
				row.cells = append(row.cells, tableCell{text: text, isHeader: isHeader})
				if isHeader {
					row.isHeader = true
				}
			}
		}
		*rows = append(*rows, row)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.findTableRows(c, rows)
	}
}

func (r *renderer) resolveURL(href string) string {
	if href == "" {
		return ""
	}
	// Absolute URL.
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:") {
		return href
	}
	// Fragment.
	if strings.HasPrefix(href, "#") {
		return r.pageURL + href
	}
	// Resolve relative.
	base, err := url.Parse(r.pageURL)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return base.ResolveReference(ref).String()
}

// --- Helper functions ---

func findElement(n *html.Node, name string) *html.Node {
	if n.Type == html.ElementNode && n.Data == name {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, name); found != nil {
			return found
		}
	}
	return nil
}

func findText(n *html.Node, elementName string) string {
	el := findElement(n, elementName)
	if el == nil {
		return ""
	}
	return strings.TrimSpace(textContent(el))
}

func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(textContent(c))
	}
	return sb.String()
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func isWhitespace(r rune) bool {
	return unicode.IsSpace(r)
}

func truncateToWidth(s string, maxWidth int) string {
	w := 0
	for i, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > maxWidth {
			if maxWidth >= 3 && i > 0 {
				return s[:i-1] + "…"
			}
			return s[:i]
		}
		w += rw
	}
	return s
}

func intToStr(n int) string {
	return fmt.Sprintf("%d", n)
}

func padRight(s string, width int) string {
	w := runewidth.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}
