package browser

import (
	"bufio"
	"bytes"
	"fmt"
	"mime"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"wez/config"
	"wez/favorites"
	"wez/fetch"
	"wez/history"
	"wez/keymap"
	"wez/render"
	"wez/ui"
)

var (
	metaTagRe       = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	contentAttrRe   = regexp.MustCompile(`(?is)\bcontent\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	httpEquivAttrRe = regexp.MustCompile(`(?is)\bhttp-equiv\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
)

const (
	maxMetaRedirectHops = 8
	aboutWelcomeURL     = "about:welcome"
	aboutHistoryURL     = "about:history"
	aboutBookmarksURL   = "about:bookmarks"
)

type formSubmission struct {
	Method    string
	ActionURL string
	Values    url.Values
}

type navState struct {
	URL     string
	ScrollY int
	CursorY int
	CursorX int
}

type navigateOptions struct {
	pushCurrent  bool
	clearForward bool
	restore      *navState
}

func defaultNavigateOptions() navigateOptions {
	return navigateOptions{pushCurrent: true, clearForward: true}
}

// Browser ties all components together.
type Browser struct {
	UI      *ui.UI
	Cfg     config.Config
	History *history.History
	Seen    *history.Seen
	Favs    *favorites.Store
	Fetcher *fetch.Client

	// Navigation stacks.
	backStack    []navState
	forwardStack []navState
	currentURL   string
	sourceURL    string
	sourceBody   string

	viewActive  bool
	viewKind    string
	viewPrevDoc *render.Document
	viewScrollY int
	viewCursorY int
	viewCursorX int
}

// New creates a new Browser instance.
func New(cfg config.Config, km *keymap.Keymap) (*Browser, error) {
	tui, err := ui.New(cfg, km)
	if err != nil {
		return nil, err
	}

	hist := history.New()
	_ = hist.Load()
	seen := history.NewSeen()
	_ = seen.Load()
	favs := favorites.New()
	_ = favs.Load()

	return &Browser{
		UI:      tui,
		Cfg:     cfg,
		History: hist,
		Seen:    seen,
		Favs:    favs,
		Fetcher: fetch.NewClientWithOptions(fetch.ClientOptions{
			PersistCookies:        cfg.PersistCookies,
			CookieJarPath:         cfg.CookieJarPath,
			PersistSessionCookies: cfg.PersistSessionCookies,
		}),
	}, nil
}

// Navigate fetches and renders a URL.
func (b *Browser) Navigate(rawURL string) {
	b.navigateWithOptions(rawURL, defaultNavigateOptions())
}

func (b *Browser) navigateWithOptions(rawURL string, opts navigateOptions) {
	if isAboutWelcomeURL(rawURL) {
		b.navigateInternalPage(aboutWelcomeURL, b.ShowWelcome, opts)
		return
	}
	if isAboutHistoryURL(rawURL) {
		b.navigateInternalPage(aboutHistoryURL, func() {
			b.UI.SetDocument(b.buildHistoryDoc())
		}, opts)
		return
	}
	if isAboutBookmarksURL(rawURL) {
		b.navigateInternalPage(aboutBookmarksURL, func() {
			b.UI.SetDocument(b.buildFavoritesDoc())
		}, opts)
		return
	}
	if isJavaScriptURL(rawURL) {
		b.UI.SetStatus("Ignored javascript URL")
		return
	}

	prev, hasPrev := b.currentNavState()
	b.leaveAuxView()

	result, err := fetchWithMetaRedirects(rawURL, b.Cfg.FollowMetaRedirects, func(u string) (*fetch.Result, error) {
		return b.fetchWithStatusAnimation(u, "Loading")
	})
	if err != nil {
		b.showError(fmt.Sprintf("Error loading %s: %v", rawURL, err))
		return
	}

	if !shouldDownload(result.ContentType) {
		b.applyNavigationTransition(result.FinalURL, opts, prev, hasPrev)
	}
	b.applyFetchResult(result, opts.restore)
}

func (b *Browser) navigateInternalPage(targetURL string, renderFn func(), opts navigateOptions) {
	prev, hasPrev := b.currentNavState()
	b.leaveAuxView()
	b.applyNavigationTransition(targetURL, opts, prev, hasPrev)
	b.currentURL = targetURL
	renderFn()
	if opts.restore != nil {
		b.UI.RestoreViewport(opts.restore.ScrollY, opts.restore.CursorY, opts.restore.CursorX)
	}
	b.UI.SetStatus("")
}

func (b *Browser) applyFetchResult(result *fetch.Result, restore *navState) {
	if shouldDownload(result.ContentType) {
		b.sourceURL = ""
		b.sourceBody = ""
		savedPath, saveErr := b.saveDownload(result)
		if saveErr != nil {
			b.showError(fmt.Sprintf("download failed: %v", saveErr))
			return
		}
		if b.UI.Doc == nil {
			b.ShowWelcome()
		}
		b.UI.SetStatusAlert("Downloaded to " + savedPath)
		return
	}

	statusMsg := ""
	if result.StatusCode >= 400 {
		statusMsg = fmt.Sprintf("HTTP %d", result.StatusCode)
	}

	w, _ := b.UI.Screen.Size()
	if b.Seen != nil {
		_ = b.Seen.Add(result.FinalURL)
	}

	var doc *render.Document
	ct := strings.ToLower(result.ContentType)
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") {
		doc = render.RenderWithVisited(result.Body, result.FinalURL, w, b.isURLSeen)
	} else {
		doc = render.RenderPlainText(result.Body, result.FinalURL, w)
	}

	b.sourceURL = result.FinalURL
	b.sourceBody = normalizeSourceBody(result.Body)

	b.currentURL = result.FinalURL

	// Record in history.
	_ = b.History.Add(result.FinalURL, doc.Title)

	b.UI.SetDocument(doc)
	if restore != nil {
		b.UI.RestoreViewport(restore.ScrollY, restore.CursorY, restore.CursorX)
	}
	b.UI.SetStatus(statusMsg)
}

