package aptekamos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// How long pages stay cached. Prices and stock move during the day, leaflets and dictionaries do not.
const (
	ttlOffers     = 10 * time.Minute
	ttlProducts   = 30 * time.Minute
	ttlPharmacy   = time.Hour
	ttlReference  = 24 * time.Hour
	ttlLeaflet    = 24 * time.Hour
	ttlAllMap     = 6 * time.Hour
	maxListLength = 20
)

// Region is one of the site's regions; Slug is its path prefix (empty for Moscow).
type Region struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (r Region) prefix() string {
	if r.Slug == "" {
		return ""
	}
	return "/" + r.Slug
}

// Regions lists the regions AptekaMos covers.
func Regions() []Region { return append([]Region(nil), regions...) }

var regionAliases = map[string]string{
	"moscow": "", "москва": "", "мск": "", "московская область": "", "подмосковье": "",
	"питер": "spb", "петербург": "spb", "санкт-петербург": "spb", "спб": "spb", "ленинградская область": "spb",
	"екатеринбург": "ekb", "самара": "samara", "казань": "tatarstan", "нижний новгород": "nn", "ростов": "rostov-na-donu",
	"ростов-на-дону": "rostov-na-donu", "краснодар": "krasnodar", "новосибирск": "novosibirsk", "уфа": "bashkortostan",
}

// ResolveRegion accepts a region id, its slug, its name or a common city name; empty means Moscow.
func ResolveRegion(s string) (Region, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return regions[0], nil
	}
	if slug, ok := regionAliases[s]; ok {
		s = slug
		if s == "" {
			return regions[0], nil
		}
	}
	if id, err := strconv.Atoi(s); err == nil {
		for _, r := range regions {
			if r.ID == id {
				return r, nil
			}
		}
		return Region{}, fmt.Errorf("no region with id %d; call reference with kind=regions for the list", id)
	}
	var matches []Region
	for _, r := range regions {
		if r.Slug != "" && r.Slug == s {
			return r, nil
		}
		if strings.ToLower(r.Name) == s {
			return r, nil
		}
		if len([]rune(s)) >= 4 && strings.Contains(strings.ToLower(r.Name), s) {
			matches = append(matches, r)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return Region{}, fmt.Errorf("region %q is ambiguous: %s", s, strings.Join(names, ", "))
	}
	return Region{}, fmt.Errorf("unknown region %q; call reference with kind=regions for the list", s)
}

// Target names a product: an exact drug, or a brand in one dosage form (all of its dosages).
type Target struct {
	DrugID  int `json:"drug_id,omitempty"`
	BrandID int `json:"brand_id,omitempty"`
	FormID  int `json:"form_id,omitempty"`
	Pack    int `json:"pack,omitempty"`
}

func (t Target) validate() error {
	switch {
	case t.DrugID > 0:
		return nil
	case t.BrandID > 0 && t.FormID > 0:
		return nil
	case t.BrandID > 0 || t.FormID > 0:
		return errors.New("a brand needs both brand_id and form_id (or give drug_id)")
	default:
		return errors.New("name the product with drug_id, or with brand_id and form_id")
	}
}

// The site only reads the trailing ids of a path and redirects to the canonical slugs.
func (t Target) pagePath() string {
	if t.DrugID > 0 {
		return fmt.Sprintf("/tovary/lekarstva/x-1/x-%d/ceni", t.DrugID)
	}
	return fmt.Sprintf("/tovary/trd-x-%d/frm-x-%d/kupit", t.BrandID, t.FormID)
}

// label names the product the way the tools take it, for messages.
func (t Target) label() string {
	if t.DrugID > 0 {
		return fmt.Sprintf("drug_id %d", t.DrugID)
	}
	return fmt.Sprintf("brand_id %d with form_id %d", t.BrandID, t.FormID)
}

// notFoundError says which entity is missing and still is ErrNotFound.
type notFoundError struct{ what string }

func (e *notFoundError) Error() string        { return e.what + " not found" }
func (e *notFoundError) Is(target error) bool { return target == ErrNotFound }

// notFound turns the site's 404 into a message about the entity asked for; other errors pass through.
func notFound(err error, what string) error {
	if errors.Is(err, ErrNotFound) {
		return &notFoundError{what}
	}
	return err
}

func (t Target) mapPath() string {
	return t.pagePath() + "/na-karte"
}

