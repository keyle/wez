package ui

import (
	"fmt"
	"testing"

	"github.com/gdamore/tcell/v2"

	"wez/config"
	"wez/keymap"
	"wez/render"
)

func benchmarkUI(b *testing.B, lineCount, width int) *UI {
	b.Helper()

	sim := tcell.NewSimulationScreen("UTF-8")
	if err := sim.Init(); err != nil {
		b.Fatalf("failed to init simulation screen: %v", err)
	}
	b.Cleanup(sim.Fini)
	sim.SetSize(width, 32)

	lines := make([]render.Line, 0, lineCount)
	for i := 0; i < lineCount; i++ {
		text := fmt.Sprintf("line %04d - the quick brown fox jumps over the lazy dog", i)
		lines = append(lines, render.Line{Spans: []render.Span{{Text: text, LinkIdx: -1, ControlIdx: -1}}})
	}

	u := &UI{Screen: sim, Cfg: config.Default(), Mode: ModeNormal}
	u.SetDocument(&render.Document{Title: "Bench", URL: "https://example.com", Lines: lines})
	u.Draw()
	return u
}

func BenchmarkScrollDownDraw(b *testing.B) {
	u := benchmarkUI(b, 3000, 120)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		u.executeAction(keymap.ScrollDown)
		u.Draw()
	}
}

func BenchmarkPageDownUpDraw(b *testing.B) {
	u := benchmarkUI(b, 3000, 120)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if i&1 == 0 {
			u.executeAction(keymap.PageDown)
		} else {
			u.executeAction(keymap.PageUp)
		}
		u.Draw()
	}
}

func BenchmarkScrollDownNoDraw(b *testing.B) {
	u := benchmarkUI(b, 3000, 120)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		u.executeAction(keymap.ScrollDown)
	}
}
