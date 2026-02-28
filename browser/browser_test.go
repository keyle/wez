package browser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"wez/config"
	"wez/fetch"
	"wez/history"
	"wez/render"
	"wez/ui"
)

func TestExtractMetaRefreshURLAbsolute(t *testing.T) {
	body := []byte(`<!doctype html><html><head><meta http-equiv="refresh" content="0;url=https://example.com/final?q=x"></head><body>redir</body></html>`)

	got, ok := extractMetaRefreshURL(body, "https://example.com/start")
	if !ok {
		t.Fatal("expected meta refresh URL")
	}
	if got != "https://example.com/final?q=x" {
		t.Fatalf("unexpected redirect URL: %q", got)
	}
}

func TestExtractMetaRefreshURLNoscriptRelative(t *testing.T) {
	body := []byte(`<!doctype html><html><body><noscript><meta http-equiv="refresh" content="0; URL='/html/?q=wez'"></noscript></body></html>`)

	got, ok := extractMetaRefreshURL(body, "https://duckduckgo.com/lite/")
	if !ok {
		t.Fatal("expected meta refresh URL in noscript")
	}
	if got != "https://duckduckgo.com/html/?q=wez" {
		t.Fatalf("unexpected resolved redirect URL: %q", got)
	}
}

func TestExtractMetaRefreshURLRejectsUnsupportedScheme(t *testing.T) {
	body := []byte(`<!doctype html><html><head><meta http-equiv="refresh" content="0;url=javascript:alert(1)"></head></html>`)

	_, ok := extractMetaRefreshURL(body, "https://example.com/")
	if ok {
		t.Fatal("expected javascript: redirect to be ignored")
	}
}

func TestDumpURLRendersHTML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>Hello</h1><p>Terminal browser</p></body></html>`))
	}))
	defer ts.Close()

	out, err := DumpURL(config.Default(), ts.URL, 80)
	if err != nil {
		t.Fatalf("unexpected dump error: %v", err)
	}
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "Terminal browser") {
		t.Fatalf("expected rendered content in dump, got: %q", out)
	}
}

func TestDumpURLFollowsMetaRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><meta http-equiv="refresh" content="0; url=/final"></head><body>redir</body></html>`))
		case "/final":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><p>Final page</p></body></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	cfg := config.Default()
	cfg.FollowMetaRedirects = true
	out, err := DumpURL(cfg, ts.URL+"/start", 80)
	if err != nil {
		t.Fatalf("unexpected dump error: %v", err)
	}
	if !strings.Contains(out, "Final page") {
		t.Fatalf("expected redirected final page, got: %q", out)
	}
}

func TestBuildFormSubmissionIncludesHiddenAndClickedSubmit(t *testing.T) {
	doc := &render.Document{
		URL: "https://news.ycombinator.com/login",
		Forms: []render.Form{{
			Method:   "POST",
			Action:   "https://news.ycombinator.com/login",
			Enctype:  "application/x-www-form-urlencoded",
			Controls: []int{0, 1, 2, 3},
		}},
		Controls: []render.Control{
			{Kind: "input", Type: "hidden", FormIdx: 0, Name: "goto", Value: "news"},
			{Kind: "input", Type: "text", FormIdx: 0, Name: "acct", Value: "alice"},
			{Kind: "input", Type: "password", FormIdx: 0, Name: "pw", Value: "secret"},
			{Kind: "input", Type: "submit", FormIdx: 0, Name: "do", Value: "login"},
		},
	}

	sub, err := buildFormSubmission(doc, 3)
	if err != nil {
		t.Fatalf("unexpected submission error: %v", err)
	}
	if sub.Method != "POST" {
		t.Fatalf("expected POST method, got %q", sub.Method)
	}
	if sub.ActionURL != "https://news.ycombinator.com/login" {
		t.Fatalf("unexpected action URL: %q", sub.ActionURL)
	}

	want := url.Values{
		"goto": []string{"news"},
		"acct": []string{"alice"},
		"pw":   []string{"secret"},
		"do":   []string{"login"},
	}
	if sub.Values.Encode() != want.Encode() {
		t.Fatalf("unexpected encoded values: got %q want %q", sub.Values.Encode(), want.Encode())
	}
}

