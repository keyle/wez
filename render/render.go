package render

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Render parses raw HTML and produces a Document ready for terminal display.
func Render(htmlBytes []byte, pageURL string, width int) *Document {
	return RenderWithVisited(htmlBytes, pageURL, width, nil)
}

// RenderWithVisited parses HTML and marks links with visited styling when callback returns true.
func RenderWithVisited(htmlBytes []byte, pageURL string, width int, isVisited func(string) bool) *Document {
	doc, err := html.Parse(strings.NewReader(string(htmlBytes)))
	if err != nil {
		return &Document{
			Title: "Parse Error",
			URL:   pageURL,
			Lines: []Line{{Spans: []Span{{Text: "Error parsing HTML: " + err.Error(), LinkIdx: -1, ControlIdx: -1}}}},
		}
	}

	r := &renderer{
		width:         width,
		curLinkIdx:    -1,
		curControlIdx: -1,
		pageURL:       pageURL,
		isVisited:     isVisited,
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

	lines, links, mapping := compactVerticalWhitespaceWithMapping(r.lines, r.links)
	anchors := remapAnchors(r.anchors, mapping, len(lines))

	return &Document{
		Title:    r.title,
		URL:      pageURL,
		Lines:    lines,
		Links:    links,
		Anchors:  anchors,
		Forms:    r.forms,
		Controls: r.controls,
	}
}

// RenderPlainText produces a Document from plain text content.
func RenderPlainText(text []byte, pageURL string, width int) *Document {
	lines := strings.Split(string(text), "\n")
	var docLines []Line
	for _, line := range lines {
		wrapped := wrapPlainLine(line, width)
		for _, part := range wrapped {
			docLines = append(docLines, Line{
				Spans: []Span{{Text: part, LinkIdx: -1, ControlIdx: -1}},
			})
		}
	}
	return &Document{
		Title: pageURL,
		URL:   pageURL,
		Lines: docLines,
	}
}

func wrapPlainLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	if line == "" {
		return []string{""}
	}

	var out []string
	runes := []rune(line)
	for start := 0; start < len(runes); {
		w := 0
		end := start
		for end < len(runes) {
			rw := runewidth.RuneWidth(runes[end])
			if rw < 1 {
				rw = 1
			}
			if w+rw > width {
				break
			}
			w += rw
			end++
		}
		if end == start {
			end++
		}
		out = append(out, string(runes[start:end]))
		start = end
	}

	return out
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
	pendingSpace         bool
	preformatted         bool
	suppressLeadingSpace bool

	// Style context
	bold      bool
	italic    bool
	underline bool
	strike    bool
	color     string
	bgColor   string

	// Link context
	curLinkIdx    int // -1 if not in a link
	curControlIdx int // -1 if not in a form control

	// Form context
	forms     []Form
	controls  []Control
	formStack []int

	// List context
	listStack []listCtx

	// Table row context (number of cells rendered in current row)
	tableRowCells  []int
	hnRowIndent    int
	hnRowHasIndent bool

	// Page URL for resolving relative links.
	pageURL string

	// Link visit callback.
	isVisited func(string) bool

	// Named anchors by fragment to line index.
	anchors map[string]int
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
	controlIdx := r.curControlIdx
	imageURL := ""

	// Merge with last span if same attributes.
	if len(r.curSpans) > 0 {
		last := &r.curSpans[len(r.curSpans)-1]
		if last.Style == style && last.LinkIdx == linkIdx && last.ControlIdx == controlIdx && last.ImageURL == "" {
			last.Text += text
			r.curCol += runewidth.StringWidth(text)
			return
		}
	}

	r.curSpans = append(r.curSpans, Span{
		Text:       text,
		Style:      style,
		LinkIdx:    linkIdx,
		ControlIdx: controlIdx,
		ImageURL:   imageURL,
	})
	r.curCol += runewidth.StringWidth(text)
}

func (r *renderer) flushLine() {
	if len(r.curSpans) > 0 {
		line := Line{Spans: r.curSpans}
		if r.preformatted || !lineIsBlank(line) {
			r.lines = append(r.lines, line)
		}
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
		if !lineIsBlank(last) {
			r.lines = append(r.lines, Line{})
		}
	}
}

