package fetch

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"wez/config"
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
	if !strings.Contains(ua, config.Version) {
		t.Errorf("expected user agent containing version %q, got %q", config.Version, ua)
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

func TestFetchUnsupportedScheme(t *testing.T) {
	_, err := Fetch("data:image/png;base64,AAAA")
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
	if !strings.Contains(err.Error(), "unsupported URL scheme") {
		t.Fatalf("expected unsupported scheme error, got: %v", err)
	}
}

func TestFetchEmptyURL(t *testing.T) {
	_, err := Fetch("   ")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "empty URL") {
		t.Fatalf("expected empty URL error, got: %v", err)
	}
}

func TestClientPersistsCookies(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/set":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc123", Path: "/"})
			_, _ = w.Write([]byte("ok"))
		case "/check":
			cookie, err := r.Cookie("sid")
			if err != nil {
				http.Error(w, "missing cookie", http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(cookie.Value))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	c := NewClient()
	if _, err := c.Fetch(ts.URL + "/set"); err != nil {
		t.Fatalf("set fetch failed: %v", err)
	}
	res, err := c.Fetch(ts.URL + "/check")
	if err != nil {
		t.Fatalf("check fetch failed: %v", err)
	}
	if string(res.Body) != "abc123" {
		t.Fatalf("expected cookie value abc123, got %q", string(res.Body))
	}
}

func TestClientSubmitFormPOST(t *testing.T) {
	var gotMethod string
	var gotCT string
	var gotBody string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	c := NewClient()
	_, err := c.SubmitForm(ts.URL, "POST", url.Values{"a": {"1"}, "b": {"two words"}})
	if err != nil {
		t.Fatalf("unexpected submit error: %v", err)
	}

	if gotMethod != "POST" {
		t.Fatalf("expected POST, got %q", gotMethod)
	}
	if !strings.HasPrefix(strings.ToLower(gotCT), "application/x-www-form-urlencoded") {
		t.Fatalf("unexpected content-type: %q", gotCT)
	}
	if gotBody != "a=1&b=two+words" && gotBody != "b=two+words&a=1" {
		t.Fatalf("unexpected form body: %q", gotBody)
	}
}

func TestPersistentCookiesAcrossClients(t *testing.T) {
	var sawCookie bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "persist-me", Path: "/"})
			_, _ = w.Write([]byte("ok"))
		case "/check":
			cookie, err := r.Cookie("sid")
			if err == nil && cookie.Value == "persist-me" {
				sawCookie = true
			}
			_, _ = w.Write([]byte("done"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	jarPath := filepath.Join(t.TempDir(), "cookies.json")
	opts := ClientOptions{PersistCookies: true, CookieJarPath: jarPath, PersistSessionCookies: true}

	c1 := NewClientWithOptions(opts)
	if _, err := c1.Fetch(ts.URL + "/login"); err != nil {
		t.Fatalf("login fetch failed: %v", err)
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("close save failed: %v", err)
	}

	c2 := NewClientWithOptions(opts)
	defer func() { _ = c2.Close() }()
	if _, err := c2.Fetch(ts.URL + "/check"); err != nil {
		t.Fatalf("check fetch failed: %v", err)
	}
	if !sawCookie {
		t.Fatal("expected persisted cookie to be sent by second client")
	}
}

func TestPersistSessionCookiesDisabled(t *testing.T) {
	var sawCookie bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "session-only", Path: "/"})
			_, _ = w.Write([]byte("ok"))
		case "/check":
			if _, err := r.Cookie("sid"); err == nil {
				sawCookie = true
			}
			_, _ = w.Write([]byte("done"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	jarPath := filepath.Join(t.TempDir(), "cookies.json")
	opts := ClientOptions{PersistCookies: true, CookieJarPath: jarPath, PersistSessionCookies: false}

	c1 := NewClientWithOptions(opts)
	if _, err := c1.Fetch(ts.URL + "/login"); err != nil {
		t.Fatalf("login fetch failed: %v", err)
	}
	if err := c1.Close(); err != nil {
		t.Fatalf("close save failed: %v", err)
	}

	c2 := NewClientWithOptions(opts)
	defer func() { _ = c2.Close() }()
	if _, err := c2.Fetch(ts.URL + "/check"); err != nil {
		t.Fatalf("check fetch failed: %v", err)
	}
	if sawCookie {
		t.Fatal("expected session cookie not to persist when disabled")
	}
}