func (b *Browser) leaveAuxView() {
	if !b.viewActive {
		return
	}
	b.viewActive = false
	b.viewKind = ""
	b.viewPrevDoc = nil
}

func (b *Browser) currentNavState() (navState, bool) {
	url := strings.TrimSpace(b.currentURL)
	if url == "" {
		return navState{}, false
	}

	state := navState{URL: url}
	if b.viewActive {
		state.ScrollY = b.viewScrollY
		state.CursorY = b.viewCursorY
		state.CursorX = b.viewCursorX
		return state, true
	}
	if b.UI != nil {
		state.ScrollY = b.UI.ScrollY
		state.CursorY = b.UI.CursorY
		state.CursorX = b.UI.CursorX
	}
	return state, true
}

func (b *Browser) applyNavigationTransition(targetURL string, opts navigateOptions, prev navState, hasPrev bool) {
	if opts.pushCurrent && hasPrev {
		if strings.TrimSpace(targetURL) == "" || prev.URL != targetURL {
			b.backStack = append(b.backStack, prev)
		}
	}
	if opts.clearForward {
		b.forwardStack = nil
	}
}

// DumpURL fetches and renders a URL into plain text output.
func DumpURL(cfg config.Config, rawURL string, width int) (string, error) {
	if width <= 0 {
		width = 80
	}

	result, err := fetchWithMetaRedirects(rawURL, cfg.FollowMetaRedirects, fetch.Fetch)
	if err != nil {
		return "", err
	}

	if shouldDownload(result.ContentType) {
		return "", fmt.Errorf("cannot dump binary content type %q", result.ContentType)
	}

	var doc *render.Document
	ct := strings.ToLower(result.ContentType)
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") {
		doc = render.Render(result.Body, result.FinalURL, width)
	} else {
		doc = render.RenderPlainText(result.Body, result.FinalURL, width)
	}

	var sb strings.Builder
	for i, line := range doc.Lines {
		var lineSB strings.Builder
		for _, span := range line.Spans {
			lineSB.WriteString(span.Text)
		}
		lineText := strings.TrimRight(lineSB.String(), " \t")
		sb.WriteString(lineText)
		if i < len(doc.Lines)-1 {
			sb.WriteByte('\n')
		}
	}

	return sb.String(), nil
}

// Run starts the main event loop. If initialURL is non-empty, navigates there first.
func (b *Browser) Run(initialURL string) {
	defer b.UI.Close()
	defer b.Close()

	if initialURL != "" {
		b.Navigate(initialURL)
	} else {
		b.ShowWelcome()
	}

	for {
		b.UI.Draw()

		ev := b.UI.Screen.PollEvent()
		action := b.UI.HandleEvent(ev)

		switch action {
		case ui.ActionQuit:
			return

		case ui.ActionEscape:
			if b.viewActive {
				b.exitHistoryView()
			}

		case ui.ActionNavigate:
			url := strings.TrimSpace(b.UI.InputBuffer)
			if url != "" {
				b.Navigate(url)
			}

		case ui.ActionFollowLink:
			if b.UI.Doc != nil {
				_, linkURL, ok := b.UI.Doc.LinkAt(b.UI.CursorY, b.UI.CursorX)
				if ok {
					if strings.HasPrefix(linkURL, "mailto:") {
						b.openMailto(linkURL)
					} else {
						b.Navigate(linkURL)
					}
					break
				}

				if controlIdx, ok := b.UI.Doc.ControlAt(b.UI.CursorY, b.UI.CursorX); ok {
					b.activateControl(controlIdx)
				}
			}

		case ui.ActionCommitFormInput:
			b.UI.ApplyFormInput()

		case ui.ActionAddFavorite:
			b.addCurrentFavorite(strings.TrimSpace(b.UI.InputBuffer))

		case ui.ActionRemoveFavorite:
			b.removeFavoriteAtCursorOrCurrent()

		case ui.ActionBack:
			if len(b.backStack) > 0 {
				if current, ok := b.currentNavState(); ok {
					b.forwardStack = append(b.forwardStack, current)
				}
				prev := b.backStack[len(b.backStack)-1]
				b.backStack = b.backStack[:len(b.backStack)-1]
				opts := navigateOptions{pushCurrent: false, clearForward: false, restore: &prev}
				b.navigateWithOptions(prev.URL, opts)
			} else {
				b.UI.SetStatus("No previous page")
			}

		case ui.ActionForward:
			if len(b.forwardStack) > 0 {
				if current, ok := b.currentNavState(); ok {
					b.backStack = append(b.backStack, current)
				}
				next := b.forwardStack[len(b.forwardStack)-1]
				b.forwardStack = b.forwardStack[:len(b.forwardStack)-1]
				opts := navigateOptions{pushCurrent: false, clearForward: false, restore: &next}
				b.navigateWithOptions(next.URL, opts)
			} else {
				b.UI.SetStatus("No next page")
			}

		case ui.ActionOpenWelcome:
			b.Navigate(aboutWelcomeURL)

		case ui.ActionReload:
			if current, ok := b.currentNavState(); ok {
				opts := navigateOptions{pushCurrent: false, clearForward: false, restore: &current}
				b.navigateWithOptions(current.URL, opts)
			}

		case ui.ActionOpenImage:
			if b.UI.Doc != nil {
				imgURL, ok := b.UI.Doc.ImageAt(b.UI.CursorY, b.UI.CursorX)
				if ok {
					b.openImage(imgURL)
				} else {
					b.UI.SetStatus("No image under cursor")
				}
			}

		case ui.ActionSearch:
			b.UI.SearchTerm = b.UI.InputBuffer
			b.UI.PerformSearch()

		case ui.ActionWebSearch:
			query := strings.TrimSpace(b.UI.InputBuffer)
			if query != "" {
				b.Navigate(b.Cfg.SearchURL(query))
			}

		case ui.ActionSearchNext:
			b.UI.NextSearchMatch()

		case ui.ActionSearchPrev:
			b.UI.PrevSearchMatch()

		case ui.ActionYankURL:
			b.UI.Yank()

		case ui.ActionYankLinkURL:
			b.UI.YankLinkURL()

		case ui.ActionOpenHistory:
			b.openHistoryView()

		case ui.ActionOpenFavorites:
			b.openFavoritesView()

		case ui.ActionShowSource:
			b.openSourceView()

		case ui.ActionClearCache:
			b.clearCache()

		case ui.ActionClearHistory:
			b.clearHistory()
		}
	}
}

