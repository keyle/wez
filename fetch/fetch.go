package fetch

import (
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Result struct {
	Body        []byte
	ContentType string
	StatusCode  int
	FinalURL    string // after redirects
}

func Fetch(rawURL string) (*Result, error) {
	// Normalise URL: add https:// if no scheme present.
	if !strings.Contains(rawURL, "://") {
		rawURL = "https://" + rawURL
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "wez/1.0 (terminal browser)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", rawURL, err)
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
	body, err := io.ReadAll(io.LimitReader(reader, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}

	return &Result{
		Body:        body,
		ContentType: resp.Header.Get("Content-Type"),
		StatusCode:  resp.StatusCode,
		FinalURL:    resp.Request.URL.String(),
	}, nil
}
