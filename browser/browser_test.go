package browser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wez/config"
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