func (b *Browser) Close() {
	if b == nil || b.Fetcher == nil {
		return
	}
	if err := b.Fetcher.Close(); err != nil {
		if b.UI != nil {
			b.UI.SetStatus("cookie save error: " + err.Error())
		}
	}
}

func (b *Browser) isURLSeen(rawURL string) bool {
	if b.Seen == nil {
		return false
	}
	return b.Seen.Contains(rawURL)
}

func (b *Browser) newFetcher() *fetch.Client {
	return fetch.NewClientWithOptions(fetch.ClientOptions{
		PersistCookies:        b.Cfg.PersistCookies,
		CookieJarPath:         b.Cfg.CookieJarPath,
		PersistSessionCookies: b.Cfg.PersistSessionCookies,
	})
}

func (b *Browser) openHistoryView() {
	b.enterAuxView("history")

	b.UI.SetDocument(b.buildHistoryDoc())
	b.UI.SetStatus("History view (Esc to return)")
}

func (b *Browser) openFavoritesView() {
	b.enterAuxView("favorites")

	b.UI.SetDocument(b.buildFavoritesDoc())
	b.UI.SetStatus("Bookmarks view (Esc to return)")
}

func (b *Browser) openSourceView() {
	if b.UI.Doc == nil {
		b.UI.SetStatus("No source available")
		return
	}

	docURL := strings.TrimSpace(b.UI.Doc.URL)
	if docURL == "" || strings.HasPrefix(strings.ToLower(docURL), "about:") {
		b.UI.SetStatus("No source available for this page")
		return
	}
	if b.sourceURL == "" || b.sourceBody == "" || b.sourceURL != docURL {
		b.UI.SetStatus("No source available for this page")
		return
	}

	b.enterAuxView("source")
	b.UI.SetDocument(b.buildSourceDoc())
	b.UI.SetStatus("Source view (Esc to return)")
}

func (b *Browser) enterAuxView(kind string) {
	if b.viewActive {
		b.viewKind = kind
		return
	}

	b.viewActive = true
	b.viewKind = kind
	b.viewPrevDoc = b.UI.Doc
	b.viewScrollY = b.UI.ScrollY
	b.viewCursorY = b.UI.CursorY
	b.viewCursorX = b.UI.CursorX
}

func (b *Browser) exitHistoryView() {
	if !b.viewActive {
		return
	}
	b.viewActive = false
	b.viewKind = ""

	if b.viewPrevDoc != nil {
		b.UI.SetDocument(b.viewPrevDoc)
		b.UI.ScrollY = b.viewScrollY
		b.UI.CursorY = b.viewCursorY
		b.UI.CursorX = b.viewCursorX
	} else {
		b.ShowWelcome()
	}
	b.UI.SetStatus("")
}

func (b *Browser) addCurrentFavorite(category string) {
	if b.viewActive && (b.viewKind == "history" || b.viewKind == "favorites") {
		b.UI.SetStatus("Cannot add favorite from this view")
		return
	}

	if b.Favs == nil {
		b.Favs = favorites.New()
		_ = b.Favs.Load()
	}

	u := strings.TrimSpace(b.currentURL)
	if u == "" || strings.HasPrefix(strings.ToLower(u), "about:") {
		b.UI.SetStatus("No page to favorite")
		return
	}

	title := u
	if b.UI.Doc != nil && strings.TrimSpace(b.UI.Doc.Title) != "" {
		title = b.UI.Doc.Title
	}

	cat := strings.TrimSpace(category)
	if cat == "" {
		cat = "general"
	}

	if err := b.Favs.Add(u, title, cat); err != nil {
		b.UI.SetStatus("Favorite add error: " + err.Error())
		return
	}

	if b.viewActive && b.viewKind == "favorites" {
		b.UI.SetDocument(b.buildFavoritesDoc())
	}
	b.UI.SetStatus("Added favorite [" + cat + "]")
}

func (b *Browser) removeFavoriteAtCursorOrCurrent() {
	if b.Favs == nil {
		b.Favs = favorites.New()
		_ = b.Favs.Load()
	}

	target := ""
	if b.viewActive && b.viewKind == "favorites" && b.UI.Doc != nil {
		if _, u, ok := b.UI.Doc.LinkAt(b.UI.CursorY, b.UI.CursorX); ok {
			target = strings.TrimSpace(u)
		}
	}
	if target == "" {
		target = strings.TrimSpace(b.currentURL)
	}
	if target == "" {
		b.UI.SetStatus("No favorite to remove")
		return
	}

	removed, err := b.Favs.Remove(target)
	if err != nil {
		b.UI.SetStatus("Favorite remove error: " + err.Error())
		return
	}
	if !removed {
		b.UI.SetStatus("Favorite not found")
		return
	}

	if b.viewActive && b.viewKind == "favorites" {
		b.UI.SetDocument(b.buildFavoritesDoc())
	}
	b.UI.SetStatus("Removed favorite")
}