// Filters narrow a pharmacy table or map. Ids come from the reference tool and from product cards.
type Filters struct {
	Places        []int   // cities or districts (reference kind=places)
	Metro         []int   // metro stations
	Networks      []int   // pharmacy chains
	Streets       []int   // streets
	InStock       bool    // drop pre-order only offers
	Reserve       bool    // can be reserved
	Delivery      bool    // delivers
	Preorder      bool    // delivery to the pharmacy on order
	Open24h       bool    // round the clock
	Sale          bool    // running a promotion
	Pack          int     // pack size (packs of a product card)
	Manufacturer  int     // manufacturer id (product card)
	Country       int     // country id (product card)
	PriceMin      int     // roubles
	PriceMax      int     // roubles
	PharmacyQuery string  // part of a pharmacy name or address
	Sort          string  // "", "cheap", "expensive", "near", "name"
	Lat, Lng      float64 // for Sort "near"
}

var sortCodes = map[string]string{"cheap": "1", "expensive": "2", "near": "8", "name": "6"}

func joinInts(ids []int) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

func (f Filters) query() (url.Values, error) {
	q := url.Values{}
	for key, ids := range map[string][]int{"r": f.Places, "me": f.Metro, "mn": f.Networks, "st": f.Streets} {
		if len(ids) > 0 {
			q.Set(key, joinInts(ids))
		}
	}
	for key, on := range map[string]bool{"ins": f.InStock, "rsrv": f.Reserve, "deliv": f.Delivery, "preorder": f.Preorder, "duty": f.Open24h, "sale": f.Sale} {
		if on {
			q.Set(key, "1")
		}
	}
	for key, v := range map[string]int{"p": f.Pack, "f": f.Manufacturer, "c": f.Country, "min": f.PriceMin, "max": f.PriceMax} {
		if v > 0 {
			q.Set(key, strconv.Itoa(v))
		}
	}
	if s := strings.TrimSpace(f.PharmacyQuery); s != "" {
		q.Set("q", s)
	}
	if f.Sort != "" {
		code, ok := sortCodes[f.Sort]
		if !ok {
			return nil, fmt.Errorf("unknown sort %q: use cheap, expensive, near or name", f.Sort)
		}
		if f.Sort == "near" {
			if f.Lat == 0 && f.Lng == 0 {
				return nil, errors.New("sort=near needs lat and lng")
			}
			q.Set("llat", strconv.FormatFloat(f.Lat, 'f', -1, 64))
			q.Set("llng", strconv.FormatFloat(f.Lng, 'f', -1, 64))
		}
		q.Set("so", code)
	}
	return q, nil
}

func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

// Service answers questions about AptekaMos by fetching and reading its pages.
type Service struct {
	client *Client
}

func NewService(c *Client) *Service { return &Service{client: c} }

func (s *Service) fetch(ctx context.Context, path string, ttl time.Duration) (*Page, error) {
	p, err := s.client.Get(ctx, path, ttl)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func pageURL(p *Page) string { return siteHost + p.Path }

// ProductList is a list of products with the page it came from. PriceNote warns that the price
// ranges of the products span every pack size and pharmacy.
type ProductList struct {
	Products  []Product `json:"products"`
	PriceNote string    `json:"price_note,omitempty"`
	URL       string    `json:"url"`
}

const productListNote = "price_min..price_max of each product spans all its pack sizes (№1, №4...) and all pharmacies, " +
	"so it is not the price of one pack; drug_card gives the price of each pack."

func (s *Service) productList(ctx context.Context, path string) (ProductList, error) {
	p, err := s.fetch(ctx, path, ttlProducts)
	if err != nil {
		return ProductList{}, err
	}
	doc, err := p.Doc()
	if err != nil {
		return ProductList{}, err
	}
	list := ProductList{Products: parseProducts(doc), URL: pageURL(p)}
	for _, pr := range list.Products {
		if pr.PriceMin > 0 {
			list.PriceNote = productListNote
			break
		}
	}
	return list, nil
}

// Search finds products by name or active substance.
func (s *Service) Search(ctx context.Context, query, region string) (ProductList, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return ProductList{}, errors.New("query is empty")
	}
	r, err := ResolveRegion(region)
	if err != nil {
		return ProductList{}, err
	}
	return s.productList(ctx, r.prefix()+"/tovary?"+url.Values{"q": {query}}.Encode())
}

