package history

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Seen struct {
	mu   sync.Mutex
	path string
	set  map[string]struct{}
}

func NewSeen() *Seen {
	homeDir, _ := os.UserHomeDir()
	dir := filepath.Join(homeDir, ".cache", "wez")
	return &Seen{
		path: filepath.Join(dir, "seen_urls"),
		set:  make(map[string]struct{}),
	}
}

func (s *Seen) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	s.set = make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		u := strings.TrimSpace(scanner.Text())
		if u == "" {
			continue
		}
		s.set[u] = struct{}{}
	}
	return scanner.Err()
}

func (s *Seen) Add(rawURL string) error {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.set == nil {
		s.set = make(map[string]struct{})
	}
	if _, ok := s.set[u]; ok {
		return nil
	}
	s.set[u] = struct{}{}

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(u + "\n")
	return err
}

func (s *Seen) Contains(rawURL string) bool {
	u := strings.TrimSpace(rawURL)
	if u == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.set[u]
	return ok
}

func (s *Seen) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.set = make(map[string]struct{})
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
