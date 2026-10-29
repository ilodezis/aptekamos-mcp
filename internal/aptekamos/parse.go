package aptekamos

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func text(s *goquery.Selection) string { return cleanText(s.Text()) }

func attrLink(s *goquery.Selection, names ...string) string {
	for _, n := range names {
		if v, ok := s.Attr(n); ok && v != "" {
			return decodeLink(v)
		}
	}
	return ""
}

// parseProducts reads the product lists of search, browse (INN, group, brand) and substitutes pages.
func parseProducts(doc *goquery.Document) []Product {
	var out []Product
	doc.Find(".am-pc-product-type-2-c").Each(func(_ int, s *goquery.Selection) {
		name := s.Find(".am-pc-product-name").First()
		href := attrLink(name, "href")
		p := Product{
			Name:   text(name),
			INN:    text(s.Find(".am-pc-product-inter-val").First()),
			Group:  text(s.Find(".am-pc-product-phg").First()),
			Status: text(s.Find(".am-pc-product-not-in-retail").First()),
			URL:    siteHost + href,
		}
		p.Available = p.Status == ""
		p.Pharmacies = parseCount(text(s.Find(".am-pc-product-orgs-count").First()))
		p.PriceMin, p.PriceMax = parsePriceRange(text(s.Find(".am-pc-product-prices-val").First()))
		p.INNID = segmentID(attrLink(s.Find(".am-pc-product-synonyms").First(), "data-go-to-link", "href"), "inn")
		p.GroupID = segmentID(attrLink(s.Find(".am-pc-product-analogs").First(), "data-go-to-link", "href"), "phg")

		basket := attrLink(s.Find(".am-pc-product-basket-btn").First(), "data-go-to-link")
		p.BrandID = segmentID(basket, "trd")
		p.FormID = segmentID(basket, "frm")
		p.DrugID = drugIDFromPath(href)
		if p.DrugID == 0 {
			p.DrugID = segmentID(basket, "med")
		}
		if p.BrandID == 0 {
			p.BrandID, p.FormID = segmentID(href, "trd"), segmentID(href, "frm")
		}
		if p.DrugID != 0 {
			p.Kind = KindDrug
		} else {
			p.Kind = KindTradeform
		}
		out = append(out, p)
	})
	inheritIDs(out)
	return out
}

// namePrefix is a product name up to its first token with a digit: brand and dosage form
// ("Тирвелла раствор для инъекций 10мг 0,5мл" -> "Тирвелла раствор для инъекций").
func namePrefix(name string) string {
	var head []string
	for _, w := range strings.Fields(name) {
		if strings.ContainsAny(w, "0123456789") {
			break
		}
		head = append(head, w)
	}
	return strings.Join(head, " ")
}

// inheritIDs gives rows without a basket link (nobody sells them, so the button is dead) the brand_id and
// form_id of the rows with the same name prefix. A prefix that leads to several (brand, form) pairs is
// ambiguous and stays unfilled.
func inheritIDs(items []Product) {
	type pair struct{ brand, form int }
	known := map[string][]pair{}
	for _, p := range items {
		prefix := namePrefix(p.Name)
		if prefix == "" || p.BrandID == 0 || p.FormID == 0 || slices.Contains(known[prefix], pair{p.BrandID, p.FormID}) {
			continue
		}
		known[prefix] = append(known[prefix], pair{p.BrandID, p.FormID})
	}
	for i := range items {
		p := &items[i]
		if p.DrugID == 0 || (p.BrandID != 0 && p.FormID != 0) {
			continue
		}
		if ids := known[namePrefix(p.Name)]; len(ids) == 1 {
			p.BrandID, p.FormID = ids[0].brand, ids[0].form
		}
	}
}

