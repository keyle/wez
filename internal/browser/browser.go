package browser

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"wez/internal/config"
	"wez/internal/fetch"
	"wez/internal/history"
	"wez/internal/keymap"
	"wez/internal/render"
	"wez/internal/ui"
)

// Browser ties all components together.
type Browser struct {
	UI      *ui.UI
	Cfg     config.Config
	History *history.History

	// Navigation stacks.
	backStack    []string
	forwardStack []string
	currentURL   string
}

// New creates a new Browser instance.
func New(cfg config.Config, km *keymap.Keymap) (*Browser, error) {
	tui, err := ui.New(cfg, km)
	if err != nil {
		return nil, err
	}

	hist := history.New()
	_ = hist.Load()

	return &Browser{
		UI:      tui,
		Cfg:     cfg,
		History: hist,
	}, nil
}

// Navigate fetches and renders a URL.
func (b *Browser) Navigate(rawURL string) {
	b.UI.SetStatus("Loading " + rawURL + "...")
	b.UI.Draw()

	result, err := fetch.Fetch(rawURL)
	if err != nil {
		b.showError(fmt.Sprintf("Error loading %s: %v", rawURL, err))
		return
	}

	statusMsg := ""
	if result.StatusCode >= 400 {
		statusMsg = fmt.Sprintf("HTTP %d", result.StatusCode)
	}

	w, _ := b.UI.Screen.Size()

	var doc *render.Document
	ct := strings.ToLower(result.ContentType)
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") {
		doc = render.Render(result.Body, result.FinalURL, w)
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

// Run starts the main event loop. If initialURL is non-empty, navigates there first.
func (b *Browser) Run(initialURL string) {
	defer b.UI.Close()

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
				}
			}

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

		case ui.ActionSearchNext:
			b.UI.NextSearchMatch()

		case ui.ActionSearchPrev:
			b.UI.PrevSearchMatch()

		case ui.ActionYankURL:
			if b.currentURL != "" {
				b.UI.SetStatus("URL: " + b.currentURL)
			}
		}
	}
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

func (b *Browser) openMailto(mailtoURL string) {
	handler := b.Cfg.MailtoHandler
	if handler == "" {
		b.UI.SetStatus("No mailto_handler configured")
		return
	}

	// Strip "mailto:" prefix to get the email address.
	email := strings.TrimPrefix(mailtoURL, "mailto:")

	var cmdStr string
	if strings.Contains(handler, "%s") {
		cmdStr = strings.Replace(handler, "%s", email, 1)
	} else {
		cmdStr = handler + " " + email
	}

	parts := strings.Fields(cmdStr)
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
	b.UI.SetStatus("Fetching image...")
	b.UI.Draw()

	result, err := fetch.Fetch(imgURL)
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
	viewer := b.Cfg.ImageViewer
	var cmdStr string
	if strings.Contains(viewer, "%s") {
		cmdStr = strings.Replace(viewer, "%s", tmpFile, 1)
	} else {
		cmdStr = viewer + " " + tmpFile
	}

	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Suspend tcell so the viewer gets raw terminal access.
	b.UI.Screen.Fini()

	fmt.Printf("Opening image: %s\n", imgURL)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Viewer error: %v\n", err)
	}

	// Pause so the user can see the image before tcell takes over again.
	fmt.Print("\nPress Enter to return to wez...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadBytes('\n')

	// Re-initialize tcell.
	if err := b.UI.Screen.Init(); err != nil {
		panic("failed to re-init screen: " + err.Error())
	}
	b.UI.Screen.EnableMouse()
	b.UI.SetStatus("")
}

// ShowWelcome displays a welcome page when no URL is provided.
func (b *Browser) ShowWelcome() {
	doc := &render.Document{
		Title: "wez - terminal web browser",
		URL:   "about:welcome",
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
			{Spans: []render.Span{{Text: "  Ctrl-B      Page up", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Ctrl-D/U    Half page down / up", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Space       Page down", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Tab / S-Tab Jump to next / previous link", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  Enter       Follow link under cursor", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  b / B / H   Go back", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  L           Go forward", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  r           Reload page", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  /           Search in page", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  n / N       Next / previous search match", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  i           Open image under cursor", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  y           Show current URL", LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "  q           Quit", LinkIdx: -1}}},
			{},
			{Spans: []render.Span{{Text: "Config:  ~/.config/wez/config.toml", Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "Keymap:  ~/.config/wez/keymap.toml", Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}},
			{Spans: []render.Span{{Text: "History: ~/.cache/wez/history", Style: render.SpanStyle{Color: "code"}, LinkIdx: -1}}},
			{},
			{Spans: []render.Span{{Text: "Press 'o' to enter a URL and start browsing.", LinkIdx: -1}}},
		},
	}
	b.UI.SetDocument(doc)
}
