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
)

type formSubmission struct {
	Method    string
	ActionURL string
	Values    url.Values
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
	backStack    []string
	forwardStack []string
	currentURL   string

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
	if isAboutWelcomeURL(rawURL) {
		if b.currentURL != "" && b.currentURL != aboutWelcomeURL {
			b.backStack = append(b.backStack, b.currentURL)
		}
		b.forwardStack = nil
		b.currentURL = aboutWelcomeURL
		b.ShowWelcome()
		b.UI.SetStatus("")
		return
	}

	if b.viewActive {
		b.viewActive = false
		b.viewKind = ""
		b.viewPrevDoc = nil
	}
	if isJavaScriptURL(rawURL) {
		b.UI.SetStatus("Ignored javascript URL")
		return
	}

	result, err := fetchWithMetaRedirects(rawURL, b.Cfg.FollowMetaRedirects, func(u string) (*fetch.Result, error) {
		return b.fetchWithStatusAnimation(u, "Loading")
	})
	if err != nil {
		b.showError(fmt.Sprintf("Error loading %s: %v", rawURL, err))
		return
	}
	b.applyFetchResult(result)
}

func (b *Browser) applyFetchResult(result *fetch.Result) {
	if shouldDownload(result.ContentType) {
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

	// Update navigation state.
	if b.currentURL != "" {
		b.backStack = append(b.backStack, b.currentURL)
	}
	b.forwardStack = nil
	b.currentURL = result.FinalURL

	// Record in history.
	_ = b.History.Add(result.FinalURL, doc.Title)

	b.UI.SetDocument(doc)
	b.UI.SetStatus(statusMsg)
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
				b.forwardStack = append(b.forwardStack, b.currentURL)
				prev := b.backStack[len(b.backStack)-1]
				b.backStack = b.backStack[:len(b.backStack)-1]
				b.currentURL = ""
				b.Navigate(prev)
			} else {
				b.UI.SetStatus("No previous page")
			}

		case ui.ActionForward:
			if len(b.forwardStack) > 0 {
				b.backStack = append(b.backStack, b.currentURL)
				next := b.forwardStack[len(b.forwardStack)-1]
				b.forwardStack = b.forwardStack[:len(b.forwardStack)-1]
				b.currentURL = ""
				b.Navigate(next)
			} else {
				b.UI.SetStatus("No next page")
			}

		case ui.ActionReload:
			if b.currentURL != "" {
				url := b.currentURL
				b.currentURL = ""
				b.Navigate(url)
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
	b.UI.SetStatus("Favorites view (Esc to return)")
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
		return &render.Document{Title: "History", URL: "about:history", Lines: lines, Links: links}
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

	return &render.Document{Title: "History", URL: "about:history", Lines: lines, Links: links}
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
		Text:    "Favorites",
		Style:   render.SpanStyle{Bold: true, Color: "heading"},
		LinkIdx: -1,
	}}})

	if len(entries) == 0 {
		lines = append(lines, render.Line{})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: "No favorites yet. Use za to add one.", LinkIdx: -1}}})
		lines = append(lines, render.Line{})
		lines = append(lines, render.Line{Spans: []render.Span{{Text: removeHint, LinkIdx: -1, Style: render.SpanStyle{Color: "code"}}}})
		return &render.Document{Title: "Favorites", URL: "about:favorites", Lines: lines, Links: links}
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

	return &render.Document{Title: "Favorites", URL: "about:favorites", Lines: lines, Links: links}
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

func isAboutWelcomeURL(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), aboutWelcomeURL)
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

	b.applyFetchResult(result)
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
	doc := &render.Document{
		Title: "wez - terminal web browser",
		URL:   aboutWelcomeURL,
		Lines: []render.Line{
			{Spans: []render.Span{{Text: "wez " + config.Version, Style: render.SpanStyle{Bold: true, Color: "heading"}, LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "A terminal web browser", LinkIdx: -1}}},
			{},
			{Spans: []render.Span{{Text: "Keybindings:", Style: render.SpanStyle{Bold: true}, LinkIdx: -1}}},
			{},
			{Spans: []render.Span{{Text: "  o / O       Open URL bar (O pre-fills current URL)", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  j / k       Scroll down / up", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  h / l       Move cursor left / right", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  gg / G      Go to top / bottom", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Ctrl-F      Page down", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  PgUp        Page up", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Ctrl-D/U    Half page down / up", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Space       Page down", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Tab / S-Tab Jump to next / previous link/control", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Enter       Activate link/form control under cursor", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "              (edit fields, toggle checks, submit buttons)", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  b / B       Go back", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Ctrl-H      Open history view (Esc to return)", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Ctrl-B      Open favorites view (Esc to return)", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  L           Go forward", LinkIdx: -1}}},
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
		},
	}
	b.UI.SetDocument(doc)
}
