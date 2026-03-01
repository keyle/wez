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

func TestRenderEmptyAnchorRendersPlaceholder(t *testing.T) {
	html := `<html><body><a href="vote?id=1&amp;how=up&amp;goto=item?id=1"><div class="votearrow"></div></a></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com/item?id=1", 80)

	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(doc.Links))
	}
	if doc.Links[0].URL != "https://news.ycombinator.com/vote?id=1&how=up&goto=item?id=1" {
		t.Fatalf("unexpected link URL: %q", doc.Links[0].URL)
	}
	if !strings.Contains(docText(doc), "[a]") {
		t.Fatalf("expected empty anchor placeholder '[a]', got: %q", docText(doc))
	}

	foundPlaceholderLink := false
	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			if span.Text == "[a]" && span.LinkIdx == 0 {
				foundPlaceholderLink = true
			}
		}
	}
	if !foundPlaceholderLink {
		t.Fatalf("expected '[a]' placeholder to be link-styled, got: %q", docText(doc))
	}
}

func TestRenderIgnoresJavaScriptLinks(t *testing.T) {
	html := `<html><body><a href="javascript:void(0)">Click me</a></body></html>`
	doc := Render([]byte(html), "https://example.com", 80)

	if len(doc.Links) != 0 {
		t.Fatalf("expected javascript link to be non-clickable, got %d links", len(doc.Links))
	}

	foundVisitedColor := false
	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			if strings.Contains(span.Text, "Click me") && span.Style.Color == "visited_link" {
				foundVisitedColor = true
			}
		}
	}
	if !foundVisitedColor {
		t.Fatalf("expected javascript link text to render with visited color, got: %q", docText(doc))
	}
}

func TestRenderVisitedLinksStyled(t *testing.T) {
	html := `<html><body><a href="/seen">Seen</a> <a href="/new">New</a></body></html>`
	doc := RenderWithVisited([]byte(html), "https://example.com", 80, func(u string) bool {
		return u == "https://example.com/seen"
	})

	if len(doc.Links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(doc.Links))
	}

	var seenVisited, newNormal bool
	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			if strings.Contains(span.Text, "Seen") && span.Style.Color == "visited_link" {
				seenVisited = true
			}
			if strings.Contains(span.Text, "New") && span.Style.Color == "link" {
				newNormal = true
			}
		}
	}
	if !seenVisited || !newNormal {
		t.Fatalf("expected visited/new coloring, got: %q", docText(doc))
	}
}

func TestAdjacentLinksGetWordBoundarySpace(t *testing.T) {
	html := `<html><body><p><a href="/hn">Hacker News</a><a href="/new">new</a></p></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com", 120)

	text := docText(doc)
	if strings.Contains(text, "Hacker Newsnew") {
		t.Fatalf("expected space between adjacent link words, got: %q", text)
	}
	if !strings.Contains(text, "Hacker News new") {
		t.Fatalf("expected adjacent links to include a single boundary space, got: %q", text)
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

func TestRenderFragmentLinkReplacesExistingFragment(t *testing.T) {
	html := `<html><body><a href="#thread-2">Next</a></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com/item?id=1#thread-1", 80)

	if len(doc.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(doc.Links))
	}
	if doc.Links[0].URL != "https://news.ycombinator.com/item?id=1#thread-2" {
		t.Fatalf("unexpected fragment link URL: %q", doc.Links[0].URL)
	}
}

func TestRenderCollectsAnchorsByIDAndName(t *testing.T) {
	html := `<html><body><h2 id="section-two">Section Two</h2><a name="legacy-anchor"></a><p>Body</p></body></html>`
	doc := Render([]byte(html), "https://example.com/doc", 80)

	if _, ok := doc.AnchorLine("section-two"); !ok {
		t.Fatal("expected id-based anchor to be available")
	}
	if _, ok := doc.AnchorLine("legacy-anchor"); !ok {
		t.Fatal("expected name-based anchor to be available")
	}
	if _, ok := doc.AnchorLine("legacy%2Danchor"); !ok {
		t.Fatal("expected URL-escaped anchor lookup to work")
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

func TestHNCommentIndentCellRendersThreadIndent(t *testing.T) {
	html := `<html><body><table><tr><td class="ind" indent="2"><img src="s.gif" height="1" width="80"></td><td class="votelinks"></td><td class="default">child comment</td></tr></table></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com/item?id=1", 120)

	text := docText(doc)
	if strings.Contains(text, "[IMG]") {
		t.Fatalf("expected indent cell image to be suppressed, got: %q", text)
	}

	var found bool
	for _, line := range doc.Lines {
		lt := lineToText(line)
		if strings.Contains(lt, "child comment") {
			found = true
			if !strings.HasPrefix(lt, "        ") {
				t.Fatalf("expected threaded indentation prefix, got line: %q", lt)
			}
		}
	}
	if !found {
		t.Fatalf("expected child comment line, got: %q", text)
	}
}

func TestHNCommentIndentAppliesToWrappedBodyAndReply(t *testing.T) {
	html := `<html><body><table><tr><td class="ind" indent="2"><img src="s.gif" height="1" width="80"></td><td class="default"><div class="comment"><div class="commtext c00">This is a deeply indented comment body that should wrap across multiple lines in a narrow viewport.</div><div class="reply"><a href="/reply?id=1">reply</a></div></div></td></tr></table></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com/item?id=1", 44)

	hasReply := false
	for _, line := range doc.Lines {
		lt := lineToText(line)
		if strings.TrimSpace(lt) == "" {
			continue
		}
		if !strings.HasPrefix(lt, "        ") {
			t.Fatalf("expected all comment lines indented by thread level, got: %q", lt)
		}
		if strings.Contains(lt, "reply") {
			hasReply = true
		}
	}
	if !hasReply {
		t.Fatalf("expected reply line in output, got: %q", docText(doc))
	}
}

func TestHNVotePlaceholderNoLeadingSpaceAtIndentZero(t *testing.T) {
	html := `<html><body><table><tr><td class="ind" indent="0"><img src="s.gif" height="1" width="0"></td><td class="votelinks"><a href="/vote?id=1"><div class="votearrow"></div></a></td><td class="default"><a href="/user?id=alice">alice</a></td></tr></table></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com/item?id=1", 120)

	found := false
	for _, line := range doc.Lines {
		lt := lineToText(line)
		if strings.Contains(lt, "[a]") && strings.Contains(lt, "alice") {
			found = true
			if strings.HasPrefix(lt, " ") {
				t.Fatalf("expected vote placeholder to start at column 0, got: %q", lt)
			}
			if !strings.HasPrefix(lt, "[a] alice") {
				t.Fatalf("expected vote placeholder and author to align without leading padding, got: %q", lt)
			}
		}
	}
	if !found {
		t.Fatalf("expected line containing vote placeholder and author, got: %q", docText(doc))
	}
}

func TestIndentedParagraphDoesNotAddExtraLeadingSpace(t *testing.T) {
	html := `<html><body><table><tr><td class="ind" indent="1"><img src="s.gif" height="1" width="40"></td><td class="default"><div class="comment"><p>
  hello world
</p></div></td></tr></table></body></html>`
	doc := Render([]byte(html), "https://news.ycombinator.com/item?id=1", 80)

	for _, line := range doc.Lines {
		lt := lineToText(line)
		if strings.Contains(lt, "hello world") {
			if strings.HasPrefix(lt, "     hello world") {
				t.Fatalf("expected no extra leading content space beyond indent, got: %q", lt)
			}
			if !strings.HasPrefix(lt, "    hello world") {
				t.Fatalf("expected text to align to indent, got: %q", lt)
			}
			return
		}
	}

	t.Fatalf("expected indented paragraph text, got: %q", docText(doc))
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

func TestLinkLeadingWhitespaceTrimmed(t *testing.T) {
	html := `<html><body><p>Hello <a href="/u"> world</a></p></body></html>`
	doc := Render([]byte(html), "https://example.com", 120)

	if !strings.Contains(docText(doc), "Hello world") {
		t.Fatalf("expected single-space separation before link text, got: %q", docText(doc))
	}

	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			if span.LinkIdx >= 0 && strings.HasPrefix(span.Text, " ") {
				t.Fatalf("link span should not start with whitespace: %q", span.Text)
			}
		}
	}
}

func TestBlockquoteParagraphDoesNotBreakAfterMarker(t *testing.T) {
	html := `<html><body><blockquote><p>contents</p></blockquote></body></html>`
	doc := Render([]byte(html), "https://example.com", 80)

	found := false
	for _, line := range doc.Lines {
		text := lineToText(line)
		if strings.Contains(text, "|") {
			found = true
			if !strings.Contains(text, "contents") {
				t.Fatalf("expected blockquote marker and paragraph on same line, got %q", text)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected blockquote marker line, got %q", docText(doc))
	}
}

func TestListItemParagraphDoesNotBreakAfterBullet(t *testing.T) {
	html := `<html><body><ul><li><p>item one</p></li></ul></body></html>`
	doc := Render([]byte(html), "https://example.com", 80)

	found := false
	for _, line := range doc.Lines {
		text := lineToText(line)
		if strings.Contains(text, "*") {
			found = true
			if !strings.Contains(text, "item one") {
				t.Fatalf("expected list bullet and paragraph on same line, got %q", text)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected list bullet line, got %q", docText(doc))
	}
}

func TestBlockquoteParagraphWithLeadingNewlineDoesNotBreakAfterMarker(t *testing.T) {
	html := `<html><body><ul><li><blockquote>
<p>quoted line</p>
</blockquote></li></ul></body></html>`
	doc := Render([]byte(html), "https://example.com", 80)

	found := false
	for _, line := range doc.Lines {
		text := lineToText(line)
		if strings.Contains(text, "|") {
			found = true
			if !strings.Contains(text, "quoted line") {
				t.Fatalf("expected blockquote marker and paragraph on same line, got %q", text)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected blockquote marker line, got %q", docText(doc))
	}
}

func TestListItemParagraphWithLeadingNewlineDoesNotBreakAfterBullet(t *testing.T) {
	html := `<html><body><blockquote><ul><li>
<p>nested item</p>
</li></ul></blockquote></body></html>`
	doc := Render([]byte(html), "https://example.com", 80)

	found := false
	for _, line := range doc.Lines {
		text := lineToText(line)
		if strings.Contains(text, "*") {
			found = true
			if !strings.Contains(text, "nested item") {
				t.Fatalf("expected list bullet and paragraph on same line, got %q", text)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected list bullet line, got %q", docText(doc))
	}
}

func TestWrappedLinkIndentIsNotLinkStyled(t *testing.T) {
	html := `<html><body><blockquote><a href="/x">alpha beta gamma delta epsilon zeta eta theta iota kappa lambda</a></blockquote></body></html>`
	doc := Render([]byte(html), "https://example.com", 24)

	for _, line := range doc.Lines {
		for _, span := range line.Spans {
			if span.LinkIdx >= 0 && strings.TrimSpace(span.Text) == "" {
				t.Fatalf("expected indentation spaces outside link styling, got link span: %q", span.Text)
			}
		}
	}
}

func TestWrappedLinkInOrderedListDoesNotAddExtraLeadingSpace(t *testing.T) {
	html := `<html><body><ol><li>54 <a href="/s">
  Package Managers a la Carte dependency resolution details
</a></li></ol></body></html>`
	doc := Render([]byte(html), "https://example.com", 28)

	var checked bool
	for _, line := range doc.Lines {
		lt := lineToText(line)
		if strings.Contains(lt, "dependency") || strings.Contains(lt, "resolution") || strings.Contains(lt, "details") {
			checked = true
			if strings.HasPrefix(lt, "     ") {
				t.Fatalf("expected wrapped link line to align to list indent without extra leading space, got %q", lt)
			}
			if !strings.HasPrefix(lt, "    ") {
				t.Fatalf("expected wrapped link line to keep list indentation, got %q", lt)
			}
		}
	}
	if !checked {
		t.Fatalf("expected wrapped link continuation lines, got: %q", docText(doc))
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

func TestRenderSVGImage(t *testing.T) {
	html := `<html><body><img src="icon.svg" alt="Logo"></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "[SVG: Logo]") {
		t.Errorf("expected '[SVG: Logo]' in output, got: %q", text)
	}
}

func TestRenderSVGImageNoAlt(t *testing.T) {
	html := `<html><body><img src="https://cdn.example.com/icon.svg?size=32"></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "[SVG]") {
		t.Errorf("expected '[SVG]' in output, got: %q", text)
	}
}

func TestRenderFormControls(t *testing.T) {
	html := `<html><body>
	<form action="/login" method="post">
		<input type="hidden" name="goto" value="news">
		username: <input type="text" name="acct" value="alice" size="12">
		password: <input type="password" name="pw" value="secret" size="12">
		<input type="submit" name="do" value="login">
	</form>
	</body></html>`

	doc := Render([]byte(html), "https://news.ycombinator.com/login", 120)
	if len(doc.Forms) != 1 {
		t.Fatalf("expected 1 form, got %d", len(doc.Forms))
	}
	if len(doc.Controls) < 4 {
		t.Fatalf("expected at least 4 controls, got %d", len(doc.Controls))
	}

	var hiddenFound bool
	for _, c := range doc.Controls {
		if c.Kind == "input" && c.Type == "hidden" && c.Name == "goto" && c.Value == "news" {
			hiddenFound = true
		}
		if c.Type != "hidden" {
			if c.Line < 0 {
				t.Fatalf("visible control %q missing line position", c.Name)
			}
			idx, ok := doc.ControlAt(c.Line, c.Col)
			if !ok {
				t.Fatalf("expected control hit at line=%d col=%d", c.Line, c.Col)
			}
			if idx < 0 || idx >= len(doc.Controls) {
				t.Fatalf("invalid control index %d at line=%d col=%d", idx, c.Line, c.Col)
			}
		}
	}
	if !hiddenFound {
		t.Fatal("expected hidden goto control")
	}
}

func TestRenderButtonTag(t *testing.T) {
	html := `<html><body><form action="/go"><button type="submit" name="act" value="ok">Login</button></form></body></html>`
	doc := Render([]byte(html), "https://example.com", 80)

	if len(doc.Controls) != 1 {
		t.Fatalf("expected 1 control, got %d", len(doc.Controls))
	}
	c := doc.Controls[0]
	if c.Kind != "button" || c.Type != "submit" {
		t.Fatalf("expected submit button control, got kind=%q type=%q", c.Kind, c.Type)
	}
	if c.Value != "ok" {
		t.Fatalf("expected button value from value attr, got %q", c.Value)
	}
	if !strings.Contains(docText(doc), "[ ok ]") {
		t.Fatalf("expected rendered button text, got: %q", docText(doc))
	}
}

func TestRenderSelectCheckboxRadio(t *testing.T) {
	html := `<html><body><form>
	<input type="checkbox" name="a" value="1" checked>
	<input type="radio" name="r" value="x">
	<input type="radio" name="r" value="y" checked>
	<select name="s"><option value="one">One</option><option value="two" selected>Two</option></select>
	</form></body></html>`

	doc := Render([]byte(html), "https://example.com", 100)
	if len(doc.Controls) != 4 {
		t.Fatalf("expected 4 controls, got %d", len(doc.Controls))
	}

	if !strings.Contains(docText(doc), "[x]") {
		t.Fatalf("expected checked checkbox marker, got: %q", docText(doc))
	}
	if !strings.Contains(docText(doc), "(*)") {
		t.Fatalf("expected checked radio marker, got: %q", docText(doc))
	}

	last := doc.Controls[len(doc.Controls)-1]
	if last.Kind != "select" {
		t.Fatalf("expected last control to be select, got %q", last.Kind)
	}
	if last.Value != "two" {
		t.Fatalf("expected selected value 'two', got %q", last.Value)
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

func TestRenderNoConsecutiveBlankLines(t *testing.T) {
	html := `<html><body><section><div><p>one</p></div><div><p>two</p></div></section></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	blankRun := 0
	for _, line := range doc.Lines {
		if strings.TrimSpace(lineToText(line)) == "" {
			blankRun++
			if blankRun > 1 {
				t.Fatalf("found consecutive blank lines in output: %q", docText(doc))
			}
		} else {
			blankRun = 0
		}
	}
}

func TestCompactVerticalWhitespaceRemapsLinks(t *testing.T) {
	lines := []Line{
		{},
		{},
		{Spans: []Span{{Text: "hello", LinkIdx: 0}}},
		{},
		{},
		{Spans: []Span{{Text: "world", LinkIdx: 1}}},
		{},
	}
	links := []Link{
		{URL: "https://a.example", Line: 2, Col: 0},
		{URL: "https://b.example", Line: 5, Col: 0},
	}

	outLines, outLinks := compactVerticalWhitespace(lines, links)
	if len(outLines) == 0 {
		t.Fatal("expected non-empty output lines")
	}
	if len(outLinks) != 2 {
		t.Fatalf("expected 2 links after compaction, got %d", len(outLinks))
	}
	if outLinks[0].Line >= len(outLines) || outLinks[1].Line >= len(outLines) {
		t.Fatalf("link lines out of bounds after compaction: %#v", outLinks)
	}
	if strings.TrimSpace(lineToText(outLines[outLinks[0].Line])) != "hello" {
		t.Fatalf("first link should point to 'hello' line, got %q", lineToText(outLines[outLinks[0].Line]))
	}
	if strings.TrimSpace(lineToText(outLines[outLinks[1].Line])) != "world" {
		t.Fatalf("second link should point to 'world' line, got %q", lineToText(outLines[outLinks[1].Line]))
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

func TestNextPrevFocusableIncludesControls(t *testing.T) {
	doc := &Document{
		Lines: []Line{{}},
		Links: []Link{
			{URL: "https://example.com/a", Line: 0, Col: 1},
			{URL: "https://example.com/b", Line: 0, Col: 30},
		},
		Controls: []Control{
			{Kind: "input", Type: "text", Line: 0, Col: 10, Width: 6},
			{Kind: "button", Type: "submit", Line: 0, Col: 20, Width: 5},
			{Kind: "input", Type: "hidden", Line: -1, Col: -1, Width: 0},
			{Kind: "input", Type: "text", Line: 0, Col: 25, Width: 6, Disabled: true},
		},
	}

	line, col, ok := doc.NextFocusable(0, 0)
	if !ok || line != 0 || col != 1 {
		t.Fatalf("expected first focus target at 0:1, got %d:%d ok=%v", line, col, ok)
	}
	line, col, ok = doc.NextFocusable(0, 1)
	if !ok || line != 0 || col != 10 {
		t.Fatalf("expected next focus target at 0:10, got %d:%d ok=%v", line, col, ok)
	}
	line, col, ok = doc.NextFocusable(0, 10)
	if !ok || line != 0 || col != 20 {
		t.Fatalf("expected next focus target at 0:20, got %d:%d ok=%v", line, col, ok)
	}
	line, col, ok = doc.NextFocusable(0, 20)
	if !ok || line != 0 || col != 30 {
		t.Fatalf("expected next focus target at 0:30, got %d:%d ok=%v", line, col, ok)
	}
	line, col, ok = doc.NextFocusable(0, 30)
	if !ok || line != 0 || col != 1 {
		t.Fatalf("expected wrapped focus target at 0:1, got %d:%d ok=%v", line, col, ok)
	}

	line, col, ok = doc.PrevFocusable(0, 30)
	if !ok || line != 0 || col != 20 {
		t.Fatalf("expected previous focus target at 0:20, got %d:%d ok=%v", line, col, ok)
	}
	line, col, ok = doc.PrevFocusable(0, 1)
	if !ok || line != 0 || col != 30 {
		t.Fatalf("expected wrapped previous focus target at 0:30, got %d:%d ok=%v", line, col, ok)
	}
}

func TestNextPrevFocusableIncludesImagePlaceholders(t *testing.T) {
	doc := &Document{
		Lines: []Line{{Spans: []Span{
			{Text: "[IMG]", ImageURL: "https://example.com/photo.png"},
			{Text: "  "},
			{Text: "[SVG]", ImageURL: "https://example.com/icon.svg"},
		}}},
	}

	line, col, ok := doc.NextFocusable(0, -1)
	if !ok || line != 0 || col != 0 {
		t.Fatalf("expected first image focus target at 0:0, got %d:%d ok=%v", line, col, ok)
	}

	line, col, ok = doc.NextFocusable(0, 0)
	if !ok || line != 0 || col != 7 {
		t.Fatalf("expected svg focus target at 0:7, got %d:%d ok=%v", line, col, ok)
	}

	line, col, ok = doc.PrevFocusable(0, 7)
	if !ok || line != 0 || col != 0 {
		t.Fatalf("expected previous focus target at 0:0, got %d:%d ok=%v", line, col, ok)
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