// parseCard reads the product header of a drug (/ceni) or tradeform (/kupit) page.
func parseCard(doc *goquery.Document) Card {
	block := doc.Find("#am-pc-products-fragment .am-pc-product").First()
	if block.Length() == 0 {
		block = doc.Selection
	}
	c := Card{
		Name:    text(block.Find(".am-pc-product-name").First()),
		INN:     text(block.Find(".am-pc-product-inter-val").First()),
		Group:   text(block.Find(".am-pc-product-phg").First()),
		GroupID: segmentID(attrLink(doc.Find(".am-pc-product-analogs, .am-pc-product-analogs-btn").First(), "data-go-to-link", "href"), "phg"),
	}

	replace := attrLink(doc.Find(".am-pc-product-replace").First(), "data-go-to-link")
	if replace != "" {
		c.INNID = segmentID(replace, "inn")
		c.BrandID = segmentID(replace, "trd")
		c.FormID = segmentID(replace, "frm")
		c.DrugID = segmentID(replace, "med")
		path, _, _ := strings.Cut(replace, "?")
		c.substitutesPath = path + "?pm=modal"
	}
	if c.DrugID == 0 && c.BrandID == 0 {
		canon, _ := doc.Find(`link[rel="canonical"]`).Attr("href")
		canon = decodeLink(canon)
		c.DrugID = drugIDFromPath(canon)
		c.BrandID, c.FormID = segmentID(canon, "trd"), segmentID(canon, "frm")
	}
	if c.DrugID != 0 {
		c.Kind = KindDrug
	} else {
		c.Kind = KindTradeform
	}

	c.Pharmacies = parseListTotal(doc)
	if v := doc.Find("#am-pc-add-product-prices-min-val"); v.Length() > 0 {
		c.PriceMin, _ = parsePriceRange(text(v))
		c.PriceMax = maxOrMin(doc, c.PriceMin)
	} else {
		c.PriceMin, c.PriceMax = parsePriceRange(text(block.Find(".am-pc-product-prices").First()))
	}

	block.Find("select.am-com-selector option").Each(func(i int, o *goquery.Selection) {
		if _, sel := o.Attr("selected"); sel {
			return
		}
		link := attrLink(o, "value")
		opt := Option{Name: text(o)}
		switch {
		case queryInt(link, "p") != 0:
			opt.ID = queryInt(link, "p")
			c.Packs = append(c.Packs, Pack{Name: opt.Name, ID: opt.ID, Units: packUnits(opt.Name, opt.ID)})
		case queryInt(link, "c") != 0:
			opt.ID = queryInt(link, "c")
			c.Countries = append(c.Countries, opt)
		case queryInt(link, "f") != 0:
			opt.ID = queryInt(link, "f")
			c.Manufacturers = append(c.Manufacturers, opt)
		case drugIDFromPath(link) != 0 && drugIDFromPath(link) != c.DrugID:
			opt.ID = drugIDFromPath(link)
			c.Dosages = append(c.Dosages, opt)
		}
	})

	doc.Find("[data-go-to-link]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		link := attrLink(s, "data-go-to-link")
		if !strings.Contains(link, "aid=sisd") || !strings.Contains(link, "productids=") {
			return true
		}
		_, q, _ := strings.Cut(link, "?")
		if vals, err := url.ParseQuery(q); err == nil && vals.Get("productids") != "" {
			c.productKey = vals.Get("productids")
			return false
		}
		return true
	})
	return c
}

// parseListTotal reads how many pharmacies a table page covers.
func parseListTotal(doc *goquery.Document) int {
	for _, sel := range []string{"#am-pc-orgs-count", ".am-pc-orgs-count", "#am-pc-add-product-orgs-count-val", "#am-pc-products-fragment .am-pc-product-orgs-count"} {
		if s := doc.Find(sel).First(); s.Length() > 0 {
			return parseCount(text(s))
		}
	}
	return 0
}

var featureClasses = []struct{ class, feature string }{
	{"am-pc-inretail-filter", FeatureInStock},
	{"am-pc-reserve-filter", FeatureReserve},
	{"am-pc-delivery-filter", FeatureDelivery},
	{"am-pc-fullday-filter", Feature24h},
	{"am-pc-sale-filter", FeatureSale},
}

