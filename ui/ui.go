package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"

	"wez/config"
	"wez/keymap"
	"wez/render"
)

// Mode represents the current input mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeURLInput
	ModeSearch
	ModeWebSearch
	ModeVisual
	ModeVisualLine
)

// Action is returned by HandleEvent to tell the browser what to do.
type Action int

const (
	ActionNone Action = iota
	ActionQuit
	ActionNavigate   // navigate to URL in InputBuffer
	ActionFollowLink // follow link under cursor
	ActionBack
	ActionForward
	ActionReload
	ActionOpenImage // open image under cursor
	ActionSearch    // search for InputBuffer
	ActionWebSearch // web search for InputBuffer
	ActionSearchNext
	ActionSearchPrev
	ActionYankURL     // yank selected text or current line
	ActionYankLinkURL // yank href of link under cursor
	ActionOpenMailto  // open mailto link under cursor
)

// UI manages the terminal display and input.
type UI struct {
	Screen tcell.Screen
	Cfg    config.Config
	Keymap *keymap.Keymap

	// Document
	Doc *render.Document

	// Viewport
	ScrollY int
	CursorY int // cursor line in document coordinates
	CursorX int // cursor column in document coordinates

	// Input
	Mode        Mode
	InputBuffer string
	InputCursor int
	InputPrompt string

	// Status
	StatusMsg   string
	StatusLink  string // link URL shown in status bar
	statusAlert bool

	// Pending key for multi-key sequences (e.g. gg)
	pendingSeq string

	// Visual selection
	selAnchorY int
	selAnchorX int

	// Mouse drag state
	mouseSelecting bool
	mouseStartY    int
	mouseStartX    int
	mouseHadDrag   bool

	// Search
	SearchTerm    string
	SearchMatches []searchMatch
	SearchIdx     int
}

type searchMatch struct {
	line int
	col  int
}

// New creates a new UI.
func New(cfg config.Config, km *keymap.Keymap) (*UI, error) {
	screen, err := tcell.NewScreen()
	if err != nil {
		return nil, fmt.Errorf("creating screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return nil, fmt.Errorf("initializing screen: %w", err)
	}
	screen.EnableMouse()
	screen.Clear()

	return &UI{
		Screen: screen,
		Cfg:    cfg,
		Keymap: km,
		Mode:   ModeNormal,
	}, nil
}

// Close shuts down the terminal.
func (u *UI) Close() {
	if u.Screen == nil {
		return
	}
	u.Screen.HideCursor()
	u.Screen.DisableMouse()
	u.Screen.Fini()
}

// Suspend releases terminal control for external interactive commands.
func (u *UI) Suspend() {
	if u.Screen == nil {
		return
	}
	u.Screen.HideCursor()
	u.Screen.DisableMouse()
	if err := u.Screen.Suspend(); err != nil {
		u.Screen.Fini()
	}
}

// Resume reacquires terminal control after Suspend.
func (u *UI) Resume() error {
	if u.Screen != nil {
		if err := u.Screen.Resume(); err == nil {
			u.Screen.EnableMouse()
			return nil
		}
	}

	newScreen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("creating screen: %w", err)
	}
	if err := newScreen.Init(); err != nil {
		return err
	}
	newScreen.EnableMouse()
	u.Screen = newScreen
	return nil
}

// SetDocument sets the current document and resets the viewport.
func (u *UI) SetDocument(doc *render.Document) {
	u.Doc = doc
	u.ScrollY = 0
	u.CursorY = 0
	u.CursorX = 0
	u.StatusLink = ""
	u.SearchMatches = nil
	u.Mode = ModeNormal
	u.mouseSelecting = false
}

// SetStatus sets a temporary status message.
func (u *UI) SetStatus(msg string) {
	u.StatusMsg = msg
	u.statusAlert = false
}

