package favorites

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"wez/config"
)

type Item struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Category string    `json:"category"`
	AddedAt  time.Time `json:"added_at"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	items []Item
}

func New() *Store {
	return &Store{path: filepath.Join(config.ConfigDir(), "fav.json")}
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.items = nil
			return nil
		}
		return err
	}

	var items []Item
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	s.items = sanitizeItems(items)
	return nil
}

func (s *Store) List() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Item, len(s.items))
	copy(out, s.items)
	return out
}

func (s *Store) Get(rawURL string) (Item, bool) {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return Item{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.URL == u {
			return item, true
		}
	}
	return Item{}, false
}

func (s *Store) Add(rawURL, title, category string) error {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return nil
	}
	t := strings.TrimSpace(title)
	if t == "" {
		t = u
	}
	c := normalizeCategory(category)

	s.mu.Lock()
	defer s.mu.Unlock()

	if i := s.indexOfURL(u); i >= 0 {
		s.items[i].Title = t
		s.items[i].Category = c
		if s.items[i].AddedAt.IsZero() {
			s.items[i].AddedAt = time.Now()
		}
	} else {
		s.items = append(s.items, Item{
			URL:      u,
			Title:    t,
			Category: c,
			AddedAt:  time.Now(),
		})
	}

	return s.saveLocked()
}

func (s *Store) Remove(rawURL string) (bool, error) {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return false, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	i := s.indexOfURL(u)
	if i < 0 {
		return false, nil
	}

	copy(s.items[i:], s.items[i+1:])
	s.items = s.items[:len(s.items)-1]

	if err := s.saveLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func normalizeCategory(category string) string {
	c := strings.TrimSpace(category)
	if c == "" {
		return "general"
	}
	return c
}

func sanitizeItems(items []Item) []Item {
	out := make([]Item, 0, len(items))
	seen := make(map[string]bool, len(items))

	for _, item := range items {
		u := strings.TrimSpace(item.URL)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true

		t := strings.TrimSpace(item.Title)
		if t == "" {
			t = u
		}
		c := normalizeCategory(item.Category)
		if item.AddedAt.IsZero() {
			item.AddedAt = time.Now()
		}

		out = append(out, Item{
			URL:      u,
			Title:    t,
			Category: c,
			AddedAt:  item.AddedAt,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return strings.ToLower(out[i].Category) < strings.ToLower(out[j].Category)
		}
		return out[i].AddedAt.After(out[j].AddedAt)
	})

	return out
}

func (s *Store) indexOfURL(url string) int {
	for i, item := range s.items {
		if item.URL == url {
			return i
		}
	}
	return -1
}

func (s *Store) saveLocked() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	items := sanitizeItems(s.items)
	s.items = items

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "fav-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