// Browse lists the products of one active substance (inn), pharmacological group or brand.
func (s *Service) Browse(ctx context.Context, kind string, id int, region string) (ProductList, error) {
	prefix := map[string]string{"inn": "inn", "group": "phg", "brand": "trd"}[kind]
	if prefix == "" {
		return ProductList{}, fmt.Errorf("unknown kind %q: use inn, group or brand", kind)
	}
	if id <= 0 {
		return ProductList{}, errors.New("id must be positive")
	}
	r, err := ResolveRegion(region)
	if err != nil {
		return ProductList{}, err
	}
	l, err := s.productList(ctx, fmt.Sprintf("%s/tovary/%s-x-%d", r.prefix(), prefix, id))
	return l, notFound(err, fmt.Sprintf("%s_id %d", kind, id))
}

// FillIDs adds brand_id and form_id to exact-drug rows that still lack them (nobody sells them, so the list
// has no basket link) by reading the drug's card, which is cached. Call it on the rows that go out, not on a
// whole list. A card that cannot be read leaves its row as it is.
func (s *Service) FillIDs(ctx context.Context, products []Product, region string) []Product {
	out := append([]Product(nil), products...)
	for i := range out {
		p := &out[i]
		if p.DrugID == 0 || (p.BrandID != 0 && p.FormID != 0) {
			continue
		}
		c, err := s.Card(ctx, Target{DrugID: p.DrugID}, region)
		if errors.Is(err, ErrBlocked) || ctx.Err() != nil {
			break
		}
		if err == nil && c.BrandID != 0 && c.FormID != 0 {
			p.BrandID, p.FormID = c.BrandID, c.FormID
		}
	}
	return out
}

func (s *Service) cardPage(ctx context.Context, t Target, region Region, q url.Values) (*Page, error) {
	if err := t.validate(); err != nil {
		return nil, err
	}
	if t.Pack > 0 {
		if q == nil {
			q = url.Values{"p": {strconv.Itoa(t.Pack)}}
		} else if q.Get("p") == "" {
			q = q.Clone()
			q.Set("p", strconv.Itoa(t.Pack))
		}
	}
	p, err := s.fetch(ctx, withQuery(region.prefix()+t.pagePath(), q), ttlOffers)
	return p, notFound(err, t.label())
}

// Card reads what a product is: names, ids, dosages, packs, makers, price range and pharmacy count.
func (s *Service) Card(ctx context.Context, t Target, region string) (Card, error) {
	r, err := ResolveRegion(region)
	if err != nil {
		return Card{}, err
	}
	p, err := s.cardPage(ctx, t, r, nil)
	if err != nil {
		return Card{}, err
	}
	doc, err := p.Doc()
	if err != nil {
		return Card{}, err
	}
	c := parseCard(doc)
	c.URL = siteHost + r.prefix() + t.pagePath() // the short form: the site redirects it to the canonical slug
	if c.DrugID == 0 && t.DrugID > 0 {
		c.DrugID, c.Kind = t.DrugID, KindDrug
	}
	if c.BrandID == 0 && t.BrandID > 0 {
		c.BrandID, c.FormID = t.BrandID, t.FormID
	}
	return c, nil
}

// Listing is a pharmacy table: offers for a product, pharmacies, or pharmacies for a shopping list.
type Listing struct {
	Card      *Card          `json:"product,omitempty"`
	Summary   *BasketSummary `json:"list,omitempty"`
	Total     int            `json:"pharmacies"`
	PriceNote string         `json:"price_note,omitempty"`
	Rows      []Row          `json:"rows"`
	URL       string         `json:"url"`
}

// Offers lists where a product is sold, with filters.
func (s *Service) Offers(ctx context.Context, t Target, f Filters, region string) (Listing, error) {
	r, err := ResolveRegion(region)
	if err != nil {
		return Listing{}, err
	}
	q, err := f.query()
	if err != nil {
		return Listing{}, err
	}
	p, err := s.cardPage(ctx, t, r, q)
	if err != nil {
		return Listing{}, err
	}
	doc, err := p.Doc()
	if err != nil {
		return Listing{}, err
	}
	c := parseCard(doc)
	c.URL = siteHost + r.prefix() + t.pagePath()
	l := Listing{Card: &c, Total: parseListTotal(doc), Rows: parseRows(doc), URL: pageURL(p)}
	if f.Pack == 0 {
		l.PriceNote = mixedPacksNote(c)
	}
	return l, nil
}

