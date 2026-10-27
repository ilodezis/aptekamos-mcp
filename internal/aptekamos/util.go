package aptekamos

import (
	"encoding/base64"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const siteHost = "https://aptekamos.ru"

// decodeLink turns a link from a page into a site-relative path. AptekaMos hides most links
// in data attributes as base64 of the absolute URL; plain links and anything that is not
// a site URL pass through (HTML entities already decoded by the parser are kept as they are).
func decodeLink(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.HasPrefix(s, "/") && !strings.Contains(s, ":") {
		if raw, err := base64.StdEncoding.DecodeString(s); err == nil && strings.HasPrefix(string(raw), "http") {
			s = string(raw)
		}
	}
	s = strings.ReplaceAll(s, "&amp;", "&")
	return strings.TrimPrefix(s, siteHost)
}

var segmentRe = regexp.MustCompile(`^([a-z]+)-(?:.*-)?(\d+)$`)

// segmentID finds the path segment "<prefix>-<slug>-<id>" and returns its id.
func segmentID(link, prefix string) int {
	path, _, _ := strings.Cut(link, "?")
	for _, seg := range strings.Split(path, "/") {
		m := segmentRe.FindStringSubmatch(seg)
		if m != nil && m[1] == prefix {
			id, _ := strconv.Atoi(m[2])
			return id
		}
	}
	return 0
}

var trailingIDRe = regexp.MustCompile(`-(\d+)$`)

// drugIDFromPath returns the id of an exact product from its page path:
// /tovary/<category>/<slug>-<id>/<slug>-<drug>/ceni, or a med-<slug>-<drug> segment.
func drugIDFromPath(link string) int {
	link = decodeLink(link)
	if id := segmentID(link, "med"); id != 0 {
		return id
	}
	path, _, _ := strings.Cut(link, "?")
	segs := strings.Split(strings.Trim(path, "/"), "/")
	for i, s := range segs {
		// the category segment (lekarstva, bad, izdeliya-med-naznacheniya...) carries no id
		if s != "tovary" || i+3 >= len(segs) || trailingIDRe.MatchString(segs[i+1]) {
			continue
		}
		if m := trailingIDRe.FindStringSubmatch(segs[i+3]); m != nil && trailingIDRe.MatchString(segs[i+2]) {
			id, _ := strconv.Atoi(m[1])
			return id
		}
	}
	return 0
}

// queryInt returns an integer query parameter of a link, 0 when it is absent.
func queryInt(link, key string) int {
	_, q, ok := strings.Cut(link, "?")
	if !ok {
		return 0
	}
	vals, err := url.ParseQuery(q)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(vals.Get(key))
	return n
}

var numberRe = regexp.MustCompile(`\d[\d\s\x{00a0}]*(?:[.,]\d+)?`)

func parseNumber(s string) float64 {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == ' ' || r == '\t' || r == '\n' {
			return -1
		}
		if r == ',' {
			return '.'
		}
		return r
	}, s)
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// parsePriceRange reads "627р", "286 ... 574р" or "1 045 ... 5 500" into a range.
// A single price is returned as both ends; text without digits gives zeros.
func parsePriceRange(s string) (float64, float64) {
	nums := numberRe.FindAllString(s, -1)
	switch len(nums) {
	case 0:
		return 0, 0
	case 1:
		v := parseNumber(nums[0])
		return v, v
	default:
		return parseNumber(nums[0]), parseNumber(nums[len(nums)-1])
	}
}

// parseCount reads the first integer in s ("5 117", "Аптеки (842)").
func parseCount(s string) int {
	m := numberRe.FindString(s)
	if m == "" {
		return 0
	}
	return int(parseNumber(m))
}

// splitPhones splits the ";"-joined phone list a network row shows, dropping the "..." tail.
func splitPhones(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ";") {
		p = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(p), "."))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var spaceRe = regexp.MustCompile(`\s+`)

func cleanText(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(strings.ReplaceAll(s, " ", " "), " "))
}

// distanceKm is the great-circle distance between two points.
func distanceKm(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}

var unitsRe = regexp.MustCompile(`№\s*(\d+)`)

// unitsFromName reads the pack size out of a name or a pack label: "№4" gives 4, no mark gives 0.
func unitsFromName(s string) int {
	m := unitsRe.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// unitPrice is the price of one unit of a pack, to the kopeck; it is 0 for a single unit or an unknown count.
func unitPrice(price float64, units int) float64 {
	if units <= 1 || price <= 0 {
		return 0
	}
	return math.Round(price/float64(units)*100) / 100
}
