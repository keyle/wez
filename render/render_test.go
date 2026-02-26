package render

import (
	"strings"
	"testing"
)

func TestRenderBasicParagraph(t *testing.T) {
	html := `<html><body><p>Hello world</p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "Hello world") {
		t.Errorf("expected 'Hello world' in output, got: %q", text)
	}
}

func TestRenderHeadings(t *testing.T) {
	html := `<html><body><h1>Title</h1><h2>Subtitle</h2></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "# Title") {
		t.Errorf("expected '# Title' in output, got: %q", text)
	}
	if !strings.Contains(text, "## Subtitle") {
		t.Errorf("expected '## Subtitle' in output, got: %q", text)
	}
}

func TestRenderBoldItalic(t *testing.T) {
	html := `<html><body><p><strong>bold</strong> and <em>italic</em></p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "bold") {
		t.Errorf("expected 'bold' in output, got: %q", text)
	}
	if !strings.Contains(text, "italic") {
		t.Errorf("expected 'italic' in output, got: %q", text)
	}

	// Check styles.
	found := false
	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			if strings.Contains(span.Text, "bold") && span.Style.Bold {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected bold span for 'bold' text")
	}
}

func TestRenderLinks(t *testing.T) {
	html := `<html><body><p><a href="http://example.com/page">Click here</a></p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(doc.Links))
	}
	if doc.Links[0].URL != "http://example.com/page" {
		t.Errorf("expected link URL 'http://example.com/page', got %q", doc.Links[0].URL)
	}

	text := docText(doc)
	if !strings.Contains(text, "Click here") {
		t.Errorf("expected 'Click here' in output, got: %q", text)
	}
}

func TestRenderRelativeLinks(t *testing.T) {
	html := `<html><body><a href="/about">About</a></body></html>`
	doc := Render([]byte(html), "http://example.com/page", 80)

	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(doc.Links))
	}
	if doc.Links[0].URL != "http://example.com/about" {
		t.Errorf("expected resolved URL 'http://example.com/about', got %q", doc.Links[0].URL)
	}
}

func TestRenderNoscript(t *testing.T) {
	html := `<html><body><noscript>JavaScript is required</noscript></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "NOSCRIPT") {
		t.Errorf("expected '[NOSCRIPT]' marker in output, got: %q", text)
	}
	if !strings.Contains(text, "JavaScript is required") {
		t.Errorf("expected noscript content in output, got: %q", text)
	}
}

func TestRenderUnorderedList(t *testing.T) {
	html := `<html><body><ul><li>First</li><li>Second</li><li>Third</li></ul></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "First") {
		t.Errorf("expected 'First' in output, got: %q", text)
	}
	if !strings.Contains(text, "Second") {
		t.Errorf("expected 'Second' in output, got: %q", text)
	}
	if !strings.Contains(text, "*") {
		t.Errorf("expected bullet marker '*' in output, got: %q", text)
	}
}

func TestRenderOrderedList(t *testing.T) {
	html := `<html><body><ol><li>First</li><li>Second</li></ol></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "1.") {
		t.Errorf("expected '1.' in output, got: %q", text)
	}
	if !strings.Contains(text, "2.") {
		t.Errorf("expected '2.' in output, got: %q", text)
	}
}

func TestRenderPreformatted(t *testing.T) {
	html := `<html><body><pre>func main() {
    fmt.Println("hello")
}</pre></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "func main()") {
		t.Errorf("expected preformatted text preserved, got: %q", text)
	}
}

func TestRenderTable(t *testing.T) {
	html := `<html><body><table>
		<tr><th>Name</th><th>Age</th></tr>
		<tr><td>Alice</td><td>30</td></tr>
		<tr><td>Bob</td><td>25</td></tr>
	</table></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "Name") {
		t.Errorf("expected 'Name' in table output, got: %q", text)
	}
	if !strings.Contains(text, "Alice") {
		t.Errorf("expected 'Alice' in table output, got: %q", text)
	}
	if strings.Contains(text, "NameAge") {
		t.Errorf("expected whitespace between table cells, got: %q", text)
	}

	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected multiple lines for table rows, got: %q", text)
	}
	if !strings.Contains(lines[0], "Name") || !strings.Contains(lines[1], "Alice") || !strings.Contains(lines[2], "Bob") {
		t.Errorf("expected each table row to render on its own line, got lines: %#v", lines)
	}
}

func TestRenderTableSoftWrap(t *testing.T) {
	html := `<html><body><table><tr><td>verylongword verylongword verylongword verylongword</td></tr></table></body></html>`
	doc := Render([]byte(html), "http://example.com", 20)

	nonEmpty := 0
	for i, line := range doc.Lines {
		if line.Width() > 20 {
			t.Fatalf("line %d exceeded width 20: %d", i, line.Width())
		}
		if strings.TrimSpace(lineToText(line)) != "" {
			nonEmpty++
		}
	}
	if nonEmpty < 2 {
		t.Fatalf("expected wrapped table content across multiple lines, got: %q", docText(doc))
	}
}

func TestTableLinkDoesNotStartWithSpace(t *testing.T) {
	html := `<html><body><table><tr><td>1.</td><td> <a href="/user">alice</a></td><td> <a href="/item">42 comments</a></td></tr></table></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com", 120)

	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			if span.LinkIdx >= 0 && strings.HasPrefix(span.Text, " ") {
				t.Fatalf("link span should not start with whitespace: %q", span.Text)
			}
		}
	}
}

