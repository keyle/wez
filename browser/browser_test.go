package browser

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"wez/config"
	"wez/render"
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