// NearestResult is a page of map pharmacies sorted by distance.
type NearestResult struct {
	Total      int           `json:"matched"`
	Pharmacies []MapPharmacy `json:"pharmacies"`
	URL        string        `json:"url"`
}

// Nearest returns pharmacies closest to a point. With a target only those selling it (with its
// lowest price there); without one, all pharmacies matching the filters. radiusKm 0 means no limit.
func (s *Service) Nearest(ctx context.Context, t *Target, lat, lng float64, f Filters, region string, limit int, radiusKm float64) (NearestResult, error) {
	if lat == 0 && lng == 0 {
		return NearestResult{}, errors.New("lat and lng are required")
	}
	r, err := ResolveRegion(region)
	if err != nil {
		return NearestResult{}, err
	}
	f.Sort = ""
	q, err := f.query()
	if err != nil {
		return NearestResult{}, err
	}
	q.Set("pm", "modal")
	path, ttl := r.prefix()+"/magaziny/apteki/na-karte/karta", ttlAllMap
	if t != nil {
		if err := t.validate(); err != nil {
			return NearestResult{}, err
		}
		path, ttl = r.prefix()+t.mapPath(), ttlOffers
	}
	res, err := s.nearestFrom(ctx, withQuery(path, q), ttl, lat, lng, limit, radiusKm)
	if t != nil {
		err = notFound(err, t.label())
	}
	return res, err
}

// dropParam removes key=val from the query of a path without touching the rest of it.
func dropParam(path, key, val string) string {
	base, query, ok := strings.Cut(path, "?")
	if !ok {
		return path
	}
	var keep []string
	for _, kv := range strings.Split(query, "&") {
		if kv != "" && kv != key+"="+val {
			keep = append(keep, kv)
		}
	}
	if len(keep) == 0 {
		return base
	}
	return base + "?" + strings.Join(keep, "&")
}

func (s *Service) nearestFrom(ctx context.Context, path string, ttl time.Duration, lat, lng float64, limit int, radiusKm float64) (NearestResult, error) {
	p, err := s.fetch(ctx, path, ttl)
	if err != nil {
		return NearestResult{}, err
	}
	doc, err := p.Doc()
	if err != nil {
		return NearestResult{}, err
	}
	all, err := parseMapPharmacies(doc)
	if err != nil {
		return NearestResult{}, err
	}
	res := NearestResult{Total: len(all), URL: siteHost + dropParam(p.Path, "pm", "modal")}
	for i := range all {
		all[i].DistanceKm = float64(int(distanceKm(lat, lng, all[i].Lat, all[i].Lng)*100+0.5)) / 100
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].DistanceKm < all[j].DistanceKm })
	if limit <= 0 {
		limit = maxListLength
	}
	for _, m := range all {
		if radiusKm > 0 && m.DistanceKm > radiusKm {
			break
		}
		if len(res.Pharmacies) == limit {
			break
		}
		res.Pharmacies = append(res.Pharmacies, m)
	}
	if res.Pharmacies == nil {
		res.Pharmacies = []MapPharmacy{}
	}
	return res, nil
}

// Offer is what one pharmacy has of a product: the exact packs and prices.
type Offer struct {
	Pharmacy Pharmacy    `json:"pharmacy"`
	Items    []PriceItem `json:"items"`
	URL      string      `json:"url"`
}

