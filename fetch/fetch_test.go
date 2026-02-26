package fetch

import (
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchBasicHTML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte("<html><body><h1>Hello</h1></body></html>"))
	}))
	defer ts.Close()

	result, err := Fetch(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
	if !strings.Contains(result.ContentType, "text/html") {
		t.Errorf("expected text/html content type, got %q", result.ContentType)
	}
	if !strings.Contains(string(result.Body), "Hello") {
		t.Errorf("expected 'Hello' in body, got %q", string(result.Body))
	}
}

func TestFetchGzip(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Error("expected gzip in Accept-Encoding")
		}

		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Encoding", "gzip")

		gz := gzip.NewWriter(w)
		gz.Write([]byte("<html><body>Compressed</body></html>"))
		gz.Close()
	}))
	defer ts.Close()

	result, err := Fetch(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(result.Body), "Compressed") {
		t.Errorf("expected gzip-decoded body with 'Compressed', got %q", string(result.Body))
	}
}

func TestFetchRedirect(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/final", http.StatusMovedPermanently)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>Final</body></html>"))
	}))
	defer ts.Close()

	result, err := Fetch(ts.URL + "/redirect")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasSuffix(result.FinalURL, "/final") {
		t.Errorf("expected final URL to end with /final, got %q", result.FinalURL)
	}
	if !strings.Contains(string(result.Body), "Final") {
		t.Errorf("expected 'Final' in body")
	}
}

func TestFetchUserAgent(t *testing.T) {
	var ua string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.Write([]byte("ok"))
	}))
	defer ts.Close()

	_, err := Fetch(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(ua, "wez") {
		t.Errorf("expected user agent containing 'wez', got %q", ua)
	}
}

func TestFetchAutoHTTPS(t *testing.T) {
	// Fetching a domain without scheme should add https://.
	// This will fail to connect (no server), but the URL should be correct.
	_, err := Fetch("thisdomaindoesnotexist12345.example")
	if err == nil {
		t.Error("expected error fetching non-existent domain")
	}
}

func TestFetch404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("<html><body>Not Found</body></html>"))
	}))
	defer ts.Close()

	result, err := Fetch(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", result.StatusCode)
	}
}
