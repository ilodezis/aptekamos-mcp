package aptekamos

import (
	"bytes"
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	// DefaultMinInterval spaces requests to the site; AptekaMos is a small site and gets two a second at most.
	DefaultMinInterval = 500 * time.Millisecond
	defaultCacheBytes  = 192 << 20
	userAgent          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0 Safari/537.36"
	maxBodyBytes       = 64 << 20
)

var (
	ErrNotFound = errors.New("AptekaMos has no such page")
	ErrBlocked  = errors.New("AptekaMos refused the request (captcha or rate limit); try again in a few minutes")
)

// The site's own scripts reload the page when they see these markers or a 203.
var blockedRe = regexp.MustCompile(`page-id=["']?[^> ]*AME-(?:CAPTCHA|ERR-403)`)

// Page is a fetched page: its body and the path it ended up at after redirects.
type Page struct {
	Body []byte
	Path string
}

func (p *Page) Doc() (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(bytes.NewReader(p.Body))
}

// Client fetches AptekaMos pages one at a time with a pause between requests and caches them.
type Client struct {
	HTTP        *http.Client
	BaseURL     string
	MinInterval time.Duration

	cache     *cache
	rateMu    sync.Mutex
	nextStart time.Time
}

func NewClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 60 * time.Second},
		BaseURL:     siteHost,
		MinInterval: DefaultMinInterval,
		cache:       newCache(defaultCacheBytes),
	}
}

func (c *Client) throttle(ctx context.Context) error {
	c.rateMu.Lock()
	now := time.Now()
	start := c.nextStart
	if start.Before(now) {
		start = now
	}
	c.nextStart = start.Add(c.MinInterval)
	c.rateMu.Unlock()

	wait := time.Until(start)
	if wait <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Get fetches a site path ("/tovary?q=…"). A ttl above zero serves and stores the page in the cache.
func (c *Client) Get(ctx context.Context, path string, ttl time.Duration) (*Page, error) {
	if ttl > 0 {
		if p := c.cache.get(path); p != nil {
			return p, nil
		}
	}
	if err := c.throttle(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AptekaMos did not answer: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading AptekaMos answer: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusForbidden, resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode == 203:
		return nil, ErrBlocked
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("AptekaMos answered HTTP %d for %s", resp.StatusCode, path)
	case blockedRe.Match(body):
		return nil, ErrBlocked
	}
	p := &Page{Body: body, Path: strings.TrimPrefix(resp.Request.URL.String(), c.BaseURL)}
	if ttl > 0 {
		c.cache.put(path, p, ttl)
	}
	return p, nil
}

// cache keeps recent pages up to a byte budget, dropping the oldest first.
type cache struct {
	mu    sync.Mutex
	max   int
	used  int
	order *list.List
	items map[string]*list.Element
}

type cacheEntry struct {
	key     string
	page    *Page
	expires time.Time
}

func newCache(maxBytes int) *cache {
	return &cache{max: maxBytes, order: list.New(), items: map[string]*list.Element{}}
}

func (c *cache) get(key string) *Page {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil
	}
	e := el.Value.(*cacheEntry)
	if time.Now().After(e.expires) {
		c.remove(el)
		return nil
	}
	return e.page
}

func (c *cache) put(key string, p *Page, ttl time.Duration) {
	size := len(p.Body)
	if size > c.max {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.remove(el)
	}
	for c.used+size > c.max && c.order.Len() > 0 {
		c.remove(c.order.Front())
	}
	c.items[key] = c.order.PushBack(&cacheEntry{key: key, page: p, expires: time.Now().Add(ttl)})
	c.used += size
}

func (c *cache) remove(el *list.Element) {
	e := c.order.Remove(el).(*cacheEntry)
	delete(c.items, e.key)
	c.used -= len(e.page.Body)
}