// OfferAt shows the exact packs and prices of a product in one pharmacy.
func (s *Service) OfferAt(ctx context.Context, pharmacyID int, t Target, region string) (Offer, error) {
	if pharmacyID <= 0 {
		return Offer{}, errors.New("pharmacy_id must be positive")
	}
	if err := t.validate(); err != nil {
		return Offer{}, err
	}
	r, err := ResolveRegion(region)
	if err != nil {
		return Offer{}, err
	}
	var path string
	if t.DrugID > 0 {
		brand, form := t.BrandID, t.FormID
		if brand == 0 || form == 0 {
			c, err := s.Card(ctx, Target{DrugID: t.DrugID}, region)
			if err != nil {
				return Offer{}, err
			}
			brand, form = c.BrandID, c.FormID
		}
		if brand == 0 || form == 0 {
			return Offer{}, errors.New("could not tell this drug's brand and form; pass brand_id and form_id too")
		}
		path = fmt.Sprintf("%s/tovary/trd-x-%d/frm-x-%d/med-x-%d/kupit/org-x-%d", r.prefix(), brand, form, t.DrugID, pharmacyID)
	} else {
		path = fmt.Sprintf("%s/tovary/trd-x-%d/frm-x-%d/kupit/org-x-%d", r.prefix(), t.BrandID, t.FormID, pharmacyID)
	}
	p, err := s.fetch(ctx, path, ttlOffers)
	if err != nil {
		// the page is missing for an unknown pharmacy as well as for a product it has no page of
		return Offer{}, notFound(err, fmt.Sprintf("pharmacy_id %d or %s", pharmacyID, t.label()))
	}
	doc, err := p.Doc()
	if err != nil {
		return Offer{}, err
	}
	o := Offer{Pharmacy: parsePharmacy(doc), Items: parseOfferItems(doc), URL: pageURL(p)}
	if o.Pharmacy.ID == 0 {
		o.Pharmacy.ID = pharmacyID
	}
	o.Pharmacy.URL = siteHost + fmt.Sprintf("/magaziny/org-x-%d", o.Pharmacy.ID)
	if o.Items == nil {
		o.Items = []PriceItem{}
	}
	if t.DrugID > 0 {
		for i := range o.Items {
			if o.Items[i].DrugID == 0 {
				o.Items[i].DrugID = t.DrugID
			}
		}
	}
	return o, nil
}

// Pharmacy reads a pharmacy's card, its order terms when it takes orders, and its map position.
func (s *Service) Pharmacy(ctx context.Context, id int) (Pharmacy, error) {
	if id <= 0 {
		return Pharmacy{}, errors.New("pharmacy_id must be positive")
	}
	p, err := s.fetch(ctx, fmt.Sprintf("/magaziny/org-x-%d", id), ttlPharmacy)
	if err != nil {
		return Pharmacy{}, notFound(err, fmt.Sprintf("pharmacy_id %d", id))
	}
	doc, err := p.Doc()
	if err != nil {
		return Pharmacy{}, err
	}
	ph := parsePharmacy(doc)
	ph.URL = siteHost + fmt.Sprintf("/magaziny/org-x-%d", id)
	if ph.ID == 0 {
		ph.ID = id
	}
	if ph.TakesOrders {
		if cp, err := s.fetch(ctx, fmt.Sprintf("/magaziny/org-x-%d/usloviya-zakaza?pm=modal", id), ttlReference); err == nil {
			if cd, err := cp.Doc(); err == nil {
				ph.OrderConditions = parseOrderConditions(cd)
			}
		}
	}
	// The position is extra: a failure there must not lose the card.
	if mp, err := s.fetch(ctx, fmt.Sprintf("/magaziny/apteki/na-karte/karta?o=%d&pm=modal", id), ttlPharmacy); err == nil {
		if md, err := mp.Doc(); err == nil {
			if pins, err := parseMapPharmacies(md); err == nil {
				for _, pin := range pins {
					if pin.ID == id {
						ph.Lat, ph.Lng, ph.Open24h = pin.Lat, pin.Lng, pin.Open24h
					}
				}
			}
		}
	}
	return ph, nil
}

// PriceListResult is a search in one pharmacy's price list.
type PriceListResult struct {
	PharmacyID int         `json:"pharmacy_id"`
	Items      []PriceItem `json:"items"`
	URL        string      `json:"url"`
}

// PriceList searches a pharmacy's whole price list by product name.
func (s *Service) PriceList(ctx context.Context, pharmacyID int, query string) (PriceListResult, error) {
	if pharmacyID <= 0 {
		return PriceListResult{}, errors.New("pharmacy_id must be positive")
	}
	q := url.Values{"pm": {"modal"}}
	if query = strings.TrimSpace(query); query != "" {
		q.Set("q", query)
	}
	p, err := s.fetch(ctx, fmt.Sprintf("/magaziny/org-x-%d/price-list?%s", pharmacyID, q.Encode()), ttlOffers)
	if err != nil {
		return PriceListResult{}, notFound(err, fmt.Sprintf("pharmacy_id %d", pharmacyID))
	}
	doc, err := p.Doc()
	if err != nil {
		return PriceListResult{}, err
	}
	res := PriceListResult{PharmacyID: pharmacyID, Items: parsePriceList(doc), URL: siteHost + fmt.Sprintf("/magaziny/org-x-%d", pharmacyID)}
	if res.Items == nil {
		res.Items = []PriceItem{}
	}
	return res, nil
}