// SetStatusAlert sets a highlighted status message.
func (u *UI) SetStatusAlert(msg string) {
	u.StatusMsg = msg
	u.statusAlert = true
}

// Draw renders the current state to the terminal.
func (u *UI) Draw() {
	u.Screen.Clear()
	w, h := u.Screen.Size()

	// Reserve rows: status bar is the last row, URL bar is the first when active.
	contentStart := 0
	contentEnd := h - 1 // status bar

	if u.Mode == ModeURLInput || u.Mode == ModeSearch || u.Mode == ModeWebSearch {
		contentStart = 1
	}

	contentHeight := contentEnd - contentStart
	u.clampCursor()

	// Draw content.
	if u.Doc != nil {
		for screenRow := contentStart; screenRow < contentEnd; screenRow++ {
			docLine := u.ScrollY + (screenRow - contentStart)
			if docLine < 0 || docLine >= len(u.Doc.Lines) {
				continue
			}
			line := u.Doc.Lines[docLine]
			x := 0
			for _, span := range line.Spans {
				style := u.spanStyle(span)
				for _, r := range span.Text {
					if x >= w {
						break
					}
					rw := runewidth.RuneWidth(r)
					if rw < 1 {
						rw = 1
					}
					drawStyle := style
					if u.isSelectedCell(docLine, x) {
						drawStyle = drawStyle.Reverse(true)
					}
					if docLine == u.CursorY && x == u.CursorX {
						drawStyle = drawStyle.Reverse(true).Bold(true)
					}
					u.Screen.SetContent(x, screenRow, r, nil, drawStyle)
					for i := 1; i < rw; i++ {
						if x+i < w {
							u.Screen.SetContent(x+i, screenRow, ' ', nil, drawStyle)
						}
					}
					x += rw
				}
			}

			// Always draw a visible cursor cell, even past end-of-line.
			if docLine == u.CursorY && u.CursorX >= 0 && u.CursorX < w {
				mainc, combc, style, _ := u.Screen.GetContent(u.CursorX, screenRow)
				if mainc == 0 {
					mainc = ' '
					combc = nil
					style = tcell.StyleDefault
				}
				style = style.Reverse(true).Bold(true)
				u.Screen.SetContent(u.CursorX, screenRow, mainc, combc, style)
			}
		}
	}

	// Draw URL bar / search bar if active.
	if u.Mode == ModeURLInput || u.Mode == ModeSearch || u.Mode == ModeWebSearch {
		barStyle := tcell.StyleDefault.
			Background(config.ParseColor(u.Cfg.Colors.TopBarBg)).
			Foreground(config.ParseColor(u.Cfg.Colors.TopBar))
		for x := 0; x < w; x++ {
			u.Screen.SetContent(x, 0, ' ', nil, barStyle)
		}
		prompt := u.InputPrompt
		drawString(u.Screen, 0, 0, prompt, barStyle)
		drawString(u.Screen, runewidth.StringWidth(prompt), 0, u.InputBuffer, barStyle)
		cursorPos := runewidth.StringWidth(prompt) + u.InputCursor
		if cursorPos < w {
			u.Screen.ShowCursor(cursorPos, 0)
		}
	} else {
		u.Screen.HideCursor()
	}

	// Draw status bar.
	statusStyle := tcell.StyleDefault.
		Background(config.ParseColor(u.Cfg.Colors.StatusBarBg)).
		Foreground(config.ParseColor(u.Cfg.Colors.StatusBar))
	if u.statusAlert {
		statusStyle = tcell.StyleDefault.Background(tcell.ColorYellow).Foreground(tcell.ColorBlack)
	}
	for x := 0; x < w; x++ {
		u.Screen.SetContent(x, h-1, ' ', nil, statusStyle)
	}

	// Left: status message or hovered link URL.
	leftStatus := ""
	if u.StatusMsg != "" {
		leftStatus = u.StatusMsg
	} else if u.Mode == ModeVisual {
		leftStatus = "-- VISUAL --"
	} else if u.Mode == ModeVisualLine {
		leftStatus = "-- VISUAL LINE --"
	} else if u.StatusLink != "" {
		leftStatus = u.StatusLink
	}

	// Right: version + position info.
	rightStatus := "wez " + config.Version
	if u.Doc != nil && len(u.Doc.Lines) > 0 {
		total := len(u.Doc.Lines)
		pct := 0
		if contentHeight > 0 && total > contentHeight {
			pct = (u.ScrollY * 100) / (total - contentHeight)
		} else if total <= contentHeight {
			pct = 100
		}
		rightStatus = fmt.Sprintf("%d/%d %d%% | wez %s", u.CursorY+1, total, pct, config.Version)
	}

	drawString(u.Screen, 0, h-1, truncate(leftStatus, w/2), statusStyle)
	drawString(u.Screen, w-runewidth.StringWidth(rightStatus), h-1, rightStatus, statusStyle)

	// Center: title + URL.
	if u.Doc != nil {
		titleURL := ""
		if u.Doc.Title != "" {
			titleURL = u.Doc.Title + " - " + u.Doc.URL
		} else {
			titleURL = u.Doc.URL
		}
		leftLen := runewidth.StringWidth(truncate(leftStatus, w/2))
		rightLen := runewidth.StringWidth(rightStatus)
		maxCenter := w - leftLen - rightLen - 4
		if maxCenter > 10 {
			titleURL = truncate(titleURL, maxCenter)
			centerX := leftLen + 2
			drawString(u.Screen, centerX, h-1, titleURL, statusStyle)
		}
	}

	u.Screen.Show()
	_ = contentHeight
}