func (b *Browser) clearHistory() {
	if b.History == nil {
		b.History = history.New()
	}
	if err := b.History.Clear(); err != nil {
		b.UI.SetStatus("History clear error: " + err.Error())
		return
	}
	if b.viewActive && b.viewKind == "history" {
		b.UI.SetDocument(b.buildHistoryDoc())
	}
	b.UI.SetStatus("History cleared")
}

func (b *Browser) clearCache() {
	if b.Fetcher != nil {
		_ = b.Fetcher.Close()
	}

	cacheDir := config.CacheDir()
	if err := os.RemoveAll(cacheDir); err != nil {
		b.UI.SetStatus("Cache clear error: " + err.Error())
		return
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		b.UI.SetStatus("Cache init error: " + err.Error())
		return
	}

	b.History = history.New()
	_ = b.History.Load()
	b.Seen = history.NewSeen()
	_ = b.Seen.Load()
	b.sourceURL = ""
	b.sourceBody = ""
	b.Fetcher = b.newFetcher()

	if b.viewActive && b.viewKind == "history" {
		b.UI.SetDocument(b.buildHistoryDoc())
	}
	b.UI.SetStatus("Cache cleared")
}

func (b *Browser) buildHistoryDoc() *render.Document {
	entries := b.History.Entries()
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})

	lines := make([]render.Line, 0, len(entries)+8)
	links := make([]render.Link, 0, len(entries))
	clearHint := b.historyClearHint()
	cacheHint := b.historyCacheClearHint()

	lines = append(lines, render.Line{Spans: []render.Span{{
		Text:    "History",
		Style:   render.SpanStyle{Bold: true, Color: "heading"},
		LinkIdx: -1,
	}}})

	if len(entries) == 0 {
		lines = append(lines, render.Line{})
		lines = append(lines, render.Line{Spans: []render.Span{{
			Text:    "No history entries.",
			LinkIdx: -1,
		}}})
		lines = append(lines, render.Line{})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: clearHint, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: cacheHint, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
		return &render.Document{Title: "History", URL: aboutHistoryURL, Lines: lines, Links: links}
	}

	lines = append(lines, render.Line{})
	currentDay := ""
	for _, e := range entries {
		day := e.Timestamp.Local().Format("2006-01-02 Monday")
		if day != currentDay {
			if currentDay != "" {
				lines = append(lines, render.Line{})
			}
			currentDay = day
			lines = append(lines, render.Line{Spans: []render.Span{{
				Text:    day,
				Style:   render.SpanStyle{Bold: true},
				LinkIdx: -1,
			}}})
		}

		timePart := e.Timestamp.Local().Format("15:04:05")
		title := strings.TrimSpace(e.Title)
		if title == "" {
			title = e.URL
		}
		lineIdx := len(lines)
		linkIdx := len(links)
		linkCol := len(timePart) + 3
		linkColor := "link"
		if b.isURLSeen(e.URL) {
			linkColor = "visited_link"
		}

		lines = append(lines, render.Line{Spans: []render.Span{
			{Text: timePart + " - ", LinkIdx: -1},
			{Text: title, LinkIdx: linkIdx, Style: render.SpanStyle{Underline: true, Color: linkColor}},
		}})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: "    " + e.URL, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
		links = append(links, render.Link{URL: e.URL, Line: lineIdx, Col: linkCol})
	}

	lines = append(lines, render.Line{})
	lines = append(lines, render.Line{Spans: []render.Span{{Text: clearHint, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
	lines = append(lines, render.Line{Spans: []render.Span{{Text: cacheHint, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})

	return &render.Document{Title: "History", URL: aboutHistoryURL, Lines: lines, Links: links}
}

func (b *Browser) historyClearHint() string {
	keys := []string{"zh"}
	if b != nil && b.UI != nil && b.UI.Keymap != nil {
		if configured := b.UI.Keymap.KeysForAction(keymap.ClearHistory); len(configured) > 0 {
			keys = configured
		}
	}
	return "Clear history: " + strings.Join(keys, " / ")
}

func (b *Browser) historyCacheClearHint() string {
	keys := []string{"zc"}
	if b != nil && b.UI != nil && b.UI.Keymap != nil {
		if configured := b.UI.Keymap.KeysForAction(keymap.ClearCache); len(configured) > 0 {
			keys = configured
		}
	}
	return "Clear cache: " + strings.Join(keys, " / ")
}

func (b *Browser) buildFavoritesDoc() *render.Document {
	if b.Favs == nil {
		b.Favs = favorites.New()
		_ = b.Favs.Load()
	}

	entries := b.Favs.List()
	sort.Slice(entries, func(i, j int) bool {
		ci := strings.ToLower(strings.TrimSpace(entries[i].Category))
		cj := strings.ToLower(strings.TrimSpace(entries[j].Category))
		if ci != cj {
			return ci < cj
		}
		return entries[i].AddedAt.After(entries[j].AddedAt)
	})

	lines := make([]render.Line, 0, len(entries)*2+6)
	links := make([]render.Link, 0, len(entries))
	removeHint := b.favoriteRemoveHint()

	lines = append(lines, render.Line{Spans: []render.Span{{
		Text:    "Bookmarks",
		Style:   render.SpanStyle{Bold: true, Color: "heading"},
		LinkIdx: -1,
	}}})

	if len(entries) == 0 {
		lines = append(lines, render.Line{})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: "No favorites yet. Use za to add one.", LinkIdx: -1}}})
		lines = append(lines, render.Line{})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: removeHint, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
		return &render.Document{Title: "Bookmarks", URL: aboutBookmarksURL, Lines: lines, Links: links}
	}

	lines = append(lines, render.Line{})
	currentCategory := ""
	for _, e := range entries {
		cat := strings.TrimSpace(e.Category)
		if cat == "" {
			cat = "general"
		}
		if cat != currentCategory {
			if currentCategory != "" {
				lines = append(lines, render.Line{})
			}
			currentCategory = cat
			lines = append(lines, render.Line{Spans: []render.Span{{Text: cat, Style: render.SpanStyle{Bold: true}, LinkIdx: -1}}})
		}

		title := strings.TrimSpace(e.Title)
		if title == "" {
			title = e.URL
		}
		lineIdx := len(lines)
		linkIdx := len(links)
		linkColor := "link"
		if b.isURLSeen(e.URL) {
			linkColor = "visited_link"
		}

		lines = append(lines, render.Line{Spans: []render.Span{{Text: "- ", LinkIdx: -1}, {Text: title, LinkIdx: linkIdx, Style: render.SpanStyle{Underline: true, Color: linkColor}}}})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: "  " + e.URL, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
		links = append(links, render.Link{URL: e.URL, Line: lineIdx, Col: 2})
	}

	lines = append(lines, render.Line{})
	lines = append(lines, render.Line{Spans: []render.Span{{Text: removeHint, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})

	return &render.Document{Title: "Bookmarks", URL: aboutBookmarksURL, Lines: lines, Links: links}
}

func (b *Browser) buildSourceDoc() *render.Document {
	width := 80
	if b != nil && b.UI != nil && b.UI.Screen != nil {
		if w, _ := b.UI.Screen.Size(); w > 0 {
			width = w
		}
	}

	bodyDoc := render.RenderPlainText([]byte(b.sourceBody), "about:source-body", width)

	lines := make([]render.Line, 0, 64)
	lines = append(lines, render.Line{Spans: []render.Span{{
		Text:    "Source",
		Style:   render.SpanStyle{Bold: true, Color: "heading"},
		LinkIdx: -1,
	}}})
	if strings.TrimSpace(b.sourceURL) != "" {
		lines = append(lines, render.Line{Spans: []render.Span{{Text: b.sourceURL, Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}})
	}
	lines = append(lines, render.Line{})
	lines = append(lines, bodyDoc.Lines...)

	return &render.Document{Title: "Source", URL: "about:source", Lines: lines}
}

func (b *Browser) favoriteRemoveHint() string {
	keys := []string{"zd"}
	if b != nil && b.UI != nil && b.UI.Keymap != nil {
		if configured := b.UI.Keymap.KeysForAction(keymap.RemoveFavorite); len(configured) > 0 {
			keys = configured
		}
	}
	return "Remove favorite: " + strings.Join(keys, " / ")
}

func isJavaScriptURL(raw string) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(v, "javascript:")
}

func normalizeSourceBody(body []byte) string {
	s := string(body)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func isAboutWelcomeURL(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), aboutWelcomeURL)
}

func isAboutHistoryURL(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), aboutHistoryURL)
}