func TestBuildFormSubmissionCheckboxRadioSelect(t *testing.T) {
	doc := &render.Document{
		URL: "https://example.com/form",
		Forms: []render.Form{{
			Method:   "GET",
			Action:   "https://example.com/search",
			Controls: []int{0, 1, 2, 3},
		}},
		Controls: []render.Control{
			{Kind: "input", Type: "checkbox", FormIdx: 0, Name: "a", Value: "1", Checked: true},
			{Kind: "input", Type: "radio", FormIdx: 0, Name: "r", Value: "x", Checked: false},
			{Kind: "input", Type: "radio", FormIdx: 0, Name: "r", Value: "y", Checked: true},
			{Kind: "select", Type: "select", FormIdx: 0, Name: "s", Options: []render.ControlOption{
				{Value: "one", Label: "One", Selected: false},
				{Value: "two", Label: "Two", Selected: true},
			}},
		},
	}

	sub, err := buildFormSubmission(doc, 0)
	if err != nil {
		t.Fatalf("unexpected submission error: %v", err)
	}
	if sub.Method != "GET" {
		t.Fatalf("expected GET method, got %q", sub.Method)
	}

	if got := sub.Values.Get("a"); got != "1" {
		t.Fatalf("expected checkbox value 1, got %q", got)
	}
	if got := sub.Values.Get("r"); got != "y" {
		t.Fatalf("expected checked radio value y, got %q", got)
	}
	if got := sub.Values.Get("s"); got != "two" {
		t.Fatalf("expected selected option two, got %q", got)
	}
}

func TestBuildFormSubmissionRejectsJavaScriptAction(t *testing.T) {
	doc := &render.Document{
		URL: "https://example.com/form",
		Forms: []render.Form{{
			Method:   "POST",
			Action:   "javascript:void(0)",
			Controls: []int{0},
		}},
		Controls: []render.Control{{Kind: "input", Type: "submit", FormIdx: 0, Name: "do", Value: "go"}},
	}

	if _, err := buildFormSubmission(doc, 0); err == nil {
		t.Fatal("expected javascript action to be rejected")
	}
}

func TestIsAboutWelcomeURL(t *testing.T) {
	if !isAboutWelcomeURL("about:welcome") {
		t.Fatal("expected exact about:welcome to match")
	}
	if !isAboutWelcomeURL(" ABOUT:WELCOME ") {
		t.Fatal("expected case/space variant to match")
	}
	if isAboutWelcomeURL("about:history") {
		t.Fatal("did not expect about:history to match")
	}
}

func TestIsAboutHistoryURL(t *testing.T) {
	if !isAboutHistoryURL("about:history") {
		t.Fatal("expected about:history to match")
	}
	if !isAboutHistoryURL(" ABOUT:HISTORY ") {
		t.Fatal("expected case/space variant to match")
	}
	if isAboutHistoryURL("about:bookmarks") {
		t.Fatal("did not expect about:bookmarks to match history")
	}
}

func TestIsAboutBookmarksURL(t *testing.T) {
	if !isAboutBookmarksURL("about:bookmarks") {
		t.Fatal("expected about:bookmarks to match")
	}
	if !isAboutBookmarksURL(" ABOUT:BOOKMARKS ") {
		t.Fatal("expected case/space variant to match")
	}
	if isAboutBookmarksURL("about:favorites") {
		t.Fatal("did not expect legacy about:favorites to match")
	}
}