// HandleEvent processes a tcell event and returns an action.
func (u *UI) HandleEvent(ev tcell.Event) Action {
	switch ev := ev.(type) {
	case *tcell.EventResize:
		u.Screen.Sync()
		return ActionNone

	case *tcell.EventKey:
		switch u.Mode {
		case ModeURLInput:
			return u.handleInputKey(ev, ActionNavigate)
		case ModeSearch:
			return u.handleInputKey(ev, ActionSearch)
		case ModeWebSearch:
			return u.handleInputKey(ev, ActionWebSearch)
		default:
			return u.handleNormalKey(ev)
		}

	case *tcell.EventMouse:
		if u.Mode == ModeURLInput || u.Mode == ModeSearch || u.Mode == ModeWebSearch {
			return ActionNone
		}
		u.handleMouseEvent(ev)
		return ActionNone
	}
	return ActionNone
}

func (u *UI) handleNormalKey(ev *tcell.EventKey) Action {
	// Hardcoded keys that always work, regardless of keymap.
	switch ev.Key() {
	case tcell.KeyCtrlC:
		return ActionQuit
	case tcell.KeyEscape:
		u.StatusMsg = ""
		u.pendingSeq = ""
		if u.isVisualMode() {
			u.Mode = ModeNormal
		}
		return ActionNone
	}

	keyStr := keymap.EventToKeyString(ev)
	if keyStr == "" {
		return ActionNone
	}

	actionName, newPending := u.Keymap.Resolve(u.pendingSeq, keyStr)
	u.pendingSeq = newPending

	if actionName == "" {
		return ActionNone
	}

	return u.executeAction(actionName)
}

