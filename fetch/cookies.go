package fetch

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type persistentCookieJar struct {
	base           http.CookieJar
	path           string
	persistSession bool

	mu      sync.Mutex
	records map[string]storedCookie
}

type storedCookie struct {
	Origin   string        `json:"origin"`
	Cookie   cookiePayload `json:"cookie"`
	SavedAt  int64         `json:"saved_at,omitempty"`
	HostOnly bool          `json:"host_only,omitempty"`
}

type cookiePayload struct {
	Name     string        `json:"name"`
	Value    string        `json:"value"`
	Path     string        `json:"path,omitempty"`
	Domain   string        `json:"domain,omitempty"`
	Expires  int64         `json:"expires,omitempty"`
	MaxAge   int           `json:"max_age,omitempty"`
	Secure   bool          `json:"secure,omitempty"`
	HTTPOnly bool          `json:"http_only,omitempty"`
	SameSite http.SameSite `json:"same_site,omitempty"`
}

func newPersistentCookieJar(path string, persistSession bool) (*persistentCookieJar, error) {
	base, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	jar := &persistentCookieJar{
		base:           base,
		path:           path,
		persistSession: persistSession,
		records:        make(map[string]storedCookie),
	}
	if err := jar.load(); err != nil {
		return nil, err
	}
	return jar, nil
}

func (j *persistentCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if u == nil {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	j.base.SetCookies(u, cookies)
	now := time.Now()
	origin := cookieOriginURL(u)
	for _, ck := range cookies {
		if ck == nil || strings.TrimSpace(ck.Name) == "" {
			continue
		}
		if cookieIsExpired(ck, now) {
			delete(j.records, cookieKey(origin, ck))
			continue
		}

		normalized := cloneCookie(ck)
		if normalized.Path == "" {
			normalized.Path = defaultCookiePath(u)
		}
		hostOnly := false
		if strings.TrimSpace(normalized.Domain) == "" {
			normalized.Domain = strings.ToLower(u.Hostname())
			hostOnly = true
		}

		j.records[cookieKey(origin, normalized)] = storedCookie{
			Origin:   origin,
			Cookie:   toCookiePayload(normalized),
			SavedAt:  now.Unix(),
			HostOnly: hostOnly,
		}
	}

	_ = j.saveLocked()
}

func (j *persistentCookieJar) Cookies(u *url.URL) []*http.Cookie {
	if u == nil {
		return nil
	}
	return j.base.Cookies(u)
}

func (j *persistentCookieJar) Save() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.saveLocked()
}

func (j *persistentCookieJar) load() error {
	if strings.TrimSpace(j.path) == "" {
		return nil
	}

	data, err := os.ReadFile(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var items []storedCookie
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}

	now := time.Now()
	for _, item := range items {
		if strings.TrimSpace(item.Origin) == "" || strings.TrimSpace(item.Cookie.Name) == "" {
			continue
		}
		if !j.persistSession && isSessionPayload(item.Cookie) {
			continue
		}

		ck := fromCookiePayload(item.Cookie)
		if cookieIsExpired(ck, now) {
			continue
		}

		u, err := url.Parse(item.Origin)
		if err != nil || u.Host == "" {
			continue
		}
		j.base.SetCookies(u, []*http.Cookie{ck})
		j.records[cookieKey(item.Origin, ck)] = item
	}

	return nil
}

func (j *persistentCookieJar) saveLocked() error {
	if strings.TrimSpace(j.path) == "" {
		return nil
	}

	now := time.Now()
	items := make([]storedCookie, 0, len(j.records))
	for key, item := range j.records {
		ck := fromCookiePayload(item.Cookie)
		if cookieIsExpired(ck, now) {
			delete(j.records, key)
			continue
		}
		if !j.persistSession && isSessionPayload(item.Cookie) {
			continue
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, k int) bool {
		if items[i].Origin == items[k].Origin {
			return items[i].Cookie.Name < items[k].Cookie.Name
		}
		return items[i].Origin < items[k].Origin
	})

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(j.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "cookies-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, j.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func cookieOriginURL(u *url.URL) string {
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: u.Host}).String()
}

func defaultCookiePath(u *url.URL) string {
	p := strings.TrimSpace(u.EscapedPath())
	if p == "" || p[0] != '/' {
		return "/"
	}
	if p == "/" {
		return "/"
	}
	idx := strings.LastIndex(p, "/")
	if idx <= 0 {
		return "/"
	}
	return p[:idx]
}

func cookieKey(origin string, ck *http.Cookie) string {
	domain := strings.ToLower(strings.TrimSpace(ck.Domain))
	path := strings.TrimSpace(ck.Path)
	if path == "" {
		path = "/"
	}
	return origin + "\t" + domain + "\t" + path + "\t" + ck.Name
}

func cookieIsExpired(ck *http.Cookie, now time.Time) bool {
	if ck == nil {
		return true
	}
	if ck.MaxAge < 0 {
		return true
	}
	if !ck.Expires.IsZero() && now.After(ck.Expires) {
		return true
	}
	return false
}

func isSessionPayload(p cookiePayload) bool {
	return p.Expires == 0 && p.MaxAge == 0
}

func toCookiePayload(c *http.Cookie) cookiePayload {
	p := cookiePayload{
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		Domain:   c.Domain,
		MaxAge:   c.MaxAge,
		Secure:   c.Secure,
		HTTPOnly: c.HttpOnly,
		SameSite: c.SameSite,
	}
	if !c.Expires.IsZero() {
		p.Expires = c.Expires.Unix()
	}
	return p
}

func fromCookiePayload(p cookiePayload) *http.Cookie {
	ck := &http.Cookie{
		Name:     p.Name,
		Value:    p.Value,
		Path:     p.Path,
		Domain:   p.Domain,
		MaxAge:   p.MaxAge,
		Secure:   p.Secure,
		HttpOnly: p.HTTPOnly,
		SameSite: p.SameSite,
	}
	if p.Expires > 0 {
		ck.Expires = time.Unix(p.Expires, 0)
	}
	return ck
}

func cloneCookie(c *http.Cookie) *http.Cookie {
	if c == nil {
		return nil
	}
	dup := *c
	return &dup
}