func TestRecentHistoryEntries(t *testing.T) {
	entries := []history.Entry{
		{URL: "https://a.example"},
		{URL: "https://b.example"},
		{URL: "https://c.example"},
		{URL: "https://d.example"},
		{URL: "https://e.example"},
		{URL: "https://f.example"},
	}

	out := recentHistoryEntries(entries, 5)
	if len(out) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(out))
	}
	if out[0].URL != "https://f.example" || out[4].URL != "https://b.example" {
		t.Fatalf("unexpected order: first=%q last=%q", out[0].URL, out[4].URL)
	}

	out = recentHistoryEntries(entries, 20)
	if len(out) != len(entries) {
		t.Fatalf("expected all entries when max too large, got %d", len(out))
	}

	out = recentHistoryEntries(nil, 5)
	if len(out) != 0 {
		t.Fatalf("expected empty output for nil input, got %d", len(out))
	}
}

func TestCurrentNavStateUsesUnderlyingPagePositionInAuxView(t *testing.T) {
	b := &Browser{
		currentURL:  "https://example.com/item",
		viewActive:  true,
		viewScrollY: 7,
		viewCursorY: 12,
		viewCursorX: 4,
		UI: &ui.UI{
			ScrollY: 99,
			CursorY: 99,
			CursorX: 99,
		},
	}

	state, ok := b.currentNavState()
	if !ok {
		t.Fatal("expected current nav state")
	}
	if state.URL != "https://example.com/item" {
		t.Fatalf("unexpected URL: %q", state.URL)
	}
	if state.ScrollY != 7 || state.CursorY != 12 || state.CursorX != 4 {
		t.Fatalf("expected aux-view saved position, got scroll=%d cursor=%d:%d", state.ScrollY, state.CursorY, state.CursorX)
	}
}

func TestApplyNavigationTransitionStoresCursorPosition(t *testing.T) {
	b := &Browser{
		UI:         &ui.UI{ScrollY: 5, CursorY: 9, CursorX: 3},
		currentURL: "https://example.com/a",
	}

	prev, ok := b.currentNavState()
	if !ok {
		t.Fatal("expected current state before navigation")
	}

	b.applyNavigationTransition("https://example.com/b", defaultNavigateOptions(), prev, ok)

	if len(b.backStack) != 1 {
		t.Fatalf("expected one back-stack entry, got %d", len(b.backStack))
	}
	got := b.backStack[0]
	if got.URL != "https://example.com/a" || got.ScrollY != 5 || got.CursorY != 9 || got.CursorX != 3 {
		t.Fatalf("unexpected saved nav state: %+v", got)
	}
}

func TestApplyFetchResultRestoresViewport(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	defer sim.Fini()
	sim.SetSize(80, 24)

	cfg := config.Default()
	b := &Browser{
		UI:      &ui.UI{Screen: sim, Cfg: cfg},
		Cfg:     cfg,
		History: history.New(),
	}

	body := "line0\nline1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\nline9\nline10\nline11\nline12\nline13\nline14\nline15\nline16\nline17\nline18\nline19\nline20\nline21\nline22\nline23\nline24\nline25\nline26\nline27\nline28\nline29"
	result := &fetch.Result{
		FinalURL:    "https://example.com/plain",
		StatusCode:  200,
		ContentType: "text/plain",
		Body:        []byte(body),
	}
	restore := &navState{URL: "https://example.com/plain", ScrollY: 5, CursorY: 9, CursorX: 2}

	b.applyFetchResult(result, restore)

	if b.UI.ScrollY != 5 || b.UI.CursorY != 9 || b.UI.CursorX != 2 {
		t.Fatalf("expected restored viewport 5/9:2, got %d/%d:%d", b.UI.ScrollY, b.UI.CursorY, b.UI.CursorX)
	}
}