func (u *UI) executeAction(actionName string) Action {
	_, h := u.Screen.Size()
	contentHeight := h - 1
	if u.Mode != ModeNormal {
		contentHeight--
	}

	maxScroll := 0
	maxLine := 0
	if u.Doc != nil {
		maxScroll = len(u.Doc.Lines) - contentHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
		maxLine = len(u.Doc.Lines) - 1
		if maxLine < 0 {
			maxLine = 0
		}
	}

	switch actionName {
	case keymap.Quit:
		return ActionQuit

	case keymap.ScrollDown:
		u.moveCursor(1, 0, maxLine)

	case keymap.ScrollUp:
		u.moveCursor(-1, 0, maxLine)

	case keymap.CursorLeft:
		if u.CursorX > 0 {
			u.CursorX--
			u.updateStatusLink()
		}

	case keymap.CursorRight:
		u.CursorX++
		u.updateStatusLink()

	case keymap.PageDown:
		u.scroll(contentHeight)

	case keymap.PageUp:
		u.scroll(-contentHeight)

	case keymap.HalfPageDown:
		u.scroll(contentHeight / 2)

	case keymap.HalfPageUp:
		u.scroll(-contentHeight / 2)

	case keymap.GoTop:
		u.ScrollY = 0
		u.CursorY = 0
		u.CursorX = 0
		u.updateStatusLink()

	case keymap.GoBottom:
		u.CursorY = maxLine
		u.ScrollY = maxScroll
		u.updateStatusLink()

	case keymap.Back:
		return ActionBack

	case keymap.Forward:
		return ActionForward

	case keymap.OpenURL:
		u.Mode = ModeURLInput
		u.InputPrompt = "URL: "
		u.InputBuffer = ""
		u.InputCursor = 0

	case keymap.OpenURLEdit:
		u.Mode = ModeURLInput
		u.InputPrompt = "URL: "
		if u.Doc != nil {
			u.InputBuffer = u.Doc.URL
			u.InputCursor = len(u.InputBuffer)
		} else {
			u.InputBuffer = ""
			u.InputCursor = 0
		}

	case keymap.FollowLink:
		return ActionFollowLink

	case keymap.NextLink:
		u.jumpToNextLink()

	case keymap.PrevLink:
		u.jumpToPrevLink()

	case keymap.Search:
		u.Mode = ModeSearch
		u.InputPrompt = "/"
		u.InputBuffer = ""
		u.InputCursor = 0

	case keymap.SearchWeb:
		u.Mode = ModeWebSearch
		u.InputPrompt = "search web: "
		u.InputBuffer = ""
		u.InputCursor = 0

	case keymap.VisualMode:
		u.startVisual(false)

	case keymap.VisualLine:
		u.startVisual(true)

	case keymap.SearchNext:
		return ActionSearchNext

	case keymap.SearchPrev:
		return ActionSearchPrev

	case keymap.Reload:
		return ActionReload

	case keymap.OpenImage:
		return ActionOpenImage

	case keymap.YankURL:
		return ActionYankURL

	case keymap.YankLinkURL:
		return ActionYankLinkURL
	}

	u.clampCursor()
	if !u.isVisualMode() {
		u.selAnchorY = u.CursorY
		u.selAnchorX = u.CursorX
	}
	return ActionNone
}

func (u *UI) handleInputKey(ev *tcell.EventKey, submitAction Action) Action {
	switch ev.Key() {
	case tcell.KeyEscape:
		u.Mode = ModeNormal
		u.InputBuffer = ""
		u.InputCursor = 0
		return ActionNone

	case tcell.KeyEnter:
		u.Mode = ModeNormal
		return submitAction

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if u.InputCursor > 0 {
			u.InputBuffer = u.InputBuffer[:u.InputCursor-1] + u.InputBuffer[u.InputCursor:]
			u.InputCursor--
		}
		return ActionNone

	case tcell.KeyDelete:
		if u.InputCursor < len(u.InputBuffer) {
			u.InputBuffer = u.InputBuffer[:u.InputCursor] + u.InputBuffer[u.InputCursor+1:]
		}
		return ActionNone

	case tcell.KeyLeft:
		if u.InputCursor > 0 {
			u.InputCursor--
		}
		return ActionNone

	case tcell.KeyRight:
		if u.InputCursor < len(u.InputBuffer) {
			u.InputCursor++
		}
		return ActionNone

	case tcell.KeyHome, tcell.KeyCtrlA:
		u.InputCursor = 0
		return ActionNone

	case tcell.KeyEnd, tcell.KeyCtrlE:
		u.InputCursor = len(u.InputBuffer)
		return ActionNone

	case tcell.KeyCtrlU:
		u.InputBuffer = u.InputBuffer[u.InputCursor:]
		u.InputCursor = 0
		return ActionNone

	case tcell.KeyCtrlW:
		if u.InputCursor > 0 {
			i := u.InputCursor - 1
			for i > 0 && u.InputBuffer[i] == ' ' {
				i--
			}
			for i > 0 && u.InputBuffer[i] != ' ' {
				i--
			}
			u.InputBuffer = u.InputBuffer[:i] + u.InputBuffer[u.InputCursor:]
			u.InputCursor = i
		}
		return ActionNone

	case tcell.KeyRune:
		ch := string(ev.Rune())
		u.InputBuffer = u.InputBuffer[:u.InputCursor] + ch + u.InputBuffer[u.InputCursor:]
		u.InputCursor++
		return ActionNone
	}

	return ActionNone
}

