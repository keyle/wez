package ui

import (
	"testing"

	"wez/render"
)

func TestJumpToLineFirstNonSpace(t *testing.T) {
	u := &UI{
		Doc:     &render.Document{Lines: []render.Line{{Spans: []render.Span{{Text: "   abc"}}}}},
		CursorY: 0,
		CursorX: 5,
	}

	u.jumpToLineFirstNonSpace()

	if u.CursorX != 3 {
		t.Fatalf("expected cursor at first non-space column 3, got %d", u.CursorX)
	}
}

func TestJumpToLineFirstNonSpaceBlankLine(t *testing.T) {
	u := &UI{
		Doc:     &render.Document{Lines: []render.Line{{Spans: []render.Span{{Text: "    "}}}}},
		CursorY: 0,
		CursorX: 2,
	}

	u.jumpToLineFirstNonSpace()

	if u.CursorX != 0 {
		t.Fatalf("expected cursor to fall back to column 0, got %d", u.CursorX)
	}
}

func TestJumpToLineEnd(t *testing.T) {
	u := &UI{
		Doc:     &render.Document{Lines: []render.Line{{Spans: []render.Span{{Text: "abc  "}}}}},
		CursorY: 0,
		CursorX: 0,
	}

	u.jumpToLineEnd()

	if u.CursorX != 4 {
		t.Fatalf("expected cursor at last char column 4, got %d", u.CursorX)
	}
}