func isAboutBookmarksURL(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), aboutBookmarksURL)
}

func (b *Browser) activateControl(controlIdx int) {
	if b.UI.Doc == nil || controlIdx < 0 || controlIdx >= len(b.UI.Doc.Controls) {
		return
	}

	c := b.UI.Doc.Controls[controlIdx]
	if c.Disabled {
		b.UI.SetStatus("Control is disabled")
		return
	}

	switch c.Kind {
	case "input":
		switch c.Type {
		case "text", "password", "search", "email", "url", "tel", "number":
			b.UI.BeginControlEdit(controlIdx)
		case "checkbox", "radio":
			b.UI.ToggleControl(controlIdx)
		case "submit":
			b.submitFormControl(controlIdx)
		case "button":
			b.UI.SetStatus("Input button has no default action")
		case "reset":
			b.UI.SetStatus("Reset controls are not implemented")
		default:
			b.UI.SetStatus("Control type not supported")
		}

	case "textarea", "select":
		b.UI.BeginControlEdit(controlIdx)

	case "button":
		switch c.Type {
		case "submit", "":
			b.submitFormControl(controlIdx)
		case "reset":
			b.UI.SetStatus("Reset buttons are not implemented")
		default:
			b.UI.SetStatus("Button has no default action")
		}
	}
}

func (b *Browser) submitFormControl(controlIdx int) {
	if b.UI.Doc == nil {
		return
	}

	sub, err := buildFormSubmission(b.UI.Doc, controlIdx)
	if err != nil {
		b.UI.SetStatus("Form submit error: " + err.Error())
		return
	}

	result, err := b.submitWithStatusAnimation(sub.ActionURL, sub.Method, sub.Values)
	if err != nil {
		b.showError(fmt.Sprintf("Error submitting form: %v", err))
		return
	}

	result, err = b.followMetaRedirectsResult(result)
	if err != nil {
		b.showError(fmt.Sprintf("Error following redirect: %v", err))
		return
	}

	if !shouldDownload(result.ContentType) {
		prev, hasPrev := b.currentNavState()
		b.applyNavigationTransition(result.FinalURL, defaultNavigateOptions(), prev, hasPrev)
	}
	b.applyFetchResult(result, nil)
}

func (b *Browser) submitWithStatusAnimation(actionURL, method string, values url.Values) (*fetch.Result, error) {
	type submitResult struct {
		result *fetch.Result
		err    error
	}

	if b.Fetcher == nil {
		b.Fetcher = b.newFetcher()
	}

	ch := make(chan submitResult, 1)
	go func() {
		result, err := b.Fetcher.SubmitForm(actionURL, method, values)
		ch <- submitResult{result: result, err: err}
	}()

	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	step := 0
	for {
		b.UI.SetStatus(fmt.Sprintf("%s Submitting %s", loadingFrame(step), shortenForStatus(actionURL, 46)))
		b.UI.Draw()

		select {
		case out := <-ch:
			if out.err == nil {
				b.UI.SetStatus("")
			}
			return out.result, out.err
		case <-ticker.C:
			step++
		}
	}
}