// Pharmacies lists pharmacies with filters (no product).
func (s *Service) Pharmacies(ctx context.Context, f Filters, region string) (Listing, error) {
	r, err := ResolveRegion(region)
	if err != nil {
		return Listing{}, err
	}
	q, err := f.query()
	if err != nil {
		return Listing{}, err
	}
	p, err := s.fetch(ctx, withQuery(r.prefix()+"/magaziny/apteki/na-karte", q), ttlPharmacy)
	if err != nil {
		return Listing{}, err
	}
	doc, err := p.Doc()
	if err != nil {
		return Listing{}, err
	}
	return Listing{Total: parseListTotal(doc), Rows: parseRows(doc), URL: pageURL(p)}, nil
}

// Substitutes lists products with the same active substance (other brands and forms).
func (s *Service) Substitutes(ctx context.Context, t Target, region string) (ProductList, error) {
	c, err := s.Card(ctx, t, region)
	if err != nil {
		return ProductList{}, err
	}
	if c.substitutesPath == "" {
		return ProductList{}, errors.New("AptekaMos shows no substitutes for this product")
	}
	r, _ := ResolveRegion(region)
	path := c.substitutesPath
	if !strings.HasPrefix(path, r.prefix()+"/") {
		path = r.prefix() + path
	}
	l, err := s.productList(ctx, path)
	l.Products = withoutSource(l.Products, t, c)
	return l, err
}

