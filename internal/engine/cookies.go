package engine

import (
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

type cookieUpdate struct {
	URL     string
	Cookies []*http.Cookie
}

// cookieStore records immutable updates so child scopes can fork a jar and
// cached shared calls can replay only the callee's changes into another jar.
type cookieStore struct {
	jar     http.CookieJar
	mu      sync.Mutex
	history []cookieUpdate
}

func newCookieStore() *cookieStore {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &cookieStore{jar: jar}
}
func (s *cookieStore) Cookies(u *url.URL) []*http.Cookie { return s.jar.Cookies(u) }
func (s *cookieStore) SetCookies(u *url.URL, cookies []*http.Cookie) {
	s.jar.SetCookies(u, cookies)
	if len(cookies) == 0 {
		return
	}
	update := cookieUpdate{URL: u.String(), Cookies: copyCookies(cookies)}
	for _, c := range update.Cookies {
		// Cache replay must not restart a relative Max-Age lifetime.
		if c.MaxAge > 0 {
			if int64(c.MaxAge) > math.MaxInt64/int64(time.Second) {
				c.Expires = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
			} else {
				c.Expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
			}
			c.MaxAge = 0
		}
	}
	s.mu.Lock()
	s.history = append(s.history, update)
	s.mu.Unlock()
}
func copyCookies(in []*http.Cookie) []*http.Cookie {
	out := make([]*http.Cookie, len(in))
	for i, c := range in {
		v := *c //nolint:gosec // Preserve the server's cookie attributes exactly; a copy must not change cookie policy.
		v.Unparsed = append([]string(nil), c.Unparsed...)
		out[i] = &v
	}
	return out
}
func copyUpdates(in []cookieUpdate) []cookieUpdate {
	out := make([]cookieUpdate, len(in))
	for i, u := range in {
		out[i] = cookieUpdate{URL: u.URL, Cookies: copyCookies(u.Cookies)}
	}
	return out
}
func (s *cookieStore) updates(from int) []cookieUpdate {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyUpdates(s.history[min(max(from, 0), len(s.history)):])
}
func (s *cookieStore) replay(updates []cookieUpdate) {
	for _, update := range updates {
		u, err := url.Parse(update.URL)
		if err == nil {
			s.SetCookies(u, update.Cookies)
		}
	}
}
func (s *cookieStore) fork() *cookieStore {
	next := newCookieStore()
	next.replay(s.updates(0))
	return next
}
func (s *cookieStore) count() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.history)
}
