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
		bc := testClient(srv)
		if _, err := bc.Get(ctx, p, time.Minute); !errors.Is(err, ErrBlocked) {
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

func TestCacheLRUEviction(t *testing.T) {
	cc := newCache(120)
	cc.put("a", &Page{Body: make([]byte, 60)}, time.Minute)
	cc.put("b", &Page{Body: make([]byte, 60)}, time.Minute)
	// Accessing "a" promotes it to most-recently-used so "b" is evicted next.
	if cc.get("a") == nil {
		t.Fatal("expected a in cache")
	}
	cc.put("c", &Page{Body: make([]byte, 60)}, time.Minute)
	if cc.get("a") == nil {
		t.Error("recently used entry a was evicted")
	}
	if cc.get("b") != nil {
		t.Error("least recently used entry b survived")
	}
	if cc.get("c") == nil {
		t.Error("newly inserted entry c missing")
	}
}

func TestClientSingleflight(t *testing.T) {
	var hits atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = w.Write([]byte("<html>shared</html>"))
	}))
	defer srv.Close()
	c := testClient(srv)

	const n = 8
	var wg sync.WaitGroup
	pages := make([]*Page, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pages[i], errs[i] = c.Get(context.Background(), "/same", time.Minute)
		}(i)
	}
	<-started
	// Give the other goroutines a moment to queue on the flight.
	time.Sleep(25 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := hits.Load(); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Errorf("caller %d err = %v", i, errs[i])
		} else if string(pages[i].Body) != "<html>shared</html>" {
			t.Errorf("caller %d body = %q", i, pages[i].Body)
		}
	}
}

func TestClientSingleflightCancelOneWaiter(t *testing.T) {
	var hits atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = w.Write([]byte("<html>ok</html>"))
	}))
	defer srv.Close()
	c := testClient(srv)

	ctx1, cancel1 := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Get(ctx1, "/one", time.Minute)
		errCh <- err
	}()
	<-started

	pageCh := make(chan *Page, 1)
	go func() {
		p, err := c.Get(context.Background(), "/one", time.Minute)
		if err != nil {
			t.Errorf("waiter 2 err = %v", err)
		}
		pageCh <- p
	}()
	time.Sleep(20 * time.Millisecond)

	// Cancelling caller 1 must return context.Canceled immediately without aborting caller 2.
	cancel1()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Errorf("caller 1 err = %v, want context.Canceled", err)
	}

	close(release)
	if p := <-pageCh; p == nil || string(p.Body) != "<html>ok</html>" {
		t.Errorf("caller 2 page = %+v", p)
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
}

func TestClientBlockedCooldown(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/cached":
			_, _ = w.Write([]byte("<html>cached</html>"))
		case "/block":
			w.WriteHeader(203)
		default:
			_, _ = w.Write([]byte("<html>ok</html>"))
		}
	}))
	defer srv.Close()
	c := testClient(srv)
	c.BlockedCooldown = 60 * time.Millisecond
	ctx := context.Background()

	// Warm one cached entry before the block.
	if _, err := c.Get(ctx, "/cached", time.Minute); err != nil {
		t.Fatal(err)
	}
	hitsBefore := hits.Load()

	// Trigger block (203).
	_, err := c.Get(ctx, "/block", time.Minute)
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "retry after") {
		t.Fatalf("blocked err = %v", err)
	}
	if hits.Load() != hitsBefore+1 {
		t.Fatalf("expected 1 hit on /block, got %d", hits.Load()-hitsBefore)
	}

	// Uncached request during cooldown fails fast without hitting the server.
	_, err = c.Get(ctx, "/other", time.Minute)
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "retry after") {
		t.Fatalf("cooldown err = %v", err)
	}
	if hits.Load() != hitsBefore+1 {
		t.Errorf("cooldown did not short-circuit: hits = %d, want %d", hits.Load(), hitsBefore+1)
	}

	// Cached page is still served during cooldown.
	if p, err := c.Get(ctx, "/cached", time.Minute); err != nil || string(p.Body) != "<html>cached</html>" {
		t.Errorf("cached page during cooldown = %+v, %v", p, err)
	}

	// After cooldown expires, requests reach the server again.
	time.Sleep(80 * time.Millisecond)
	p, err := c.Get(ctx, "/other", time.Minute)
	if err != nil || string(p.Body) != "<html>ok</html>" {
		t.Errorf("after cooldown = %+v, %v", p, err)
	}
	if hits.Load() != hitsBefore+2 {
		t.Errorf("expected server hit after cooldown, hits = %d", hits.Load())
	}
}
