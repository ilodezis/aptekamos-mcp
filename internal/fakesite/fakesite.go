// Package fakesite stands in for aptekamos.ru in tests: it serves the saved pages of
// internal/aptekamos/testdata by path and records every request.
package fakesite

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

type Site struct {
	URL string

	t    testing.TB
	dir  string
	mu   sync.Mutex
	reqs []string
}

var routes = []struct {
	re      *regexp.Regexp
	fixture string
}{
	{regexp.MustCompile(`-117477/kupit/zameni$`), "inn_tirz.html"},
	{regexp.MustCompile(`^/tovary/lekarstva/x-1/x-11562/ceni$`), "where_zhivika.html"},
	{regexp.MustCompile(`/med-x-11562/kupit/org-x-29199$`), "offer_zhivika.html"},
	{regexp.MustCompile(`/med-x-117477/kupit/org-x-\d+$`), "offer_tirz.html"},
	{regexp.MustCompile(`^/tovary/lekarstva/x-1/x-117477/ceni$`), "drug_packs.html"},
	{regexp.MustCompile(`^/tovary/lekarstva/x-1/x-117\d{3}/ceni$`), "drug_tirz.html"},
	{regexp.MustCompile(`^/tovary/inn-x-17562$`), "inn_tirz.html"},
	{regexp.MustCompile(`^/modal/regiony$`), "regions.html"},
	{regexp.MustCompile(`^/modal/metro$`), "metro.html"},
	{regexp.MustCompile(`^/modal/nets$`), "networks.html"},
	{regexp.MustCompile(`^/modal/streets$`), "streets.html"},
	{regexp.MustCompile(`(/ceni|/kupit)/na-karte$|/na-karte/karta$`), "map_network.html"},
	{regexp.MustCompile(`/kupit/zameni$`), "substitutes.html"},
	{regexp.MustCompile(`/trd-x-40203/frm-x-27/kupit/org-x-8528$`), "offer_dupes.html"},
	{regexp.MustCompile(`/med-x-13270/kupit/org-x-\d+$`), "offer_drug.html"},
	{regexp.MustCompile(`/kupit/org-x-\d+$`), "offer_tradeform.html"},
	{regexp.MustCompile(`^/tovary/lekarstva/x-1/x-13270/ceni$`), "drug.html"},
	{regexp.MustCompile(`^/tovary/lekarstva/x-1/x-13270/instrukciya$`), "instruction.html"},
	{regexp.MustCompile(`^/tovary/trd-x-40203/frm-x-27/kupit$`), "where_dosages.html"},
	{regexp.MustCompile(`^/tovary/trd-x-\d+/frm-x-\d+/kupit$`), "where_tradeform.html"},
	{regexp.MustCompile(`^/tovary/inn-x-\d+$`), "inn.html"},
	{regexp.MustCompile(`^/tovary/phg-x-\d+$`), "group.html"},
	{regexp.MustCompile(`^/tovary/trd-x-\d+$`), "brand.html"},
	{regexp.MustCompile(`^/tovary$`), "search.html"},
	{regexp.MustCompile(`^/magaziny/org-x-\d+/usloviya-zakaza$`), "order_conditions_online.html"},
	{regexp.MustCompile(`^/magaziny/org-x-\d+/price-list$`), "price_list.html"},
	{regexp.MustCompile(`^/magaziny/org-x-24084$`), "pharmacy_order.html"},
	{regexp.MustCompile(`^/magaziny/org-x-27324$`), "pharmacy.html"},
	{regexp.MustCompile(`^/magaziny/apteki/na-karte$`), "pharmacies.html"},
	{regexp.MustCompile(`^/korzina$`), "basket.html"},
}

var regionPrefix = regexp.MustCompile(`^/([a-z-]+)(/.*)$`)

var topLevel = map[string]bool{"tovary": true, "magaziny": true, "modal": true, "korzina": true}

// New starts the fake site; dir is the fixtures folder.
func New(t testing.TB, dir string) *Site {
	s := &Site{t: t, dir: dir}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

func (s *Site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.reqs = append(s.reqs, r.URL.RequestURI())
	s.mu.Unlock()
	path := r.URL.Path
	if m := regionPrefix.FindStringSubmatch(path); m != nil && !topLevel[m[1]] {
		path = m[2]
	}
	if strings.Contains(path, "999999999") { // an id nobody has
		w.WriteHeader(http.StatusNotFound)
		return
	}
	for _, rt := range routes {
		if !rt.re.MatchString(path) {
			continue
		}
		name := rt.fixture
		if path == "/tovary" {
			name = searchFixture(r.URL.Query().Get("q"), name)
		}
		if name == "drug_packs.html" {
			// the pack filter (?p=) changes the header prices and pharmacy count
			switch r.URL.Query().Get("p") {
			case "1":
				name = "drug_packs_p1.html"
			case "4":
				name = "drug_packs_p4.html"
			}
		}
		if path == "/tovary" && strings.Contains(r.URL.Query().Get("q"), "тирзепатид") {
			name = "search_tirz.html"
		}
		body, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			s.t.Error(err)
		}
		_, _ = w.Write(body)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

// searchFixture picks the search page for a query. Like the real site it matches a well-known
// brand typed in Latin ("nurofen") by itself, finds nothing for other Latin-only names, and knows
// the tirzepatide brands.
func searchFixture(q, def string) string {
	q = strings.ToLower(q)
	switch {
	case strings.Contains(q, "zzz") || strings.Contains(q, "ззз"):
		return "search_empty.html"
	case strings.Contains(q, "тирзепатид") || strings.Contains(q, "седжаро"):
		return "search_tirz.html"
	case strings.Contains(q, "nurofen"):
		return def
	case strings.IndexFunc(q, func(r rune) bool { return r >= 'a' && r <= 'z' }) >= 0 &&
		strings.IndexFunc(q, func(r rune) bool { return r >= 'а' && r <= 'я' }) < 0:
		return "search_empty.html"
	}
	return def
}

// Requests returns the request URIs seen so far.
func (s *Site) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

// Reset forgets the requests seen so far.
func (s *Site) Reset() {
	s.mu.Lock()
	s.reqs = nil
	s.mu.Unlock()
}

// Last returns the path and query of the latest request.
func (s *Site) Last(t testing.TB) (string, url.Values) {
	t.Helper()
	reqs := s.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the site")
	}
	u, err := url.Parse(reqs[len(reqs)-1])
	if err != nil {
		t.Fatal(err)
	}
	return u.Path, u.Query()
}