func lineIsBlank(line Line) bool {
	if len(line.Spans) == 0 {
		return true
	}
	for _, span := range line.Spans {
		if strings.TrimSpace(span.Text) != "" {
			return false
		}
	}
	return true
}

func compactVerticalWhitespace(lines []Line, links []Link) ([]Line, []Link) {
	outLines, outLinks, _ := compactVerticalWhitespaceWithMapping(lines, links)
	return outLines, outLinks
}

func compactVerticalWhitespaceWithMapping(lines []Line, links []Link) ([]Line, []Link, []int) {
	if len(lines) == 0 {
		return lines, links, nil
	}

	mapping := make([]int, len(lines))
	for i := range mapping {
		mapping[i] = -1
	}

	var out []Line
	seenBlank := false
	for i, line := range lines {
		blank := lineIsBlank(line)
		if blank {
			if len(out) == 0 {
				continue // trim leading blank lines
			}
			if seenBlank {
				continue // collapse multiple blank lines
			}
			seenBlank = true
		} else {
			seenBlank = false
		}

		mapping[i] = len(out)
		out = append(out, line)
	}

	for len(out) > 0 && lineIsBlank(out[len(out)-1]) {
		out = out[:len(out)-1] // trim trailing blank lines
	}

	if len(out) == len(lines) {
		return lines, links, mapping
	}

	outLinks := make([]Link, 0, len(links))
	for _, link := range links {
		if link.Line >= 0 && link.Line < len(mapping) {
			if newLine := mapping[link.Line]; newLine >= 0 {
				link.Line = newLine
				outLinks = append(outLinks, link)
				continue
			}
		}
		// Fallback: keep link and clamp line if mapping was lost.
		if len(out) > 0 {
			if link.Line < 0 {
				link.Line = 0
			} else if link.Line >= len(out) {
				link.Line = len(out) - 1
			}
			outLinks = append(outLinks, link)
		}
	}

	return out, outLinks, mapping
}

func remapAnchors(anchors map[string]int, mapping []int, outLineCount int) map[string]int {
	if len(anchors) == 0 {
		return nil
	}

	out := make(map[string]int, len(anchors))
	for key, line := range anchors {
		out[key] = remapLineThroughMapping(line, mapping, outLineCount)
	}
	return out
}