func (u *UI) scroll(delta int) {
	_, h := u.Screen.Size()
	contentHeight := h - 1

	maxScroll := 0
	if u.Doc != nil {
		maxScroll = len(u.Doc.Lines) - contentHeight
		if maxScroll < 0 {
			maxScroll = 0
		}
	}

	u.ScrollY += delta
	if u.ScrollY < 0 {
		u.ScrollY = 0
	}
	if u.ScrollY > maxScroll {
		u.ScrollY = maxScroll
	}

	if u.CursorY < u.ScrollY {
		u.CursorY = u.ScrollY
	}
	if u.CursorY >= u.ScrollY+contentHeight {
		u.CursorY = u.ScrollY + contentHeight - 1
	}
	u.updateStatusLink()
}

func (u *UI) moveCursor(deltaY int, minLine, maxLine int) {
	_, h := u.Screen.Size()
	contentHeight := h - 1

	u.CursorY += deltaY
	if u.CursorY < minLine {
		u.CursorY = minLine
	}
	if u.CursorY > maxLine {
		u.CursorY = maxLine
	}

	if u.CursorY < u.ScrollY {
		u.ScrollY = u.CursorY
	}
	if u.CursorY >= u.ScrollY+contentHeight {
		u.ScrollY = u.CursorY - contentHeight + 1
	}
	u.updateStatusLink()
}

func (u *UI) jumpToNextLink() {
	if u.Doc == nil || len(u.Doc.Links) == 0 {
		return
	}
	_, line, col, ok := u.Doc.NextLink(u.CursorY, u.CursorX)
	if ok {
		u.CursorY = line
		u.CursorX = col
		u.ensureCursorVisible()
		u.updateStatusLink()
	}
}

func (u *UI) jumpToPrevLink() {
	if u.Doc == nil || len(u.Doc.Links) == 0 {
		return
	}
	_, line, col, ok := u.Doc.PrevLink(u.CursorY, u.CursorX)
	if ok {
		u.CursorY = line
		u.CursorX = col
		u.ensureCursorVisible()
		u.updateStatusLink()
	}
}

func (u *UI) ensureCursorVisible() {
	_, h := u.Screen.Size()
	contentHeight := h - 1
	if u.CursorY < u.ScrollY {
		u.ScrollY = u.CursorY
	}
	if u.CursorY >= u.ScrollY+contentHeight {
		u.ScrollY = u.CursorY - contentHeight + 1
	}
}

func (u *UI) updateStatusLink() {
	u.StatusLink = ""
	if u.Doc == nil {
		return
	}
	_, url, ok := u.Doc.LinkAt(u.CursorY, u.CursorX)
	if ok {
		u.StatusLink = url
	}
}

func (u *UI) isVisualMode() bool {
	return u.Mode == ModeVisual || u.Mode == ModeVisualLine
}