func TestRenderHR(t *testing.T) {
	html := `<html><body><p>Above</p><hr><p>Below</p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "─") {
		t.Errorf("expected horizontal rule in output, got: %q", text)
	}
}

func TestRenderImage(t *testing.T) {
	html := `<html><body><img src="photo.jpg" alt="A photo"></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "[IMG: A photo]") {
		t.Errorf("expected '[IMG: A photo]' in output, got: %q", text)
	}
}

func TestRenderImageNoAlt(t *testing.T) {
	html := `<html><body><img src="photo.jpg"></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "[IMG]") {
		t.Errorf("expected '[IMG]' in output, got: %q", text)
	}
}

func TestRenderScriptIgnored(t *testing.T) {
	html := `<html><body><script>alert('xss')</script><p>Visible</p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if strings.Contains(text, "alert") {
		t.Errorf("script content should be hidden, got: %q", text)
	}
	if !strings.Contains(text, "Visible") {
		t.Errorf("expected 'Visible' in output, got: %q", text)
	}
}

func TestRenderStyleIgnored(t *testing.T) {
	html := `<html><body><style>.foo{color:red}</style><p>Content</p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if strings.Contains(text, "color") {
		t.Errorf("style content should be hidden, got: %q", text)
	}
}

func TestRenderTitleExtraction(t *testing.T) {
	html := `<html><head><title>My Page</title></head><body><p>Content</p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	if doc.Title != "My Page" {
		t.Errorf("expected title 'My Page', got %q", doc.Title)
	}
}

func TestRenderWordWrap(t *testing.T) {
	// Create a paragraph with long text.
	words := make([]string, 20)
	for i := range words {
		words[i] = "word"
	}
	longText := strings.Join(words, " ")
	html := `<html><body><p>` + longText + `</p></body></html>`
	doc := Render([]byte(html), "http://example.com", 40)

	// Should have multiple lines due to wrapping.
	nonEmpty := 0
	for _, line := range doc.Lines {
		if len(line.Spans) > 0 {
			nonEmpty++
		}
	}
	if nonEmpty < 2 {
		t.Errorf("expected word wrapping to produce multiple lines at width 40, got %d non-empty lines", nonEmpty)
	}

	// No line should exceed width.
	for i, line := range doc.Lines {
		w := line.Width()
		if w > 40 {
			t.Errorf("line %d exceeds width 40: width=%d", i, w)
		}
	}
}

func TestRenderBlockquote(t *testing.T) {
	html := `<html><body><blockquote>Quoted text</blockquote></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "|") {
		t.Errorf("expected blockquote marker '|' in output, got: %q", text)
	}
	if !strings.Contains(text, "Quoted text") {
		t.Errorf("expected 'Quoted text' in output, got: %q", text)
	}
}

func TestLinkAt(t *testing.T) {
	html := `<html><body><p><a href="http://example.com">link text</a></p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	// Find the link text on some line.
	found := false
	for lineIdx, line := range doc.Lines {
		x := 0
		for _, span := range line.Spans {
			if span.LinkIdx >= 0 {
				_, url, ok := doc.LinkAt(lineIdx, x)
				if ok && url == "http://example.com" {
					found = true
				}
			}
			x += len(span.Text) // simplified for ASCII
		}
	}
	if !found {
		t.Error("expected to find link at its position")
	}
}

func TestNextPrevLink(t *testing.T) {
	html := `<html><body>
		<p><a href="/a">Link A</a></p>
		<p><a href="/b">Link B</a></p>
		<p><a href="/c">Link C</a></p>
	</body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	if len(doc.Links) < 3 {
		t.Fatalf("expected at least 3 links, got %d", len(doc.Links))
	}

	// NextLink from start should find first link.
	_, _, _, ok := doc.NextLink(0, 0)
	if !ok {
		t.Error("expected NextLink to find a link")
	}

	// PrevLink from a late position should find a link.
	_, _, _, ok = doc.PrevLink(100, 0)
	if !ok {
		t.Error("expected PrevLink to find a link")
	}
}

func TestRenderPlainText(t *testing.T) {
	text := "Line 1\nLine 2\nLine 3"
	doc := RenderPlainText([]byte(text), "http://example.com/file.txt", 80)

	if len(doc.Lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(doc.Lines))
	}
}

func TestWhitespaceCollapsing(t *testing.T) {
	html := `<html><body><p>Hello     world    test</p></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	// Multiple spaces should be collapsed.
	if strings.Contains(text, "  ") {
		t.Errorf("expected whitespace collapsing, got: %q", text)
	}
}

func TestRenderDefinitionList(t *testing.T) {
	html := `<html><body><dl><dt>Term</dt><dd>Definition</dd></dl></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "Term") {
		t.Errorf("expected 'Term' in output, got: %q", text)
	}
	if !strings.Contains(text, "Definition") {
		t.Errorf("expected 'Definition' in output, got: %q", text)
	}
}

// docText concatenates all span text from a document for easy testing.
func docText(doc *Document) string {
	var sb strings.Builder
	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			sb.WriteString(span.Text)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