// parseRows reads a pharmacy table: offers of a product, the pharmacy list or the shopping list.
func parseRows(doc *goquery.Document) []Row {
	var out []Row
	doc.Find("#am-pc-orgs-fragment .am-pc-org-c > .am-pc-org").Each(func(_ int, s *goquery.Selection) {
		r := Row{
			Name:    text(s.Find(".am-pc-org-name").First()),
			Address: text(s.Find(".am-pc-org-addr").First()),
			Metro:   strings.TrimPrefix(text(s.Find(".am-pc-org-metro").First()), "м. "),
		}
		mapLink := attrLink(s.Find(".am-pc-org-map").First(), "data-link")
		if orgs := s.Find(".am-pc-org-btn-orgs").First(); orgs.Length() > 0 {
			r.Kind = RowNetwork
			r.NetworkID = queryInt(attrLink(orgs, "data-go-to-link"), "mn")
			if r.NetworkID == 0 {
				r.NetworkID = queryInt(mapLink, "mn")
			}
			r.Pharmacies = parseCount(text(orgs))
			r.Phones = splitPhones(text(s.Find(".am-pc-org-phone-text").First()))
		} else {
			r.Kind = RowPharmacy
			r.PharmacyID = queryInt(mapLink, "o")
			if r.PharmacyID == 0 {
				r.PharmacyID = segmentID(attrLink(s.Find(".am-pc-org-name").First(), "data-go-to-link", "href"), "org")
			}
			r.ID = r.PharmacyID
			s.Find("a.am-pc-org-phone").Each(func(_ int, a *goquery.Selection) {
				if p := text(a); p != "" {
					r.Phones = append(r.Phones, p)
				}
			})
		}
		r.PriceMin, r.PriceMax = parsePriceRange(text(s.Find(".am-pc-org-prices").First()))
		r.PriceNote = text(s.Find(".am-pc-org-prices-descr").First())
		for _, fc := range featureClasses {
			f := s.Find("." + fc.class).First()
			if f.Length() == 0 {
				continue
			}
			r.Features = append(r.Features, fc.feature)
			if r.Kind == RowNetwork {
				if n := parseCount(text(f.Find(".am-pc-oval-count"))); n > 0 {
					if r.FeatureCounts == nil {
						r.FeatureCounts = map[string]int{}
					}
					r.FeatureCounts[fc.feature] = n
				}
			}
		}
		if s.Find(".am-pc-org-btn-order").Length() > 0 {
			r.Features = append(r.Features, FeatureOnlineOrder)
		}
		out = append(out, r)
	})
	return out
}

func parseBasketSummary(doc *goquery.Document) BasketSummary {
	s := BasketSummary{
		Items:      parseCount(text(doc.Find("#am-pc-add-product-comment-count"))),
		Pharmacies: parseCount(text(doc.Find("#am-pc-add-product-orgs-count-val"))),
	}
	s.PriceMin, _ = parsePriceRange(text(doc.Find("#am-pc-add-product-prices-min-val")))
	s.PriceMax = maxOrMin(doc, s.PriceMin)
	return s
}

// parseInstruction reads the leaflet page (/instrukciya).
func parseInstruction(doc *goquery.Document) Instruction {
	ins := Instruction{
		Name:    text(doc.Find(".am-pc-product-name").First()),
		INN:     text(doc.Find(".am-pc-product-inter-val").First()),
		Group:   text(doc.Find(".am-pc-product-phg").First()),
		GroupID: segmentID(attrLink(doc.Find(".am-pc-product-analogs, .am-pc-product-analogs-btn").First(), "data-go-to-link", "href"), "phg"),
	}
	doc.Find(".am-pc-medicine-atc").Each(func(_ int, s *goquery.Selection) { ins.ATC = append(ins.ATC, text(s)) })
	doc.Find(".am-pc-medicine-mkb").Each(func(_ int, s *goquery.Selection) { ins.ICD10 = append(ins.ICD10, text(s)) })
	doc.Find(".am-pc-medicine-sick").Each(func(_ int, s *goquery.Selection) { ins.Conditions = append(ins.Conditions, text(s)) })
	doc.Find("a.am-pc-medicine-source-link").Each(func(_ int, s *goquery.Selection) {
		src := text(s)
		if href, ok := s.Attr("href"); ok && href != "" {
			src += ": " + href
		}
		ins.Sources = append(ins.Sources, src)
	})
	doc.Find(".am-pc-medicine-group").Each(func(_ int, g *goquery.Selection) {
		if g.Find(".am-pc-medicine-group").Length() > 0 ||
			g.Find(".am-pc-medicine-group-list, .am-pc-medicine-source-c, .am-pc-medicine-atc, .am-pc-medicine-mkb, .am-pc-medicine-sick").Length() > 0 {
			return
		}
		title := text(g.Find(".am-pc-medicine-group-h").First())
		var parts []string
		g.Find(".am-pc-medicine-group-d").Each(func(_ int, d *goquery.Selection) {
			if t := text(d); t != "" {
				parts = append(parts, t)
			}
		})
		if title != "" && len(parts) > 0 {
			ins.Sections = append(ins.Sections, Section{Title: title, Text: strings.Join(parts, "\n")})
		}
	})
	return ins
}