func (u *UI) startVisual(lineWise bool) {
	if lineWise {
		if u.Mode == ModeVisualLine {
			u.Mode = ModeNormal
			return
		}
		u.Mode = ModeVisualLine
	} else {
		if u.Mode == ModeVisual {
			u.Mode = ModeNormal
			return
		}
		u.Mode = ModeVisual
	}
	u.selAnchorY = u.CursorY
	u.selAnchorX = u.CursorX
}

func (u *UI) clampCursor() {
	w, _ := u.Screen.Size()
	if u.CursorX < 0 {
		u.CursorX = 0
	}
	if w > 0 && u.CursorX >= w {
		u.CursorX = w - 1
	}

	if u.CursorY < 0 {
		u.CursorY = 0
	}
	if u.Doc != nil && len(u.Doc.Lines) > 0 && u.CursorY >= len(u.Doc.Lines) {
		u.CursorY = len(u.Doc.Lines) - 1
	}
}

func (u *UI) isSelectedCell(line, col int) bool {
	if !u.isVisualMode() {
		return false
	}

	if u.Mode == ModeVisualLine {
		startLine := minInt(u.selAnchorY, u.CursorY)
		endLine := maxInt(u.selAnchorY, u.CursorY)
		return line >= startLine && line <= endLine
	}

	startLine, startCol, endLine, endCol := u.selectionBounds()
	if line < startLine || line > endLine {
		return false
	}
	if startLine == endLine {
		return col >= startCol && col <= endCol
	}
	if line == startLine {
		return col >= startCol
	}
	if line == endLine {
		return col <= endCol
	}
	return true
}

func (u *UI) selectionBounds() (startLine, startCol, endLine, endCol int) {
	if u.selAnchorY < u.CursorY || (u.selAnchorY == u.CursorY && u.selAnchorX <= u.CursorX) {
		return u.selAnchorY, u.selAnchorX, u.CursorY, u.CursorX
	}
	return u.CursorY, u.CursorX, u.selAnchorY, u.selAnchorX
}

func (u *UI) handleMouseEvent(ev *tcell.EventMouse) {
	x, y := ev.Position()
	_, h := u.Screen.Size()
	buttons := ev.Buttons()

	if buttons&tcell.WheelUp != 0 {
		u.scroll(-3)
		return
	}
	if buttons&tcell.WheelDown != 0 {
		u.scroll(3)
		return
	}

	if y < 0 || y >= h-1 {
		if ev.Buttons() == tcell.ButtonNone {
			u.mouseSelecting = false
		}
		return
	}

	docLine := u.ScrollY + y
	if u.Doc != nil {
		if docLine < 0 {
			docLine = 0
		}
		if docLine >= len(u.Doc.Lines) {
			docLine = len(u.Doc.Lines) - 1
		}
	}

	if buttons&tcell.Button1 != 0 {
		if !u.mouseSelecting {
			u.mouseSelecting = true
			u.mouseStartY = docLine
			u.mouseStartX = x
			u.mouseHadDrag = false

			u.CursorY = docLine
			u.CursorX = x
			u.clampCursor()
			u.updateStatusLink()
		} else {
			u.CursorY = docLine
			u.CursorX = x
			u.clampCursor()
			u.updateStatusLink()
		}

		if u.CursorY != u.mouseStartY || u.CursorX != u.mouseStartX {
			u.mouseHadDrag = true
			if !u.isVisualMode() {
				u.Mode = ModeVisual
				u.selAnchorY = u.mouseStartY
				u.selAnchorX = u.mouseStartX
			}
		}
		return
	}

	if buttons == tcell.ButtonNone && u.mouseSelecting {
		if !u.mouseHadDrag && u.isVisualMode() {
			u.Mode = ModeNormal
		}
		u.mouseSelecting = false
	}
}