func (b *Browser) followMetaRedirectsResult(result *fetch.Result) (*fetch.Result, error) {
	if result == nil || !b.Cfg.FollowMetaRedirects {
		return result, nil
	}

	seen := map[string]bool{result.FinalURL: true}
	for hops := 0; hops < maxMetaRedirectHops; hops++ {
		if !isHTMLContentType(result.ContentType) {
			break
		}

		nextURL, ok := extractMetaRefreshURL(result.Body, result.FinalURL)
		if !ok || nextURL == "" {
			break
		}
		if seen[nextURL] {
			break
		}
		seen[nextURL] = true

		next, err := b.fetchWithStatusAnimation(nextURL, "Loading")
		if err != nil {
			return nil, err
		}
		result = next
	}

	return result, nil
}

func buildFormSubmission(doc *render.Document, submitControlIdx int) (formSubmission, error) {
	if doc == nil {
		return formSubmission{}, fmt.Errorf("no document")
	}
	if submitControlIdx < 0 || submitControlIdx >= len(doc.Controls) {
		return formSubmission{}, fmt.Errorf("invalid submit control")
	}

	submit := doc.Controls[submitControlIdx]
	if submit.FormIdx < 0 || submit.FormIdx >= len(doc.Forms) {
		return formSubmission{}, fmt.Errorf("control is not inside a form")
	}

	form := doc.Forms[submit.FormIdx]
	method := strings.ToUpper(strings.TrimSpace(form.Method))
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "POST" {
		method = "GET"
	}

	actionURL := strings.TrimSpace(form.Action)
	if actionURL == "" {
		return formSubmission{}, fmt.Errorf("unsupported form action")
	}
	if isJavaScriptURL(actionURL) {
		return formSubmission{}, fmt.Errorf("unsupported javascript form action")
	}

	values := url.Values{}
	for _, idx := range form.Controls {
		if idx < 0 || idx >= len(doc.Controls) {
			continue
		}
		c := doc.Controls[idx]
		for _, kv := range successfulControlValues(c, idx, submitControlIdx) {
			values.Add(kv.key, kv.value)
		}
	}

	return formSubmission{Method: method, ActionURL: actionURL, Values: values}, nil
}

type formKV struct {
	key   string
	value string
}

func successfulControlValues(c render.Control, controlIdx, submitControlIdx int) []formKV {
	if c.Disabled || strings.TrimSpace(c.Name) == "" {
		return nil
	}

	name := c.Name
	vals := make([]formKV, 0, 2)

	switch c.Kind {
	case "textarea":
		vals = append(vals, formKV{key: name, value: c.Value})

	case "select":
		for _, opt := range c.Options {
			if opt.Selected && !opt.Disabled {
				vals = append(vals, formKV{key: name, value: opt.Value})
			}
		}

	case "button":
		if strings.EqualFold(c.Type, "submit") && controlIdx == submitControlIdx {
			vals = append(vals, formKV{key: name, value: c.Value})
		}

	case "input":
		switch c.Type {
		case "hidden", "text", "password", "search", "email", "url", "tel", "number":
			vals = append(vals, formKV{key: name, value: c.Value})
		case "checkbox", "radio":
			if c.Checked {
				vals = append(vals, formKV{key: name, value: c.Value})
			}
		case "submit":
			if controlIdx == submitControlIdx {
				vals = append(vals, formKV{key: name, value: c.Value})
			}
		}
	}

	return vals
}

func (b *Browser) showError(msg string) {
	doc := &render.Document{
		Title: "Error",
		URL:   b.currentURL,
		Lines: []render.Line{
			{Spans: []render.Span{{
				Text:    msg,
				Style:   render.SpanStyle{Bold: true, Color: "noscript"},
				LinkIdx: -1,
			}}},
		},
	}
	b.UI.SetDocument(doc)
	b.UI.SetStatus(msg)
}

func (b *Browser) fetchWithStatusAnimation(rawURL, label string) (*fetch.Result, error) {
	type fetchResult struct {
		result *fetch.Result
		err    error
	}
	if strings.TrimSpace(label) == "" {
		label = "Loading"
	}

	if b.Fetcher == nil {
		b.Fetcher = b.newFetcher()
	}

	ch := make(chan fetchResult, 1)
	go func() {
		result, err := b.Fetcher.Fetch(rawURL)
		ch <- fetchResult{result: result, err: err}
	}()

	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()

	step := 0
	for {
		b.UI.SetStatus(fmt.Sprintf("%s %s %s", loadingFrame(step), label, shortenForStatus(rawURL, 46)))
		b.UI.Draw()

		select {
		case out := <-ch:
			if out.err == nil {
				b.UI.SetStatus("")
			}
			return out.result, out.err
		case <-ticker.C:
			step++
		}
	}
}

func loadingFrame(step int) string {
	const width = 8
	pos := step % (2*width - 2)
	if pos >= width {
		pos = 2*width - 2 - pos
	}

	bar := []rune("        ")
	bar[pos] = '▓'
	return "▌" + string(bar) + "▌"
}

func shortenForStatus(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-3]) + "..."
}