// parsePharmacy reads a pharmacy card; offer pages (/kupit/org-…) carry the same header.
func parsePharmacy(doc *goquery.Document) Pharmacy {
	p := Pharmacy{
		Row: Row{
			Name:       text(doc.Find("#am-pc-org-name")),
			Address:    text(doc.Find("#am-pc-org-addr-txt")),
			Metro:      text(doc.Find("#am-pc-org-metro-txt")),
			OpenStatus: text(doc.Find("#am-pc-org-open-time")),
		},
		Directions:      text(doc.Find("#am-pc-org-path-txt")),
		PriceListDate:   formatISODate(text(doc.Find("#am-pc-org-pl-info-last-updated-val"))),
		PriceListItems:  parseCount(text(doc.Find("#am-pc-org-pl-info-count-val"))),
		OrderConditions: parseOrderConditions(doc),
	}
	id := segmentID(attrLink(doc.Find(".am-pc-org-fav").First(), "data-link"), "org")
	if id == 0 {
		id = queryInt(attrLink(doc.Find("#am-pc-org-map").First(), "data-link"), "o")
	}
	p.PharmacyID = id
	p.ID = id
	open := doc.Find("#am-pc-org-open-time")
	p.OpenStatus = text(open)
	if class, _ := open.Attr("class"); p.OpenStatus != "" {
		switch {
		case strings.Contains(class, "closed"):
			v := false
			p.OpenNow = &v
		case strings.Contains(class, "open"):
			v := true
			p.OpenNow = &v
		}
	}
	doc.Find(".am-pc-org-timelist").Each(func(_ int, s *goquery.Selection) {
		if t := text(s); t != "" {
			p.Hours = append(p.Hours, t)
		}
	})
	seen := map[string]bool{}
	doc.Find("a.am-pc-org-phone").Each(func(_ int, a *goquery.Selection) {
		if t := text(a); t != "" && !seen[t] {
			seen[t] = true
			p.Phones = append(p.Phones, t)
		}
	})
	if section := doc.Find("#am-pc-org-order-section-header"); section.Length() > 0 {
		p.TakesOrders = true
		section.Parent().Find(".am-pc-checkbox-label").Each(func(_ int, s *goquery.Selection) {
			if t := text(s); t != "" {
				p.OrderOptions = append(p.OrderOptions, t)
			}
		})
	}
	if p.Open24h || strings.Contains(strings.ToLower(p.OpenStatus), "круглосуточно") || strings.Contains(strings.ToLower(strings.Join(p.Hours, " ")), "круглосуточно") {
		p.Open24h = true
		if !slices.Contains(p.Features, Feature24h) {
			p.Features = append(p.Features, Feature24h)
		}
	}
	if p.TakesOrders {
		if !slices.Contains(p.Features, FeatureOnlineOrder) {
			p.Features = append(p.Features, FeatureOnlineOrder)
		}
	}
	for _, opt := range p.OrderOptions {
		optL := strings.ToLower(opt)
		if strings.Contains(optL, "бронир") || strings.Contains(optL, "резерв") {
			if !slices.Contains(p.Features, FeatureReserve) {
				p.Features = append(p.Features, FeatureReserve)
			}
		}
		if strings.Contains(optL, "доставк") {
			if !slices.Contains(p.Features, FeatureDelivery) {
				p.Features = append(p.Features, FeatureDelivery)
			}
		}
	}
	return p
}

func parseOrderConditions(doc *goquery.Document) string {
	return text(doc.Find("#am-pc-order-conditions-c"))
}

func zipPrices(names, prices *goquery.Selection) []offerRow {
	var out []offerRow
	for i := 0; i < names.Length() && i < prices.Length(); i++ {
		price, _ := parsePriceRange(text(prices.Eq(i)))
		out = append(out, offerRow{PriceItem: PriceItem{Name: text(names.Eq(i)), Price: price}})
	}
	return out
}