func remapLineThroughMapping(line int, mapping []int, outLineCount int) int {
	if outLineCount <= 0 {
		return 0
	}
	if len(mapping) == 0 {
		if line < 0 {
			return 0
		}
		if line >= outLineCount {
			return outLineCount - 1
		}
		return line
	}

	if line >= 0 && line < len(mapping) {
		if mapped := mapping[line]; mapped >= 0 {
			return mapped
		}
		for i := line + 1; i < len(mapping); i++ {
			if mapped := mapping[i]; mapped >= 0 {
				return mapped
			}
		}
		for i := line - 1; i >= 0; i-- {
			if mapped := mapping[i]; mapped >= 0 {
				return mapped
			}
		}
	}

	if line < 0 {
		return 0
	}
	if line >= outLineCount {
		return outLineCount - 1
	}
	return line
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
		if hasLeadingSpace && r.curCol > r.indent && !r.suppressLeadingSpace {
			r.pendingSpace = true
		}
		return
	}

	for i, word := range words {
		needSpace := false
		if i == 0 {
			if !r.suppressLeadingSpace {
				needSpace = (hasLeadingSpace || r.pendingSpace) && r.curCol > r.indent
			}
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
		r.suppressLeadingSpace = false
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
		oldLinkIdx := r.curLinkIdx
		oldControlIdx := r.curControlIdx
		oldUnderline := r.underline

		r.curLinkIdx = -1
		r.curControlIdx = -1
		r.underline = false
		r.appendToLine(strings.Repeat(" ", r.indent))

		r.curLinkIdx = oldLinkIdx
		r.curControlIdx = oldControlIdx
		r.underline = oldUnderline
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
	r.registerElementAnchors(n)

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

	case atom.P, atom.Section, atom.Article, atom.Main,
		atom.Header, atom.Footer, atom.Nav, atom.Fieldset,
		atom.Figure, atom.Figcaption, atom.Details, atom.Summary,
		atom.Address:
		prefixInlineStart := len(r.curSpans) > 0 && r.curCol == r.indent
		if !prefixInlineStart {
			r.ensureBlankLine()
			if r.indent > 0 {
				r.addIndent()
			}
		}
		r.walkChildren(n)
		r.ensureBlankLine()

	case atom.Form:
		r.handleForm(n)

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
		r.ensureBlankLine()
		r.walkChildren(n)
		r.ensureBlankLine()

	case atom.Thead, atom.Tbody, atom.Tfoot:
		r.walkChildren(n)

	case atom.Tr:
		r.flushLine()
		r.hnRowIndent = 0
		r.hnRowHasIndent = false
		r.tableRowCells = append(r.tableRowCells, 0)
		r.walkChildren(n)
		r.tableRowCells = r.tableRowCells[:len(r.tableRowCells)-1]
		r.hnRowIndent = 0
		r.hnRowHasIndent = false
		r.flushLine()

	case atom.Td, atom.Th:
		if len(r.tableRowCells) > 0 {
			i := len(r.tableRowCells) - 1
			if r.tableRowCells[i] > 0 {
				r.appendToLine(" ")
			}
			r.tableRowCells[i]++
		}

		extraIndent := 0
		if tag == atom.Td {
			if level, ok := hnIndentLevel(n); ok {
				r.hnRowHasIndent = true
				r.hnRowIndent = level * 4
				if r.hnRowIndent > 0 {
					r.appendToLine(strings.Repeat(" ", r.hnRowIndent))
				}
				return
			}
			if r.hnRowHasIndent && r.hnRowIndent > 0 {
				extraIndent = r.hnRowIndent
			}
		}

		oldIndent := r.indent
		if extraIndent > 0 {
			r.indent += extraIndent
		}

		r.pendingSpace = false
		oldSuppressLeadingSpace := r.suppressLeadingSpace
		r.suppressLeadingSpace = true
		if tag == atom.Th {
			oldBold := r.bold
			r.bold = true
			r.walkChildren(n)
			r.bold = oldBold
		} else {
			r.walkChildren(n)
		}
		r.suppressLeadingSpace = oldSuppressLeadingSpace
		r.indent = oldIndent

	case atom.Img:
		r.handleImage(n)

	case atom.Input:
		r.handleInput(n)

	case atom.Textarea:
		r.handleTextarea(n)

	case atom.Select:
		r.handleSelect(n)

	case atom.Button:
		r.handleButton(n)

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

func (r *renderer) registerElementAnchors(n *html.Node) {
	if n == nil {
		return
	}

	id := strings.TrimSpace(getAttr(n, "id"))
	if id != "" {
		r.registerAnchor(id)
	}
	if n.DataAtom == atom.A {
		name := strings.TrimSpace(getAttr(n, "name"))
		if name != "" {
			r.registerAnchor(name)
		}
	}
}

func (r *renderer) registerAnchor(name string) {
	key := strings.TrimSpace(name)
	if key == "" {
		return
	}
	if r.anchors == nil {
		r.anchors = make(map[string]int)
	}

	line := len(r.lines)
	if _, exists := r.anchors[key]; !exists {
		r.anchors[key] = line
	}
	lowerKey := strings.ToLower(key)
	if _, exists := r.anchors[lowerKey]; !exists {
		r.anchors[lowerKey] = line
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
	if isJavaScriptURL(href) {
		oldColor := r.color
		r.color = "visited_link"
		r.walkChildren(n)
		r.color = oldColor
		return
	}

	// If spacing was pending before the <a>, emit it outside link styling.
	if r.pendingSpace && r.curCol > r.indent {
		r.pendingSpace = false
		r.appendToLine(" ")
	}

	// Resolve relative URL.
	resolved := r.resolveURL(href)
	if resolved == "" || isJavaScriptURL(resolved) {
		oldColor := r.color
		r.color = "visited_link"
		r.walkChildren(n)
		r.color = oldColor
		return
	}

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
	oldSuppressLeadingSpace := r.suppressLeadingSpace
	if r.isVisited != nil && r.isVisited(resolved) {
		r.color = "visited_link"
	} else {
		r.color = "link"
	}
	r.underline = true
	r.curLinkIdx = linkIdx
	r.suppressLeadingSpace = true
	startLineCount := len(r.lines)
	startSpanCount := len(r.curSpans)

	r.walkChildren(n)

	if !r.linkHasVisibleText(linkIdx, startLineCount, startSpanCount) {
		placeholder := "[a]"
		if r.curCol+runewidth.StringWidth(placeholder) > r.width && r.curCol > r.indent {
			r.flushLine()
			if r.indent > 0 {
				r.addIndent()
			}
		}
		r.links[linkIdx].Line = len(r.lines)
		r.links[linkIdx].Col = r.curCol
		r.appendToLine(placeholder)
	}

	r.color = oldColor
	r.underline = oldUnderline
	r.curLinkIdx = oldLinkIdx
	r.suppressLeadingSpace = oldSuppressLeadingSpace
}

func (r *renderer) linkHasVisibleText(linkIdx int, startLineCount, startSpanCount int) bool {
	if linkIdx < 0 {
		return false
	}

	for lineIdx := startLineCount; lineIdx < len(r.lines); lineIdx++ {
		for _, span := range r.lines[lineIdx].Spans {
			if span.LinkIdx == linkIdx && strings.TrimSpace(span.Text) != "" {
				return true
			}
		}
	}

	curSpanStart := 0
	if len(r.lines) == startLineCount {
		curSpanStart = startSpanCount
	}
	for i := curSpanStart; i < len(r.curSpans); i++ {
		span := r.curSpans[i]
		if span.LinkIdx == linkIdx && strings.TrimSpace(span.Text) != "" {
			return true
		}
	}

	return false
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
	r.curCol = r.indent
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
	oldIndent := r.indent

	var bullet string
	if len(r.listStack) > 0 {
		ctx := &r.listStack[len(r.listStack)-1]
		if ctx.ordered {
			ctx.counter++
			bullet = strings.Repeat(" ", oldIndent) + padRight(intToStr(ctx.counter)+".", 4)
		} else {
			bullet = strings.Repeat(" ", oldIndent) + "  * "
		}
	} else {
		bullet = strings.Repeat(" ", oldIndent) + "  * "
	}

	r.appendToLine(bullet)
	r.indent = oldIndent + 4
	r.curCol = r.indent
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

	labelKind := "IMG"
	if isSVGImageReference(src, resolved) {
		labelKind = "SVG"
	}

	label := "[" + labelKind
	if alt != "" {
		label += ": " + alt
	}
	label += "]"

	span := Span{
		Text:       label,
		Style:      r.currentStyle(),
		LinkIdx:    r.curLinkIdx,
		ControlIdx: -1,
		ImageURL:   resolved,
	}
	r.curSpans = append(r.curSpans, span)
	r.curCol += runewidth.StringWidth(label)

	r.color = oldColor
}

func (r *renderer) currentFormIdx() int {
	if len(r.formStack) == 0 {
		return -1
	}
	return r.formStack[len(r.formStack)-1]
}

func (r *renderer) registerControl(c Control) int {
	idx := len(r.controls)
	r.controls = append(r.controls, c)
	if c.FormIdx >= 0 && c.FormIdx < len(r.forms) {
		r.forms[c.FormIdx].Controls = append(r.forms[c.FormIdx].Controls, idx)
	}
	return idx
}

func (r *renderer) addControlToken(controlIdx int, text string, bold bool) {
	if controlIdx < 0 || controlIdx >= len(r.controls) || text == "" {
		return
	}

	if r.pendingSpace && r.curCol > r.indent {
		r.pendingSpace = false
		r.appendToLine(" ")
	}

	maxW := r.width - r.indent
	if maxW < 4 {
		maxW = 4
	}
	if runewidth.StringWidth(text) > maxW {
		text = truncateToWidth(text, maxW)
	}

	tw := runewidth.StringWidth(text)
	if r.curCol+tw > r.width && r.curCol > r.indent {
		r.flushLine()
		if r.indent > 0 {
			r.addIndent()
		}
	}

	r.controls[controlIdx].Line = len(r.lines)
	r.controls[controlIdx].Col = r.curCol
	r.controls[controlIdx].Width = tw

	oldColor := r.color
	oldBold := r.bold
	oldControl := r.curControlIdx

	r.color = "code"
	r.bold = bold || r.bold
	r.curControlIdx = controlIdx
	r.appendToLine(text)

	r.curControlIdx = oldControl
	r.bold = oldBold
	r.color = oldColor
}

func (r *renderer) handleForm(n *html.Node) {
	method := strings.ToUpper(strings.TrimSpace(getAttr(n, "method")))
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "POST" {
		method = "GET"
	}

	action := strings.TrimSpace(getAttr(n, "action"))
	if action == "" {
		action = r.pageURL
	} else if isJavaScriptURL(action) {
		action = ""
	} else {
		action = r.resolveURL(action)
	}

	enctype := strings.ToLower(strings.TrimSpace(getAttr(n, "enctype")))
	if enctype == "" {
		enctype = "application/x-www-form-urlencoded"
	}

	formIdx := len(r.forms)
	r.forms = append(r.forms, Form{
		Method:   method,
		Action:   action,
		Enctype:  enctype,
		Controls: nil,
	})

	r.ensureBlankLine()
	r.formStack = append(r.formStack, formIdx)
	r.walkChildren(n)
	r.formStack = r.formStack[:len(r.formStack)-1]
	r.ensureBlankLine()
}

func (r *renderer) handleInput(n *html.Node) {
	inputType := strings.ToLower(strings.TrimSpace(getAttr(n, "type")))
	if inputType == "" {
		inputType = "text"
	}

	ctrl := Control{
		Kind:        "input",
		Type:        inputType,
		FormIdx:     r.currentFormIdx(),
		Name:        strings.TrimSpace(getAttr(n, "name")),
		Value:       getAttr(n, "value"),
		Checked:     hasAttr(n, "checked"),
		Disabled:    hasAttr(n, "disabled"),
		ReadOnly:    hasAttr(n, "readonly"),
		DisplaySize: parsePositiveInt(getAttr(n, "size"), 20),
		Line:        -1,
		Col:         -1,
		Width:       0,
	}

	if (inputType == "checkbox" || inputType == "radio") && strings.TrimSpace(ctrl.Value) == "" {
		ctrl.Value = "on"
	}
	if inputType == "submit" && strings.TrimSpace(ctrl.Value) == "" {
		ctrl.Value = "submit"
	}
	if inputType == "reset" && strings.TrimSpace(ctrl.Value) == "" {
		ctrl.Value = "reset"
	}
	if inputType == "image" {
		ctrl.Type = "submit"
		if strings.TrimSpace(ctrl.Value) == "" {
			ctrl.Value = strings.TrimSpace(getAttr(n, "alt"))
		}
		if strings.TrimSpace(ctrl.Value) == "" {
			ctrl.Value = "submit"
		}
	}

	if inputType == "hidden" {
		r.registerControl(ctrl)
		return
	}

	if inputType == "checkbox" || inputType == "radio" {
		ctrl.DisplaySize = 3
	}

	idx := r.registerControl(ctrl)
	label := ControlDisplayText(r.controls[idx])
	r.addControlToken(idx, label, inputType == "submit" || inputType == "button" || inputType == "reset")
}

func (r *renderer) handleTextarea(n *html.Node) {
	value := textContent(n)
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", " ")

	ctrl := Control{
		Kind:        "textarea",
		Type:        "textarea",
		FormIdx:     r.currentFormIdx(),
		Name:        strings.TrimSpace(getAttr(n, "name")),
		Value:       value,
		Disabled:    hasAttr(n, "disabled"),
		ReadOnly:    hasAttr(n, "readonly"),
		DisplaySize: parsePositiveInt(getAttr(n, "cols"), 28),
		Line:        -1,
		Col:         -1,
		Width:       0,
	}

	idx := r.registerControl(ctrl)
	r.addControlToken(idx, ControlDisplayText(r.controls[idx]), false)
}

func (r *renderer) handleSelect(n *html.Node) {
	options := collectSelectOptions(n)
	multiple := hasAttr(n, "multiple")

	if !multiple {
		selected := false
		for i := range options {
			if options[i].Selected {
				selected = true
				break
			}
		}
		if !selected && len(options) > 0 {
			options[0].Selected = true
		}
	}

	ctrl := Control{
		Kind:        "select",
		Type:        "select",
		FormIdx:     r.currentFormIdx(),
		Name:        strings.TrimSpace(getAttr(n, "name")),
		Disabled:    hasAttr(n, "disabled"),
		ReadOnly:    false,
		Multiple:    multiple,
		Options:     options,
		DisplaySize: parsePositiveInt(getAttr(n, "size"), 20),
		Line:        -1,
		Col:         -1,
		Width:       0,
	}
	ctrl.Value = selectControlValue(ctrl)

	idx := r.registerControl(ctrl)
	r.addControlToken(idx, ControlDisplayText(r.controls[idx]), false)
}

func (r *renderer) handleButton(n *html.Node) {
	btnType := strings.ToLower(strings.TrimSpace(getAttr(n, "type")))
	if btnType == "" {
		btnType = "submit"
	}

	label := strings.TrimSpace(getAttr(n, "value"))
	if label == "" {
		label = strings.TrimSpace(textContent(n))
	}
	if label == "" {
		label = btnType
	}

	ctrl := Control{
		Kind:        "button",
		Type:        btnType,
		FormIdx:     r.currentFormIdx(),
		Name:        strings.TrimSpace(getAttr(n, "name")),
		Value:       label,
		Disabled:    hasAttr(n, "disabled"),
		ReadOnly:    false,
		DisplaySize: runewidth.StringWidth(label),
		Line:        -1,
		Col:         -1,
		Width:       0,
	}

	idx := r.registerControl(ctrl)
	r.addControlToken(idx, ControlDisplayText(r.controls[idx]), true)
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
	if isJavaScriptURL(href) {
		return ""
	}
	// Absolute URL.
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "mailto:") {
		return href
	}
	// Fragment.
	if strings.HasPrefix(href, "#") {
		base, err := url.Parse(r.pageURL)
		if err != nil {
			return r.pageURL + href
		}
		base.Fragment = strings.TrimPrefix(href, "#")
		return base.String()
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

func isSVGImageReference(src, resolved string) bool {
	v := strings.ToLower(strings.TrimSpace(src))
	if strings.HasPrefix(v, "data:image/svg+xml") {
		return true
	}
	return isSVGURL(v) || isSVGURL(resolved)
}

func isSVGURL(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return false
	}

	if strings.HasPrefix(v, "data:image/svg+xml") {
		return true
	}

	if u, err := url.Parse(v); err == nil {
		p := strings.ToLower(strings.TrimSpace(u.Path))
		if strings.HasSuffix(p, ".svg") || strings.HasSuffix(p, ".svgz") {
			return true
		}
	}

	if cut := strings.IndexAny(v, "?#"); cut >= 0 {
		v = v[:cut]
	}

	return strings.HasSuffix(v, ".svg") || strings.HasSuffix(v, ".svgz")
}

func isJavaScriptURL(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(v, "javascript:")
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

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return true
		}
	}
	return false
}

func parsePositiveInt(s string, fallback int) int {
	v := strings.TrimSpace(s)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func hnIndentLevel(td *html.Node) (int, bool) {
	if td == nil || td.DataAtom != atom.Td {
		return 0, false
	}

	classAttr := strings.ToLower(strings.TrimSpace(getAttr(td, "class")))
	if classAttr == "" {
		return 0, false
	}
	fields := strings.Fields(classAttr)
	isInd := false
	for _, f := range fields {
		if f == "ind" {
			isInd = true
			break
		}
	}
	if !isInd {
		return 0, false
	}

	level := parsePositiveInt(getAttr(td, "indent"), 0)
	return level, true
}

func collectSelectOptions(selectNode *html.Node) []ControlOption {
	opts := make([]ControlOption, 0, 4)
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inheritedDisabled bool) {
		if n == nil {
			return
		}

		disabled := inheritedDisabled || hasAttr(n, "disabled")
		if n.Type == html.ElementNode && n.DataAtom == atom.Option {
			label := strings.TrimSpace(textContent(n))
			value := strings.TrimSpace(getAttr(n, "value"))
			if value == "" {
				value = label
			}
			opts = append(opts, ControlOption{
				Value:    value,
				Label:    label,
				Selected: hasAttr(n, "selected"),
				Disabled: disabled,
			})
			return
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, disabled)
		}
	}
	walk(selectNode, false)
	return opts
}

func selectControlValue(c Control) string {
	vals := make([]string, 0, len(c.Options))
	for _, opt := range c.Options {
		if opt.Selected {
			vals = append(vals, opt.Value)
		}
	}
	if len(vals) == 0 {
		return ""
	}
	if c.Multiple {
		return strings.Join(vals, ",")
	}
	return vals[0]
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