func fetchWithMetaRedirects(rawURL string, followMeta bool, fetchOne func(string) (*fetch.Result, error)) (*fetch.Result, error) {
	result, err := fetchOne(rawURL)
	if err != nil {
		return nil, err
	}

	if !followMeta {
		return result, nil
	}

	seen := map[string]bool{result.FinalURL: true}
	for hops := 0; hops < maxMetaRedirectHops; hops++ {
		if !isHTMLContentType(result.ContentType) {
			break
		}

		nextURL, ok := extractMetaRefreshURL(result.Body, result.FinalURL)
		if !ok || nextURL == "" {
			break
		}
		if seen[nextURL] {
			break
		}
		seen[nextURL] = true

		result, err = fetchOne(nextURL)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

func isHTMLContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err == nil {
		ct = mediaType
	}
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
}

func extractMetaRefreshURL(body []byte, baseURL string) (string, bool) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", false
	}

	var out string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil || out != "" {
			return
		}

		if n.Type == html.ElementNode && n.DataAtom == atom.Meta {
			httpEquiv := getAttrInsensitive(n, "http-equiv")
			if strings.EqualFold(strings.TrimSpace(httpEquiv), "refresh") {
				if content := getAttrInsensitive(n, "content"); content != "" {
					if rawTarget, ok := parseMetaRefreshContent(content); ok {
						resolved := resolveAgainst(baseURL, rawTarget)
						if resolved != "" {
							out = resolved
							return
						}
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(doc)
	if out == "" {
		// Some pages place refresh meta tags inside <noscript>, which the HTML
		// parser may treat as text. Fall back to a lightweight raw scan.
		if rawTarget, ok := extractMetaRefreshURLRaw(body); ok {
			resolved := resolveAgainst(baseURL, rawTarget)
			if resolved != "" {
				out = resolved
			}
		}
	}

	if out == "" {
		return "", false
	}
	return out, true
}

func extractMetaRefreshURLRaw(body []byte) (string, bool) {
	for _, tag := range metaTagRe.FindAllString(string(body), -1) {
		httpEquiv := extractAttrValue(tag, httpEquivAttrRe)
		if !strings.EqualFold(strings.TrimSpace(httpEquiv), "refresh") {
			continue
		}
		content := extractAttrValue(tag, contentAttrRe)
		if content == "" {
			continue
		}
		if rawTarget, ok := parseMetaRefreshContent(content); ok {
			return rawTarget, true
		}
	}

	return "", false
}

func extractAttrValue(tag string, re *regexp.Regexp) string {
	m := re.FindStringSubmatch(tag)
	if len(m) == 0 {
		return ""
	}
	for i := 2; i <= 4 && i < len(m); i++ {
		if m[i] != "" {
			return strings.TrimSpace(m[i])
		}
	}
	return ""
}

func parseMetaRefreshContent(content string) (string, bool) {
	parts := strings.Split(content, ";")
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if len(p) < 4 {
			continue
		}
		if strings.EqualFold(p[:4], "url=") {
			u := strings.TrimSpace(p[4:])
			u = strings.Trim(u, "\"'")
			if u != "" {
				return u, true
			}
		}
	}
	return "", false
}

func resolveAgainst(baseURL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}

	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if r.Scheme != "" && r.Scheme != "http" && r.Scheme != "https" {
		return ""
	}

	b, err := url.Parse(baseURL)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func getAttrInsensitive(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func shouldDownload(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(ct)
	if err == nil {
		ct = mediaType
	}

	if strings.HasPrefix(ct, "text/") {
		return false
	}
	if strings.Contains(ct, "html") || strings.Contains(ct, "xhtml") {
		return false
	}
	if strings.Contains(ct, "xml") || strings.Contains(ct, "json") || strings.Contains(ct, "javascript") {
		return false
	}

	return true
}

func (b *Browser) saveDownload(result *fetch.Result) (string, error) {
	dir := b.Cfg.DownloadDir
	if dir == "" {
		dir = config.Default().DownloadDir
	}
	if strings.HasPrefix(dir, "~/") || dir == "~" {
		homeDir, err := os.UserHomeDir()
		if err == nil && homeDir != "" {
			if dir == "~" {
				dir = homeDir
			} else {
				dir = filepath.Join(homeDir, dir[2:])
			}
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating download directory: %w", err)
	}

	filename := downloadFilename(result.FinalURL, result.ContentType)
	path := uniqueDownloadPath(dir, filename)
	if err := os.WriteFile(path, result.Body, 0o644); err != nil {
		return "", fmt.Errorf("writing download: %w", err)
	}

	return path, nil
}

func downloadFilename(rawURL, contentType string) string {
	name := "download"
	if u, err := url.Parse(rawURL); err == nil {
		base := path.Base(u.Path)
		if base != "" && base != "/" && base != "." {
			name = base
		}
	}

	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	if name == "" {
		name = "download"
	}

	if filepath.Ext(name) == "" {
		if ext := defaultExtForContentType(contentType); ext != "" {
			name += ext
		}
	}

	return name
}

func defaultExtForContentType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	mediaType, _, err := mime.ParseMediaType(ct)
	if err == nil {
		ct = mediaType
	}

	switch ct {
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "application/gzip":
		return ".gz"
	case "application/x-tar":
		return ".tar"
	case "application/octet-stream":
		return ".bin"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	}

	return ""
}

func uniqueDownloadPath(dir, filename string) string {
	full := filepath.Join(dir, filename)
	if _, err := os.Stat(full); os.IsNotExist(err) {
		return full
	}

	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func (b *Browser) openMailto(mailtoURL string) {
	handler := b.Cfg.MailtoHandler
	if handler == "" {
		b.UI.SetStatus("No mailto_handler configured")
		return
	}

	// Strip "mailto:" prefix to get the email address.
	email := strings.TrimPrefix(mailtoURL, "mailto:")

	parts := buildCommandArgs(handler, email)
	if len(parts) == 0 {
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		b.UI.SetStatus("mailto error: " + err.Error())
		return
	}

	// Don't wait — mail client runs independently.
	go func() { _ = cmd.Wait() }()
	b.UI.SetStatus("Opened: " + mailtoURL)
}

func (b *Browser) openImage(imgURL string) {
	result, err := b.fetchWithStatusAnimation(imgURL, "Fetching image")
	if err != nil {
		b.UI.SetStatus("Error fetching image: " + err.Error())
		return
	}

	// Write to temp file with appropriate extension.
	tmpFile := "/tmp/wez_image"
	ct := result.ContentType
	switch {
	case strings.Contains(ct, "png"):
		tmpFile += ".png"
	case strings.Contains(ct, "gif"):
		tmpFile += ".gif"
	case strings.Contains(ct, "webp"):
		tmpFile += ".webp"
	case strings.Contains(ct, "svg"):
		tmpFile += ".svg"
	default:
		tmpFile += ".jpg"
	}

	if err := os.WriteFile(tmpFile, result.Body, 0o644); err != nil {
		b.UI.SetStatus("Error saving image: " + err.Error())
		return
	}

	// Build viewer command.
	parts := buildCommandArgs(b.Cfg.ImageViewer, tmpFile)
	if len(parts) == 0 {
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Suspend tcell so the viewer gets raw terminal access.
	b.UI.Suspend()
	defer func() {
		if err := b.UI.Resume(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to restore terminal: %v\n", err)
		}
	}()

	// Ensure viewer output starts at column 0 on a fresh line.
	fmt.Print("\r\n")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Viewer error: %v\n", err)
	}

	// Pause so the user can see the image before tcell takes over again.
	fmt.Print("\r\nPress Enter to return to wez...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadBytes('\n')

	b.UI.SetStatus("")
}

func buildCommandArgs(template, arg string) []string {
	template = strings.TrimSpace(template)
	if template == "" {
		return nil
	}

	if strings.Contains(template, "%s") {
		template = strings.Replace(template, "%s", arg, 1)
	} else {
		template = template + " " + arg
	}

	return strings.Fields(template)
}

// ShowWelcome displays a welcome page when no URL is provided.
func (b *Browser) ShowWelcome() {
	links := make([]render.Link, 0, 5)
	lines := []render.Line{
		{Spans: []render.Span{{Text: "  wez " + config.Version, Style: render.SpanStyle{Bold: true, Color: "heading"}, LinkIdx: -1}}},
		{},
		{Spans: []render.Span{{Text: "Keybindings:", Style: render.SpanStyle{Bold: true}, LinkIdx: -1}}},
		{},
		{Spans: []render.Span{{Text: "  o / O       Open action bar (URL input; O pre-fills)", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  j / k       Scroll down / up", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  h / l       Move cursor left / right", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  gg / G      Go to top / bottom", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Ctrl-F      Page down", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  PgUp        Page up", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  d / u       Half page down / up", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Space       Page down", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Tab / S-Tab Jump to next / previous link/control", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Enter       Activate link/form control under cursor", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "              (edit fields, toggle checks, submit buttons)", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  b / B       Go back", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Ctrl-W      Open welcome page", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Ctrl-H      Open history view (Esc to return)", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Ctrl-B      Open bookmarks view (Esc to return)", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Ctrl-U      Show page source (Esc to return)", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  f / F       Go forward", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  r / R       Reload page", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  za / zd     Add / remove favorite", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  zc / zh     Clear cache / clear history", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  /           Search in page", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Ctrl-o      Search web", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  n / N       Next / previous search match", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  i           Open image under cursor", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  v / V       Enter visual / visual-line mode", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  y / Y       Yank text / link URL under cursor", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  Mouse       Click to move, drag to select", LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "  q           Quit", LinkIdx: -1}}},
		{},
		{Spans: []render.Span{{Text: "Config:  ~/.config/wez/config.toml", Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "Keymap:  ~/.config/wez/keymap.toml", Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "Favs:    ~/.config/wez/fav.json", Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}},
		{Spans: []render.Span{{Text: "History: ~/.cache/wez/history", Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}},
		{},
		{Spans: []render.Span{{Text: "Press 'o' to enter a URL, 'ctrl-o' for web search.", LinkIdx: -1}}},
	}

	if b.Cfg.ShowRecentOnWelcome {
		lines = append(lines,
			render.Line{},
			render.Line{Spans: []render.Span{{Text: strings.Repeat("─", 64), Style: render.SpanStyle{Color: "hrule"}, LinkIdx: -1}}},
			render.Line{Spans: []render.Span{{Text: "Recent:", Style: render.SpanStyle{Bold: true}, LinkIdx: -1}}},
		)

		recent := recentHistoryEntries(b.History.Entries(), 5)
		if len(recent) == 0 {
			lines = append(lines, render.Line{Spans: []render.Span{{Text: "  (no history yet)", LinkIdx: -1}}})
		} else {
			for _, e := range recent {
				title := strings.TrimSpace(e.Title)
				if title == "" {
					title = e.URL
				}

				lineIdx := len(lines)
				linkIdx := len(links)
				linkColor := "link"
				if b.isURLSeen(e.URL) {
					linkColor = "visited_link"
				}

				lines = append(lines, render.Line{Spans: []render.Span{
					{Text: "  - ", LinkIdx: -1},
					{Text: title, LinkIdx: linkIdx, Style: render.SpanStyle{Underline: true, Color: linkColor}},
				}})
				lines = append(lines, render.Line{Spans: []render.Span{{Text: "    " + e.URL, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
				links = append(links, render.Link{URL: e.URL, Line: lineIdx, Col: 4})
			}
		}
	}

	doc := &render.Document{
		Title: "",
		URL:   aboutWelcomeURL,
		Lines: lines,
		Links: links,
	}
	b.UI.SetDocument(doc)
}

func recentHistoryEntries(entries []history.Entry, max int) []history.Entry {
	if max <= 0 || len(entries) == 0 {
		return nil
	}

	out := make([]history.Entry, 0, max)
	for i := len(entries) - 1; i >= 0 && len(out) < max; i-- {
		out = append(out, entries[i])
	}
	return out
}
