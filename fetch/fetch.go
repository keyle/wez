package fetch

import (
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"wez/config"
)

type Result struct {
	Body        []byte
	ContentType string
	StatusCode  int
	FinalURL    string // after redirects
}

// Client is a stateful fetcher with cookie support for sessions.
type Client struct {
	httpClient *http.Client
	jar        *persistentCookieJar
}

type ClientOptions struct {
	PersistCookies        bool
	CookieJarPath         string
	PersistSessionCookies bool
}

func NewClient() *Client {
	return NewClientWithOptions(ClientOptions{})
}

func NewClientWithOptions(opts ClientOptions) *Client {
	if opts.PersistCookies && strings.TrimSpace(opts.CookieJarPath) != "" {
		jar, err := newPersistentCookieJar(opts.CookieJarPath, opts.PersistSessionCookies)
		if err == nil {
			return &Client{httpClient: newHTTPClient(jar), jar: jar}
		}
	}

	inMemory, _ := cookiejar.New(nil)
	return &Client{httpClient: newHTTPClient(inMemory)}
}

func Fetch(rawURL string) (*Result, error) {
	return NewClient().Fetch(rawURL)
}

func (c *Client) Fetch(rawURL string) (*Result, error) {
	return c.request("GET", rawURL, nil)
}

func (c *Client) SubmitForm(rawURL, method string, values url.Values) (*Result, error) {
	m := strings.ToUpper(strings.TrimSpace(method))
	if m == "" {
		m = "GET"
	}
	if m != "GET" && m != "POST" {
		return nil, fmt.Errorf("unsupported form method: %s", m)
	}
	return c.request(m, rawURL, values)
}

func (c *Client) Close() error {
	if c == nil || c.jar == nil {
		return nil
	}
	return c.jar.Save()
}

func newHTTPClient(jar http.CookieJar) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
		Jar:       jar,
	}
}

func (c *Client) request(method, rawURL string, values url.Values) (*Result, error) {
	if c == nil || c.httpClient == nil {
		inMemory, _ := cookiejar.New(nil)
		c = &Client{httpClient: newHTTPClient(inMemory)}
	}

	normalizedURL, err := normalizeURL(rawURL)
	if err != nil {
		return nil, err
	}

	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}

	var body io.Reader
	requestURL := normalizedURL

	if method == "GET" && len(values) > 0 {
		u, err := url.Parse(normalizedURL)
		if err != nil {
			return nil, fmt.Errorf("invalid URL %q: %w", normalizedURL, err)
		}
		q := u.Query()
		for k, vs := range values {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
		requestURL = u.String()
	}

	if method == "POST" {
		body = strings.NewReader(values.Encode())
	}

	req, err := http.NewRequest(method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", requestURL, err)
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip decode: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	// Limit read to 10 MB.
	bodyBytes, err := io.ReadAll(io.LimitReader(reader, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	return &Result{
		Body:        bodyBytes,
		ContentType: resp.Header.Get("Content-Type"),
		StatusCode:  resp.StatusCode,
		FinalURL:    resp.Request.URL.String(),
	}, nil
}

func normalizeURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", fmt.Errorf("empty URL")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}

	if parsed.Scheme == "" {
		if strings.HasPrefix(rawURL, "//") {
			rawURL = "https:" + rawURL
		} else {
			rawURL = "https://" + rawURL
		}
		parsed, err = url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("invalid URL %q: %w", rawURL, err)
		}
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme: %s", parsed.Scheme)
	}

	return rawURL, nil
}

func userAgent() string {
	return fmt.Sprintf("wez/%s (terminal browser)", config.Version)
}