func TestCaptureSnapshotTrimsToMaxPageBuffers(t *testing.T) {
	b := &Browser{UI: &ui.UI{}}
	ids := make([]int, 0, maxPageBuffers+5)

	for i := 0; i < maxPageBuffers+5; i++ {
		b.UI.Doc = &render.Document{
			URL:   fmt.Sprintf("https://example.com/%d", i),
			Lines: []render.Line{{Spans: []render.Span{{Text: "x", LinkIdx: -1}}}},
		}
		ids = append(ids, b.captureSnapshot())
	}

	if len(b.snapshotIDs) != maxPageBuffers {
		t.Fatalf("expected %d snapshots, got %d", maxPageBuffers, len(b.snapshotIDs))
	}
	if len(b.snapshotByID) != maxPageBuffers {
		t.Fatalf("expected %d snapshot entries, got %d", maxPageBuffers, len(b.snapshotByID))
	}

	if _, ok := b.snapshotByID[ids[0]]; ok {
		t.Fatalf("expected oldest snapshot %d to be evicted", ids[0])
	}
	if _, ok := b.snapshotByID[ids[len(ids)-1]]; !ok {
		t.Fatalf("expected newest snapshot %d to remain", ids[len(ids)-1])
	}
}

func TestRestoreFromSnapshotUsesBufferedDocument(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	defer sim.Fini()
	sim.SetSize(80, 24)

	cfg := config.Default()
	b := &Browser{
		UI: &ui.UI{Screen: sim, Cfg: cfg},
	}

	docA := &render.Document{
		URL:   "https://example.com/a",
		Title: "Page A",
		Lines: []render.Line{{Spans: []render.Span{{Text: "A", LinkIdx: -1}}}},
	}
	b.currentURL = docA.URL
	b.sourceURL = docA.URL
	b.sourceBody = "source-a"
	b.UI.SetDocument(docA)
	b.UI.RestoreViewport(0, 0, 0)

	state, ok := b.currentNavState()
	if !ok {
		t.Fatal("expected current nav state")
	}
	state = b.navStateWithSnapshot(state)
	state.ScrollY = 0
	state.CursorY = 0
	state.CursorX = 0

	docB := &render.Document{
		URL:   "https://example.com/b",
		Title: "Page B",
		Lines: []render.Line{{Spans: []render.Span{{Text: "B", LinkIdx: -1}}}},
	}
	b.currentURL = docB.URL
	b.sourceURL = docB.URL
	b.sourceBody = "source-b"
	b.UI.SetDocument(docB)

	if !b.restoreFromSnapshot(state) {
		t.Fatal("expected snapshot restore to succeed")
	}
	if b.currentURL != docA.URL {
		t.Fatalf("expected current URL %q, got %q", docA.URL, b.currentURL)
	}
	if b.UI.Doc != docA {
		t.Fatal("expected buffered document to be restored")
	}
	if b.sourceBody != "source-a" {
		t.Fatalf("expected buffered source body to be restored, got %q", b.sourceBody)
	}
}

func TestRestoreFromSnapshotRefreshesVisitedLinkStyles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	defer sim.Fini()
	sim.SetSize(80, 24)

	cfg := config.Default()
	seen := history.NewSeen()
	if err := seen.Add("https://example.com/visited"); err != nil {
		t.Fatalf("failed adding seen URL: %v", err)
	}

	b := &Browser{
		UI:   &ui.UI{Screen: sim, Cfg: cfg},
		Seen: seen,
	}

	docA := &render.Document{
		URL: "https://example.com/a",
		Lines: []render.Line{{Spans: []render.Span{{
			Text:    "visited link",
			Style:   render.SpanStyle{Underline: true, Color: "link"},
			LinkIdx: 0,
		}}}},
		Links: []render.Link{{URL: "https://example.com/visited", Line: 0, Col: 0}},
	}
	b.currentURL = docA.URL
	b.UI.SetDocument(docA)

	state, ok := b.currentNavState()
	if !ok {
		t.Fatal("expected current nav state")
	}
	state = b.navStateWithSnapshot(state)

	b.UI.SetDocument(&render.Document{URL: "https://example.com/b", Lines: []render.Line{{}}})
	b.currentURL = "https://example.com/b"

	if !b.restoreFromSnapshot(state) {
		t.Fatal("expected snapshot restore to succeed")
	}
	if got := b.UI.Doc.Lines[0].Spans[0].Style.Color; got != "visited_link" {
		t.Fatalf("expected restored link color visited_link, got %q", got)
	}
}