var drugImgRe = regexp.MustCompile(`(?i)drug0*(\d+)(?:[-._]|$)`)

func drugIDFromImg(s *goquery.Selection) int {
	img := s.Find(".am-pc-org-product-img").First()
	if img.Length() == 0 {
		return 0
	}
	for _, attr := range []string{"data-src-origin", "src"} {
		if val, ok := img.Attr(attr); ok {
			if m := drugImgRe.FindStringSubmatch(val); len(m) > 1 {
				id, _ := strconv.Atoi(m[1])
				return id
			}
		}
	}
	return 0
}

// parseOfferItems reads the exact packs and prices of an offer page (/kupit/org-…), one item per pack.
func parseOfferItems(doc *goquery.Document) []PriceItem {
	var rows []offerRow
	doc.Find("tbody.am-pc-org-product-c").Each(func(_ int, s *goquery.Selection) {
		name := text(s.Find(".am-pc-org-product-name").First())
		if name == "" {
			return
		}
		price, _ := parsePriceRange(text(s.Find(".am-pc-org-product-price-val").First()))
		id, _ := s.Attr("data-product-id")
		drugID := drugIDFromImg(s)
		rows = append(rows, offerRow{PriceItem{Name: name, Price: price, DrugID: drugID}, id})
	})
	if len(rows) == 0 {
		rows = zipPrices(doc.Find(".am-pc-org-product-name"), doc.Find(".am-pc-org-product-price-val"))
	}
	return groupItems(rows)
}

// dedupePriceItems removes duplicate rows that share the same product name, drug_id, and price.
func dedupePriceItems(items []PriceItem) []PriceItem {
	type key struct {
		drugID int
		name   string
		price  float64
	}
	seen := make(map[key]bool, len(items))
	out := make([]PriceItem, 0, len(items))
	for _, it := range items {
		k := key{drugID: it.DrugID, name: it.Name, price: it.Price}
		if !seen[k] {
			seen[k] = true
			out = append(out, it)
		}
	}
	return out
}

// parsePriceList reads a pharmacy's price list search (/price-list?q=…).
func parsePriceList(doc *goquery.Document) []PriceItem {
	var rows []offerRow
	doc.Find(".am-pc-org-pl-product").Each(func(_ int, s *goquery.Selection) {
		name := text(s.Find(".am-pc-org-pl-product-name").First())
		if name == "" {
			return
		}
		price, _ := parsePriceRange(text(s.Find(".am-pc-org-pl-product-price").First()))
		id, _ := s.Attr("data-product-id")
		drugID := drugIDFromImg(s)
		rows = append(rows, offerRow{PriceItem{Name: name, Price: price, DrugID: drugID}, id})
	})
	if len(rows) == 0 {
		rows = zipPrices(doc.Find(".am-pc-org-pl-product-name"), doc.Find(".am-pc-org-pl-product-price"))
	}
	return dedupePriceItems(groupItems(rows))
}

type mapOrg struct {
	ID          int     `json:"orgId"`
	Lat         float64 `json:"orgLat"`
	Lng         float64 `json:"orgLng"`
	Name        string  `json:"orgName"`
	Address     string  `json:"orgAddress"`
	Path        string  `json:"orgPath"`
	Metro       string  `json:"orgMetro"`
	Phones      string  `json:"orgPhones"`
	FullDay     int     `json:"isFullDay"`
	OpenTime    string  `json:"openTime"`
	SendDate    string  `json:"orgSendDate"`
	HasOrder    int     `json:"hasOrder"`
	MinPriceStr string  `json:"minPriceStr"`
}

