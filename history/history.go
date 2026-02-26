package history

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	URL       string
	Title     string
	Timestamp time.Time
}

type History struct {
	mu      sync.Mutex
	entries []Entry
	path    string
}

func New() *History {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".cache", "wez")
	return &History{
		path: filepath.Join(dir, "history"),
	}
}

func (h *History) Load() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	f, err := os.Open(h.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, parts[0])
		if err != nil {
			continue
		}
		title := ""
		if len(parts) >= 3 {
			title = parts[2]
		}
		h.entries = append(h.entries, Entry{
			Timestamp: ts,
			URL:       parts[1],
			Title:     title,
		})
	}
	return scanner.Err()
}

func (h *History) Add(url, title string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry := Entry{
		URL:       url,
		Title:     title,
		Timestamp: time.Now(),
	}
	h.entries = append(h.entries, entry)

	// Ensure directory exists.
	dir := filepath.Dir(h.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s\t%s\t%s\n",
		entry.Timestamp.Format(time.RFC3339),
		entry.URL,
		entry.Title,
	)
	return err
}

func (h *History) Entries() []Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]Entry, len(h.entries))
	copy(result, h.entries)
	return result
}

func (h *History) Contains(url string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.entries {
		if e.URL == url {
			return true
		}
	}
	return false
}