// withoutSource drops the product a substitutes list was asked for: the row of the drug itself and the
// tradeform row of its brand and form, or, for a brand in a form, every row of it.
func withoutSource(items []Product, t Target, c Card) []Product {
	out := make([]Product, 0, len(items))
	for _, p := range items {
		sameForm := c.BrandID != 0 && c.FormID != 0 && p.BrandID == c.BrandID && p.FormID == c.FormID
		if (t.DrugID > 0 && p.DrugID == t.DrugID) || (sameForm && (t.DrugID == 0 || p.DrugID == 0)) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ShoppingList finds pharmacies that have every product of a list, with the total price there.
func (s *Service) ShoppingList(ctx context.Context, items []Target, f Filters, region string) (Listing, error) {
	if len(items) == 0 {
		return Listing{}, errors.New("the list is empty")
	}
	r, err := ResolveRegion(region)
	if err != nil {
		return Listing{}, err
	}
	keys := make([]string, 0, len(items))
	for _, t := range items {
		c, err := s.Card(ctx, t, region)
		if err != nil {
			return Listing{}, fmt.Errorf("product %+v: %w", t, err)
		}
		if c.productKey == "" {
			return Listing{}, fmt.Errorf("product %+v: AptekaMos offers no way to add it to a list", t)
		}
		keys = append(keys, c.productKey)
	}
	q, err := f.query()
	if err != nil {
		return Listing{}, err
	}
	q.Del("p")
	q.Set("aid", "bsw")
	q.Set("productids", strings.Join(keys, ","))
	p, err := s.fetch(ctx, withQuery(r.prefix()+"/korzina", q), ttlOffers)
	if err != nil {
		return Listing{}, err
	}
	doc, err := p.Doc()
	if err != nil {
		return Listing{}, err
	}
	sum := parseBasketSummary(doc)
	return Listing{Summary: &sum, Total: sum.Pharmacies, Rows: parseRows(doc), URL: pageURL(p)}, nil
}

// Instruction reads a drug's leaflet.
func (s *Service) Instruction(ctx context.Context, drugID int) (Instruction, error) {
	if drugID <= 0 {
		return Instruction{}, errors.New("drug_id must be positive")
	}
	p, err := s.fetch(ctx, fmt.Sprintf("/tovary/lekarstva/x-1/x-%d/instrukciya", drugID), ttlLeaflet)
	if err != nil {
		return Instruction{}, notFound(err, fmt.Sprintf("drug_id %d", drugID))
	}
	doc, err := p.Doc()
	if err != nil {
		return Instruction{}, err
	}
	ins := parseInstruction(doc)
	ins.DrugID = drugID
	ins.URL = pageURL(p)
	if len(ins.Sections) == 0 {
		return ins, errors.New("AptekaMos has no leaflet for this product")
	}
	return ins, nil
}

// Places lists the cities of a region, with districts where the site has them.
func (s *Service) Places(ctx context.Context, region string) ([]Place, error) {
	r, err := ResolveRegion(region)
	if err != nil {
		return nil, err
	}
	p, err := s.fetch(ctx, "/modal/regiony?pm=modal", ttlReference)
	if err != nil {
		return nil, err
	}
	doc, err := p.Doc()
	if err != nil {
		return nil, err
	}
	for _, reg := range parseRegionTree(doc) {
		if reg.ID == r.ID {
			return reg.Places, nil
		}
	}
	return nil, fmt.Errorf("the location filter has no %s", r.Name)
}

func (s *Service) filterList(ctx context.Context, region, modal, itemClass, query string) ([]Option, error) {
	r, err := ResolveRegion(region)
	if err != nil {
		return nil, err
	}
	p, err := s.fetch(ctx, r.prefix()+"/modal/"+modal+"?pm=modal", ttlReference)
	if err != nil {
		return nil, err
	}
	doc, err := p.Doc()
	if err != nil {
		return nil, err
	}
	all := parseFilterList(doc, itemClass)
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		if all == nil {
			all = []Option{}
		}
		return all, nil
	}
	out := []Option{}
	for _, o := range all {
		if strings.Contains(strings.ToLower(o.Name), query) {
			out = append(out, o)
		}
	}
	return out, nil
}

// Metro lists metro stations of a region (where it has a metro).
func (s *Service) Metro(ctx context.Context, region, query string) ([]Option, error) {
	return s.filterList(ctx, region, "metro", ".am-pc-metro", query)
}

// Networks lists the pharmacy chains of a region.
func (s *Service) Networks(ctx context.Context, region, query string) ([]Option, error) {
	return s.filterList(ctx, region, "nets", ".am-pc-net", query)
}

// Streets lists the streets of a region that have pharmacies.
func (s *Service) Streets(ctx context.Context, region, query string) ([]Option, error) {
	return s.filterList(ctx, region, "streets", ".am-pc-street", query)
}

// mixedPacksNote says why the prices of a table can not be compared: the rows of a product page
// cover every pack size (and every dosage, for a brand in one form) unless a pack is filtered.
func mixedPacksNote(c Card) string {
	var what []string
	var packs []string
	for _, p := range c.Packs {
		packs = append(packs, fmt.Sprintf("%s (pack=%d)", p.Name, p.ID))
	}
	if len(packs) > 1 {
		what = append(what, "pack sizes "+strings.Join(packs, ", "))
	}
	if c.Kind == KindTradeform && len(c.Dosages) > 0 {
		what = append(what, fmt.Sprintf("%d dosages", len(c.Dosages)+1))
	}
	if len(what) == 0 {
		return ""
	}
	return "Prices in the rows cover several " + strings.Join(what, " and ") +
		"; pass pack=<id> to see one pack size, or use price_at_pharmacy for the exact items of a pharmacy."
}

// CardWithPacks is Card plus, for an exact drug, the pharmacy count and prices of each pack size,
// which cost one extra page per pack. A brand in one form mixes dosages, so its packs stay unpriced.
func (s *Service) CardWithPacks(ctx context.Context, t Target, region string) (Card, error) {
	c, err := s.Card(ctx, t, region)
	if err != nil || t.DrugID <= 0 || len(c.Packs) == 0 {
		return c, err
	}
	r, err := ResolveRegion(region)
	if err != nil {
		return Card{}, err
	}
	for i := range c.Packs {
		pk := &c.Packs[i]
		if len(c.Packs) == 1 {
			pk.Pharmacies, pk.PriceMin, pk.PriceMax = c.Pharmacies, c.PriceMin, c.PriceMax
		} else {
			p, err := s.cardPage(ctx, t, r, url.Values{"p": {strconv.Itoa(pk.ID)}})
			if err != nil {
				return Card{}, fmt.Errorf("pack %s: %w", pk.Name, err)
			}
			doc, err := p.Doc()
			if err != nil {
				return Card{}, err
			}
			pc := parseCard(doc)
			pk.Pharmacies, pk.PriceMin, pk.PriceMax = pc.Pharmacies, pc.PriceMin, pc.PriceMax
		}
		pk.UnitPriceMin = unitPrice(pk.PriceMin, pk.Units)
	}
	return c, nil
}