// parseMapPharmacies reads the JSON the map modal keeps in data-orgs.
func parseMapPharmacies(doc *goquery.Document) ([]MapPharmacy, error) {
	raw, ok := doc.Find("[data-orgs]").First().Attr("data-orgs")
	if !ok {
		return nil, nil
	}
	var orgs []mapOrg
	if err := json.Unmarshal([]byte(raw), &orgs); err != nil {
		return nil, fmt.Errorf("map data in an unexpected shape: %w", err)
	}
	out := make([]MapPharmacy, 0, len(orgs))
	for _, o := range orgs {
		status := cleanText(o.OpenTime)
		p := MapPharmacy{
			PharmacyID: o.ID, ID: o.ID, Name: cleanText(o.Name), Address: cleanText(o.Address), Metro: cleanText(o.Metro),
			Phones: splitPhones(o.Phones), Lat: o.Lat, Lng: o.Lng,
			OpenStatus: status,
		}
		if o.FullDay == 1 || strings.Contains(strings.ToLower(status), "круглосуточно") {
			p.Features = append(p.Features, Feature24h)
		}
		if o.HasOrder == 1 {
			p.Features = append(p.Features, FeatureOnlineOrder)
		}
		p.PriceMin, _ = parsePriceRange(o.MinPriceStr)
		out = append(out, p)
	}
	return out, nil
}

func dataID(s *goquery.Selection) int {
	v, _ := s.Attr("data-id")
	id, _ := strconv.Atoi(v)
	return id
}

// parseRegionTree reads the region → city → district tree of the location filter.
func parseRegionTree(doc *goquery.Document) []Place {
	var regions []Place
	doc.Find(".am-pc-region").Each(func(_ int, r *goquery.Selection) {
		region := Place{
			ID:   dataID(r.Find(".am-pc-region-filter-node").First()),
			Name: text(r.Find(".am-pc-region-name").First()),
		}
		r.Find(".am-pc-city").Each(func(_ int, c *goquery.Selection) {
			city := Place{ID: dataID(c.Find(".am-pc-city-filter-node").First()), Name: text(c.Find(".am-pc-city-name").First())}
			c.Find(".am-pc-district").Each(func(_ int, d *goquery.Selection) {
				city.Places = append(city.Places, Place{ID: dataID(d.Find(".am-pc-district-filter-node").First()), Name: text(d.Find(".am-pc-district-name").First())})
			})
			region.Places = append(region.Places, city)
		})
		regions = append(regions, region)
	})
	return regions
}

// parseFilterList reads a flat filter modal (metro, networks, streets): itemClass is e.g. ".am-pc-metro".
func parseFilterList(doc *goquery.Document, itemClass string) []Option {
	var out []Option
	doc.Find(itemClass + ".am-list-filter-item").Each(func(_ int, s *goquery.Selection) {
		out = append(out, Option{Name: text(s.Find(".am-list-filter-label").First()), ID: dataID(s)})
	})
	return out
}

// maxOrMin reads the header's highest price; the page leaves the element out when every price is the same.
func maxOrMin(doc *goquery.Document, min float64) float64 {
	v := doc.Find("#am-pc-add-product-prices-max-val")
	if v.Length() == 0 {
		return min
	}
	_, max := parsePriceRange(text(v))
	if max < min {
		return min
	}
	return max
}

// packUnits is how many units a pack holds: "№4" gives 4; a label without the mark falls back to the id.
func packUnits(name string, id int) int {
	if n := unitsFromName(name); n > 0 {
		return n
	}
	return id
}

// offerRow is a price line before duplicates are collapsed; ID is the site's product key when the
// page gives one.
type offerRow struct {
	PriceItem
	ID string
}

// groupItems collapses rows that are the same item (same name and product key) into one: the
// lowest price, and every distinct price ascending when there is more than one. Order of first appearance is kept.
func groupItems(rows []offerRow) []PriceItem {
	type key struct{ id, name string }
	var order []key
	prices := map[key][]float64{}
	names := map[key]string{}
	drugIDs := map[key]int{}
	for _, r := range rows {
		k := key{r.ID, r.Name}
		if _, ok := prices[k]; !ok {
			order = append(order, k)
			names[k] = r.Name
		}
		prices[k] = append(prices[k], r.Price)
		if r.DrugID > 0 {
			drugIDs[k] = r.DrugID
		}
	}
	var out []PriceItem
	for _, k := range order {
		ps := prices[k]
		slices.Sort(ps)
		ps = slices.Compact(ps)
		it := PriceItem{Name: names[k], Price: ps[0], DrugID: drugIDs[k]}
		if len(ps) > 1 {
			it.Prices = ps
		}
		if n := unitsFromName(it.Name); n > 0 {
			it.Pack = n
			if n > 1 {
				it.Units, it.UnitPrice = n, unitPrice(it.Price, n)
			}
		}
		out = append(out, it)
	}
	return out
}
