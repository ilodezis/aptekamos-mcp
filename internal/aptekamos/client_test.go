package aptekamos

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.BaseURL = srv.URL
	c.MinInterval = 0
	return c
}

func TestClientGetFollowsRedirectsAndCaches(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/magaziny/org-x-27324" {
			http.Redirect(w, r, "/magaziny/org-apteka-xamovniki-27324", http.StatusMovedPermanently)
			return
		}
		if ua := r.Header.Get("User-Agent"); !strings.Contains(ua, "Mozilla") {
			t.Errorf("user agent = %q", ua)
		}
		_, _ = w.Write([]byte("<html><h1 id='am-pc-org-name'>Аптека</h1></html>"))
	}))
	defer srv.Close()
	c := testClient(srv)

	p, err := c.Get(context.Background(), "/magaziny/org-x-27324", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "/magaziny/org-apteka-xamovniki-27324" {
		t.Errorf("final path = %q", p.Path)
	}
	if !strings.Contains(string(p.Body), "Аптека") {
		t.Errorf("body = %q", p.Body)
	}
	if _, err := c.Get(context.Background(), "/magaziny/org-x-27324", time.Minute); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 2 {
		t.Errorf("site hits = %d, want 2 (redirect + page, then cache)", n)
	}
	// ttl 0 bypasses the cache
	if _, err := c.Get(context.Background(), "/magaziny/org-x-27324", 0); err != nil {
		t.Fatal(err)
	}
	if n := hits.Load(); n != 4 {
		t.Errorf("site hits = %d, want 4", n)
	}
}

func TestClientErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Ошибка 4xx"))
		case "/captcha":
			_, _ = w.Write([]byte(`<html><body page-id="AME-CAPTCHA-1">Подтвердите, что вы не робот</body></html>`))
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "/busy":
			w.WriteHeader(http.StatusTooManyRequests)
		case "/broken":
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	c := testClient(srv)
	ctx := context.Background()

	if _, err := c.Get(ctx, "/missing", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Errorf("404: %v", err)
	}
	for _, p := range []string{"/captcha", "/forbidden", "/busy"} {
		if _, err := c.Get(ctx, p, time.Minute); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := c.Get(ctx, "/broken", time.Minute); err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "502") {
		t.Errorf("502: %v", err)
	}
}

func TestClientThrottle(t *testing.T) {
	var mu sync.Mutex
	var starts []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		starts = append(starts, time.Now())
		mu.Unlock()
	}))
	defer srv.Close()
	c := testClient(srv)
	c.MinInterval = 80 * time.Millisecond

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = c.Get(context.Background(), "/p"+string(rune('a'+i)), 0)
		}(i)
	}
	wg.Wait()
	if len(starts) != 3 {
		t.Fatalf("requests = %d", len(starts))
	}
	first, last := starts[0], starts[0]
	for _, s := range starts {
		if s.Before(first) {
			first = s
		}
		if s.After(last) {
			last = s
		}
	}
	if gap := last.Sub(first); gap < 150*time.Millisecond {
		t.Errorf("three requests within %v, want >= 160ms apart in total", gap)
	}
}

func TestClientThrottleHonoursContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := testClient(srv)
	c.MinInterval = time.Hour
	if _, err := c.Get(context.Background(), "/a", 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := c.Get(ctx, "/b", 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestCacheEvictsBySize(t *testing.T) {
	cc := newCache(100)
	cc.put("a", &Page{Body: make([]byte, 60)}, time.Minute)
	cc.put("b", &Page{Body: make([]byte, 60)}, time.Minute)
	if cc.get("a") != nil {
		t.Error("oldest entry survived over the size cap")
	}
	if cc.get("b") == nil {
		t.Error("newest entry evicted")
	}
	cc.put("huge", &Page{Body: make([]byte, 500)}, time.Minute)
	if cc.get("huge") != nil {
		t.Error("an entry bigger than the whole cache was kept")
	}
	cc.put("short", &Page{Body: []byte("x")}, time.Nanosecond)
	time.Sleep(time.Millisecond)
	if cc.get("short") != nil {
		t.Error("expired entry returned")
	}
}

// The client keeps no session: a cookie the site sets must not come back on the next request.
func TestClientSendsNoCookies(t *testing.T) {
	var seen []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Cookie"))
		mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: "amSession", Value: "abc", Path: "/"})
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()
	c := testClient(srv)
	for _, path := range []string{"/one", "/two", "/three"} {
		if _, err := c.Get(context.Background(), path, 0); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("requests = %d", len(seen))
	}
	for i, ck := range seen {
		if ck != "" {
			t.Errorf("request %d carried Cookie %q", i+1, ck)
		}
	}
}
