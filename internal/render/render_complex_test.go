package render

import (
	"strings"
	"testing"
)

const complexHTML = `<!DOCTYPE html>
<html>
<head>
	<title>Test Page</title>
	<style>.hidden{display:none}</style>
	<script>console.log("this should not appear");</script>
</head>
<body>
	<header>
		<nav>
			<a href="/">Home</a> |
			<a href="/about">About</a> |
			<a href="/contact">Contact</a>
		</nav>
	</header>

	<main>
		<h1>Welcome to the Test Page</h1>
		<p>This is a <strong>bold</strong> and <em>italic</em> paragraph with a
		<a href="http://example.com">link to example.com</a>.</p>

		<h2>Features</h2>
		<ul>
			<li>Fast browsing</li>
			<li>Vim keybindings</li>
			<li>Lightweight</li>
		</ul>

		<h2>Versions</h2>
		<ol>
			<li>Version 1.0 - Initial release</li>
			<li>Version 2.0 - Major update</li>
		</ol>

		<h3>Code Example</h3>
		<pre><code>func main() {
    fmt.Println("Hello, world!")
}</code></pre>

		<blockquote>
			<p>The best terminal browser ever made.</p>
		</blockquote>

		<hr>

		<h2>Data Table</h2>
		<table>
			<thead>
				<tr><th>Feature</th><th>Status</th><th>Priority</th></tr>
			</thead>
			<tbody>
				<tr><td>HTML Rendering</td><td>Done</td><td>High</td></tr>
				<tr><td>CSS Support</td><td>Minimal</td><td>Low</td></tr>
				<tr><td>JavaScript</td><td>None</td><td>N/A</td></tr>
			</tbody>
		</table>

		<noscript>
			<p>This page requires JavaScript for full functionality.</p>
		</noscript>

		<p>Here's an image: <img src="/photo.jpg" alt="A beautiful photo"></p>

		<h2>Definition List</h2>
		<dl>
			<dt>wez</dt>
			<dd>A terminal web browser written in Go</dd>
			<dt>tcell</dt>
			<dd>Terminal cell library for Go</dd>
		</dl>
	</main>

	<footer>
		<p>Copyright 2024. Built with <a href="https://golang.org">Go</a>.</p>
	</footer>
</body>
</html>`

func TestComplexPageRender(t *testing.T) {
	doc := Render([]byte(complexHTML), "http://example.com", 80)

	text := docText(doc)

	// Title should be extracted.
	if doc.Title != "Test Page" {
		t.Errorf("expected title 'Test Page', got %q", doc.Title)
	}

	// Script/style content should not appear.
	if strings.Contains(text, "console.log") {
		t.Error("script content should not appear")
	}
	if strings.Contains(text, ".hidden") {
		t.Error("style content should not appear")
	}

	// Headings.
	if !strings.Contains(text, "# Welcome to the Test Page") {
		t.Error("expected h1 heading with #")
	}
	if !strings.Contains(text, "## Features") {
		t.Error("expected h2 heading with ##")
	}

	// Links: should have at least the nav links + body links + footer link.
	if len(doc.Links) < 5 {
		t.Errorf("expected at least 5 links, got %d", len(doc.Links))
	}

	// Noscript should be visible with marker.
	if !strings.Contains(text, "NOSCRIPT") {
		t.Error("expected NOSCRIPT marker")
	}
	if !strings.Contains(text, "JavaScript for full functionality") {
		t.Error("expected noscript content")
	}

	// Image placeholder.
	if !strings.Contains(text, "[IMG: A beautiful photo]") {
		t.Error("expected image placeholder with alt text")
	}

	// Table content.
	if !strings.Contains(text, "Feature") {
		t.Error("expected table header 'Feature'")
	}
	if !strings.Contains(text, "HTML Rendering") {
		t.Error("expected table cell 'HTML Rendering'")
	}

	// Preformatted code.
	if !strings.Contains(text, "fmt.Println") {
		t.Error("expected preformatted code content")
	}

	// Lists.
	if !strings.Contains(text, "Fast browsing") {
		t.Error("expected list item content")
	}
	if !strings.Contains(text, "1.") {
		t.Error("expected ordered list number")
	}

	// Horizontal rule.
	if !strings.Contains(text, "─") {
		t.Error("expected horizontal rule")
	}

	// Blockquote marker.
	if !strings.Contains(text, "|") {
		t.Error("expected blockquote marker")
	}

	// No line should exceed width.
	for i, line := range doc.Lines {
		w := line.Width()
		if w > 80 {
			t.Errorf("line %d exceeds width 80: width=%d, text=%q", i, w, lineToText(line))
		}
	}
}

func TestComplexPageNarrowWidth(t *testing.T) {
	doc := Render([]byte(complexHTML), "http://example.com", 40)

	// No line should exceed narrow width.
	for i, line := range doc.Lines {
		w := line.Width()
		if w > 40 {
			t.Errorf("line %d exceeds width 40: width=%d, text=%q", i, w, lineToText(line))
		}
	}

	// Should still have all content.
	text := docText(doc)
	if !strings.Contains(text, "Welcome") {
		t.Error("expected content present at narrow width")
	}
}

func TestRelativeURLResolution(t *testing.T) {
	html := `<html><body>
		<a href="/page">Absolute path</a>
		<a href="relative">Relative path</a>
		<a href="../up">Parent path</a>
		<a href="http://other.com">Full URL</a>
		<a href="#section">Fragment</a>
	</body></html>`

	doc := Render([]byte(html), "http://example.com/dir/page.html", 80)

	expected := map[string]string{
		"Absolute path": "http://example.com/page",
		"Relative path": "http://example.com/dir/relative",
		"Parent path":   "http://example.com/up",
		"Full URL":      "http://other.com",
		"Fragment":      "http://example.com/dir/page.html#section",
	}

	for _, link := range doc.Links {
		// Find what text this link covers.
		for linkText, expectedURL := range expected {
			if link.URL == expectedURL {
				delete(expected, linkText)
			}
		}
	}

	for linkText, expectedURL := range expected {
		t.Errorf("link %q: expected URL %q not found in doc links", linkText, expectedURL)
	}
}

func TestEmptyDocument(t *testing.T) {
	doc := Render([]byte(""), "http://example.com", 80)
	if doc == nil {
		t.Fatal("expected non-nil document for empty input")
	}
}

func TestMalformedHTML(t *testing.T) {
	html := `<html><body><p>Unclosed paragraph<div>Mismatched tags</p></div></body></html>`
	doc := Render([]byte(html), "http://example.com", 80)

	text := docText(doc)
	if !strings.Contains(text, "Unclosed paragraph") {
		t.Error("expected text from malformed HTML to be rendered")
	}
}

func lineToText(line Line) string {
	var sb strings.Builder
	for _, span := range line.Spans {
		sb.WriteString(span.Text)
	}
	return sb.String()
}