// Yank copies the current selection (visual modes) or current line (normal mode)
// to the system clipboard when available.
func (u *UI) Yank() {
	if u.Doc == nil || len(u.Doc.Lines) == 0 {
		u.SetStatus("Nothing to yank")
		return
	}

	var text string
	if u.isVisualMode() {
		text = u.selectedText()
		u.Mode = ModeNormal
	} else {
		if u.CursorY < 0 || u.CursorY >= len(u.Doc.Lines) {
			u.SetStatus("Nothing to yank")
			return
		}
		text = lineText(u.Doc.Lines[u.CursorY])
	}

	if text == "" {
		u.SetStatus("Nothing to yank")
		return
	}

	if err := copyToClipboard(text); err != nil {
		u.SetStatus(fmt.Sprintf("Yanked %d chars (no clipboard helper)", len([]rune(text))))
		return
	}

	u.SetStatus(fmt.Sprintf("Yanked %d chars", len([]rune(text))))
}

// YankLinkURL copies the href under the cursor to clipboard.
func (u *UI) YankLinkURL() {
	if u.Doc == nil {
		u.SetStatus("No link under cursor")
		return
	}

	_, linkURL, ok := u.Doc.LinkAt(u.CursorY, u.CursorX)
	if !ok || strings.TrimSpace(linkURL) == "" {
		u.SetStatus("No link under cursor")
		return
	}

	if err := copyToClipboard(linkURL); err != nil {
		u.SetStatus("Link: " + shortenStatusText(linkURL, 64) + " (no clipboard helper)")
		return
	}

	u.SetStatus("Yanked link: " + shortenStatusText(linkURL, 64))
}

func shortenStatusText(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes <= 3 {
		return string(r[:maxRunes])
	}
	return string(r[:maxRunes-3]) + "..."
}

func (u *UI) selectedText() string {
	if u.Doc == nil || len(u.Doc.Lines) == 0 {
		return ""
	}

	if u.Mode == ModeVisualLine {
		startLine := minInt(u.selAnchorY, u.CursorY)
		endLine := maxInt(u.selAnchorY, u.CursorY)
		var parts []string
		for line := startLine; line <= endLine && line < len(u.Doc.Lines); line++ {
			parts = append(parts, lineText(u.Doc.Lines[line]))
		}
		return strings.Join(parts, "\n")
	}

	startLine, startCol, endLine, endCol := u.selectionBounds()
	var parts []string
	for line := startLine; line <= endLine && line < len(u.Doc.Lines); line++ {
		s := 0
		e := 1 << 30
		if line == startLine {
			s = startCol
		}
		if line == endLine {
			e = endCol
		}
		parts = append(parts, sliceLineByCol(u.Doc.Lines[line], s, e))
	}
	return strings.Join(parts, "\n")
}

func sliceLineByCol(line render.Line, startCol, endCol int) string {
	if endCol < startCol {
		return ""
	}

	x := 0
	var sb strings.Builder
	for _, span := range line.Spans {
		for _, r := range span.Text {
			rw := runewidth.RuneWidth(r)
			if rw < 1 {
				rw = 1
			}
			rStart := x
			rEnd := x + rw - 1

			if rEnd < startCol {
				x += rw
				continue
			}
			if rStart > endCol {
				return sb.String()
			}

			sb.WriteRune(r)
			x += rw
		}
	}

	return sb.String()
}

