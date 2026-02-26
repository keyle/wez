package browser

import "testing"

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