func TestNavigateToSameDocumentAnchorJumpsWithoutReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	defer sim.Fini()
	sim.SetSize(80, 24)

	cfg := config.Default()
	lines := make([]render.Line, 0, 40)
	for i := 0; i < 40; i++ {
		lines = append(lines, render.Line{Spans: []render.Span{{Text: fmt.Sprintf("line-%d", i), LinkIdx: -1}}})
	}
	doc := &render.Document{
		URL:     "https://example.com/item?id=1",
		Lines:   lines,
		Anchors: map[string]int{"thread-2": 17},
	}

	b := &Browser{UI: &ui.UI{Screen: sim, Cfg: cfg}, currentURL: doc.URL}
	b.UI.SetDocument(doc)
	b.UI.RestoreViewport(3, 3, 0)

	b.navigateWithOptions("#thread-2", defaultNavigateOptions())

	if b.currentURL != "https://example.com/item?id=1#thread-2" {
		t.Fatalf("unexpected current URL after anchor jump: %q", b.currentURL)
	}
	if b.UI.ScrollY != 17 || b.UI.CursorY != 17 {
		t.Fatalf("expected anchor jump to line 17, got scroll=%d cursor=%d", b.UI.ScrollY, b.UI.CursorY)
	}
	if len(b.backStack) != 1 {
		t.Fatalf("expected one history entry for anchor jump, got %d", len(b.backStack))
	}
	if b.backStack[0].URL != "https://example.com/item?id=1" {
		t.Fatalf("unexpected back-stack URL: %q", b.backStack[0].URL)
	}
}

func TestNavigateToSameDocumentAnchorWithoutTargetShowsStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		t.Fatalf("failed to init simulation screen: %v", err)
	}
	defer sim.Fini()
	sim.SetSize(80, 24)

	cfg := config.Default()
	doc := &render.Document{URL: "https://example.com/item?id=1", Lines: []render.Line{{Spans: []render.Span{{Text: "x", LinkIdx: -1}}}}}
	b := &Browser{UI: &ui.UI{Screen: sim, Cfg: cfg}, currentURL: doc.URL}
	b.UI.SetDocument(doc)

	b.navigateWithOptions("#missing", defaultNavigateOptions())

	if !strings.Contains(b.UI.StatusMsg, "Anchor not found") {
		t.Fatalf("expected missing-anchor status, got %q", b.UI.StatusMsg)
	}
	if b.currentURL != "https://example.com/item?id=1#missing" {
		t.Fatalf("expected current URL to track missing anchor, got %q", b.currentURL)
	}
}

func TestIsSVGContentType(t *testing.T) {
	tests := []struct {
		name string
		ct   string
		want bool
	}{
		{name: "svg mime", ct: "image/svg+xml", want: true},
		{name: "svg mime with charset", ct: "image/svg+xml; charset=utf-8", want: true},
		{name: "png", ct: "image/png", want: false},
		{name: "html", ct: "text/html", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSVGContentType(tc.ct); got != tc.want {
				t.Fatalf("isSVGContentType(%q) = %v, want %v", tc.ct, got, tc.want)
			}
		})
	}
}

func TestIsSVGImageURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "svg extension", raw: "https://example.com/icon.svg", want: true},
		{name: "svg extension with query", raw: "https://example.com/icon.svg?size=32", want: true},
		{name: "svgz extension", raw: "https://example.com/icon.svgz#hash", want: true},
		{name: "data uri svg", raw: "data:image/svg+xml;base64,PHN2Zz4=", want: true},
		{name: "png extension", raw: "https://example.com/photo.png", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSVGImageURL(tc.raw); got != tc.want {
				t.Fatalf("isSVGImageURL(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