func copyToClipboard(text string) error {
	var commands [][]string
	switch runtime.GOOS {
	case "darwin":
		commands = [][]string{{"pbcopy"}}
	default:
		commands = [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	}

	for _, args := range commands {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	return fmt.Errorf("clipboard helper not found")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// PerformSearch finds all occurrences of the search term in the document.
func (u *UI) PerformSearch() {
	u.SearchMatches = nil
	u.SearchIdx = 0
	if u.Doc == nil || u.SearchTerm == "" {
		return
	}
	term := strings.ToLower(u.SearchTerm)
	for i, line := range u.Doc.Lines {
		text := lineText(line)
		lower := strings.ToLower(text)
		offset := 0
		for {
			idx := strings.Index(lower[offset:], term)
			if idx == -1 {
				break
			}
			u.SearchMatches = append(u.SearchMatches, searchMatch{
				line: i,
				col:  offset + idx,
			})
			offset += idx + len(term)
		}
	}
	if len(u.SearchMatches) > 0 {
		u.jumpToSearchMatch(0)
		u.SetStatus(fmt.Sprintf("/%s [%d/%d]", u.SearchTerm, 1, len(u.SearchMatches)))
	} else {
		u.SetStatus(fmt.Sprintf("/%s [not found]", u.SearchTerm))
	}
}

// NextSearchMatch jumps to the next search match.
func (u *UI) NextSearchMatch() {
	if len(u.SearchMatches) == 0 {
		return
	}
	u.SearchIdx = (u.SearchIdx + 1) % len(u.SearchMatches)
	u.jumpToSearchMatch(u.SearchIdx)
	u.SetStatus(fmt.Sprintf("/%s [%d/%d]", u.SearchTerm, u.SearchIdx+1, len(u.SearchMatches)))
}

// PrevSearchMatch jumps to the previous search match.
func (u *UI) PrevSearchMatch() {
	if len(u.SearchMatches) == 0 {
		return
	}
	u.SearchIdx--
	if u.SearchIdx < 0 {
		u.SearchIdx = len(u.SearchMatches) - 1
	}
	u.jumpToSearchMatch(u.SearchIdx)
	u.SetStatus(fmt.Sprintf("/%s [%d/%d]", u.SearchTerm, u.SearchIdx+1, len(u.SearchMatches)))
}

func (u *UI) jumpToSearchMatch(idx int) {
	if idx < 0 || idx >= len(u.SearchMatches) {
		return
	}
	m := u.SearchMatches[idx]
	u.CursorY = m.line
	u.CursorX = m.col
	u.ensureCursorVisible()
	u.updateStatusLink()
}

// spanStyle converts a render.Span into a tcell.Style using config colors.
func (u *UI) spanStyle(span render.Span) tcell.Style {
	style := tcell.StyleDefault

	switch span.Style.Color {
	case "link":
		style = style.Foreground(config.ParseColor(u.Cfg.Colors.Link))
	case "heading":
		style = style.Foreground(config.ParseColor(u.Cfg.Colors.Heading))
	case "code":
		style = style.Foreground(config.ParseColor(u.Cfg.Colors.Code))
	case "noscript":
		style = style.Foreground(config.ParseColor(u.Cfg.Colors.Noscript))
	case "blockquote":
		style = style.Foreground(config.ParseColor(u.Cfg.Colors.BlockQuote))
	case "image":
		style = style.Foreground(config.ParseColor(u.Cfg.Colors.Image))
	case "hrule":
		style = style.Foreground(config.ParseColor(u.Cfg.Colors.HRule))
	}

	if span.Style.BgColor == "noscript_bg" {
		style = style.Background(config.ParseColor(u.Cfg.Colors.NoscriptBg))
	}

	if span.Style.Bold {
		style = style.Bold(true)
	}
	if span.Style.Italic {
		style = style.Italic(true)
	}
	if span.Style.Underline {
		style = style.Underline(true)
	}
	if span.Style.Strike {
		style = style.StrikeThrough(true)
	}

	return style
}

// --- Helpers ---

func drawString(screen tcell.Screen, x, y int, s string, style tcell.Style) {
	for _, r := range s {
		if x >= 0 {
			screen.SetContent(x, y, r, nil, style)
		}
		x += runewidth.RuneWidth(r)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func lineText(line render.Line) string {
	var sb strings.Builder
	for _, span := range line.Spans {
		sb.WriteString(span.Text)
	}
	return sb.String()
}
