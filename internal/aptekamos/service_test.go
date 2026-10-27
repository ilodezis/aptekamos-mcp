package aptekamos

import (
	"context"
	"strings"
	"testing"

	"github.com/ilodezis/aptekamos-mcp/internal/fakesite"
)

func newFake(t *testing.T) (*Service, *fakesite.Site) {
	site := fakesite.New(t, "testdata")
	c := NewClient()
	c.BaseURL = site.URL
	c.MinInterval = 0
	return NewService(c), site
}

func TestResolveRegion(t *testing.T) {
	cases := map[string]string{"": "", "moscow": "", "Москва": "", "1": "", "spb": "spb", "2": "spb", "Санкт-Петербург": "spb", "питер": "spb",
		"Самарская область": "samara", "самара": "samara", "ekb": "ekb", "Свердловская": "ekb", "134": "ekb"}
	for in, want := range cases {
		r, err := ResolveRegion(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if r.Slug != want {
			t.Errorf("%q -> %q, want %q", in, r.Slug, want)
		}
	}
	for _, bad := range []string{"Атлантида", "область", "999"} {
		if _, err := ResolveRegion(bad); err == nil {
			t.Errorf("%q resolved", bad)
		}
	}
}

func TestFiltersQuery(t *testing.T) {
	q, err := Filters{
		Places: []int{4, 20}, Metro: []int{124, 32}, Networks: []int{100}, Streets: []int{6282},
		InStock: true, Reserve: true, Delivery: true, Preorder: true, Open24h: true, Sale: true,
		Pack: 30, Manufacturer: 5890, Country: 9, PriceMin: 100, PriceMax: 900, PharmacyQuery: "Хамовники", Sort: "cheap",
	}.query()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"r": "4,20", "me": "124,32", "mn": "100", "st": "6282", "ins": "1", "rsrv": "1", "deliv": "1", "preorder": "1",
		"duty": "1", "sale": "1", "p": "30", "f": "5890", "c": "9", "min": "100", "max": "900", "q": "Хамовники", "so": "1"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	near, err := Filters{Sort: "near", Lat: 55.75, Lng: 37.61}.query()
	if err != nil || near.Get("so") != "8" || near.Get("llat") != "55.75" || near.Get("llng") != "37.61" {
		t.Errorf("near = %v, %v", near, err)
	}
	if _, err := (Filters{Sort: "near"}).query(); err == nil {
		t.Error("near without coordinates accepted")
	}
	if _, err := (Filters{Sort: "random"}).query(); err == nil {
		t.Error("unknown sort accepted")
	}
	if empty, _ := (Filters{}).query(); len(empty) != 0 {
		t.Errorf("empty filters = %v", empty)
	}
}

func TestSearch(t *testing.T) {
	s, site := newFake(t)
	res, err := s.Search(context.Background(), "нурофен", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Products) != 17 || res.Products[0].DrugID != 102376 {
		t.Errorf("products = %d", len(res.Products))
	}
	path, q := site.Last(t)
	if path != "/tovary" || q.Get("q") != "нурофен" {
		t.Errorf("request = %s %v", path, q)
	}
	if _, err := s.Search(context.Background(), "нурофен", "spb"); err != nil {
		t.Fatal(err)
	}
	if path, _ := site.Last(t); path != "/spb/tovary" {
		t.Errorf("spb request = %s", path)
	}
	empty, err := s.Search(context.Background(), "zzz", "")
	if err != nil || len(empty.Products) != 0 {
		t.Errorf("empty = %+v, %v", empty, err)
	}
	if _, err := s.Search(context.Background(), "  ", ""); err == nil {
		t.Error("blank query accepted")
	}
}

func TestBrowse(t *testing.T) {
	s, site := newFake(t)
	for kind, wantPath := range map[string]string{"inn": "/tovary/inn-x-326", "group": "/tovary/phg-x-308", "brand": "/tovary/trd-x-7863"} {
		id := map[string]int{"inn": 326, "group": 308, "brand": 7863}[kind]
		res, err := s.Browse(context.Background(), kind, id, "")
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if len(res.Products) == 0 {
			t.Errorf("%s: no products", kind)
		}
		if path, _ := site.Last(t); path != wantPath {
			t.Errorf("%s request = %s", kind, path)
		}
	}
	if _, err := s.Browse(context.Background(), "colour", 1, ""); err == nil {
		t.Error("unknown kind accepted")
	}
}

func TestCardAndOffers(t *testing.T) {
	s, site := newFake(t)
	card, err := s.Card(context.Background(), Target{DrugID: 13270}, "")
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "Тардиферон таблетки 80мг" || card.URL != "https://aptekamos.ru/tovary/lekarstva/x-1/x-13270/ceni" {
		t.Errorf("card = %+v", card)
	}

	l, err := s.Offers(context.Background(), Target{BrandID: 7863, FormID: 34}, Filters{Metro: []int{124}, Open24h: true, Sort: "cheap"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 451 || len(l.Rows) != 16 || l.Card == nil || l.Card.Name != "Мексидол таблетки" {
		t.Errorf("listing = total %d rows %d card %+v", l.Total, len(l.Rows), l.Card)
	}
	path, q := site.Last(t)
	if path != "/tovary/trd-x-7863/frm-x-34/kupit" || q.Get("me") != "124" || q.Get("duty") != "1" || q.Get("so") != "1" {
		t.Errorf("request = %s %v", path, q)
	}
	if !strings.HasPrefix(l.URL, "https://aptekamos.ru/tovary/trd-x-7863/frm-x-34/kupit?") {
		t.Errorf("url = %s", l.URL)
	}
	if _, err := s.Offers(context.Background(), Target{BrandID: 7863}, Filters{}, ""); err == nil {
		t.Error("brand without form accepted")
	}
}

func TestNearest(t *testing.T) {
	s, site := newFake(t)
	// Stolichki on Leningradskoe sh. 8 is next to Voykovskaya.
	res, err := s.Nearest(context.Background(), &Target{DrugID: 13270}, 55.8188, 37.4975, Filters{InStock: true}, "", 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 10 || len(res.Pharmacies) != 3 {
		t.Fatalf("total %d returned %d", res.Total, len(res.Pharmacies))
	}
	if res.Pharmacies[0].ID != 26833 {
		t.Errorf("nearest = %+v", res.Pharmacies[0])
	}
	for i := 1; i < len(res.Pharmacies); i++ {
		if res.Pharmacies[i].DistanceKm < res.Pharmacies[i-1].DistanceKm {
			t.Error("not sorted by distance")
		}
	}
	path, q := site.Last(t)
	if path != "/tovary/lekarstva/x-1/x-13270/ceni/na-karte" || q.Get("pm") != "modal" || q.Get("ins") != "1" {
		t.Errorf("request = %s %v", path, q)
	}

	// no product: all pharmacies; radius cuts the far ones
	all, err := s.Nearest(context.Background(), nil, 55.8188, 37.4975, Filters{}, "", 50, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all.Pharmacies {
		if p.DistanceKm > 2 {
			t.Errorf("%d is %.1f km away, radius 2", p.ID, p.DistanceKm)
		}
	}
	if all.Total != 10 || len(all.Pharmacies) == 0 || len(all.Pharmacies) == 10 {
		t.Errorf("radius result = total %d within %d", all.Total, len(all.Pharmacies))
	}
	if path, _ := site.Last(t); path != "/magaziny/apteki/na-karte/karta" {
		t.Errorf("request = %s", path)
	}
}

func TestOfferAtPharmacy(t *testing.T) {
	s, site := newFake(t)
	o, err := s.OfferAt(context.Background(), 27324, Target{DrugID: 13270}, "")
	if err != nil {
		t.Fatal(err)
	}
	if o.Pharmacy.ID != 27324 || len(o.Items) != 1 || o.Items[0].Price != 2250 {
		t.Errorf("offer = %+v", o)
	}
	reqs := site.Requests()
	if len(reqs) != 2 || reqs[0] != "/tovary/lekarstva/x-1/x-13270/ceni" || reqs[1] != "/tovary/trd-x-8826/frm-x-34/med-x-13270/kupit/org-x-27324" {
		t.Errorf("requests = %q", reqs)
	}
	o, err = s.OfferAt(context.Background(), 27324, Target{BrandID: 7863, FormID: 34}, "")
	if err != nil || len(o.Items) != 1 || o.Items[0].Price != 627 {
		t.Errorf("tradeform offer = %+v, %v", o, err)
	}
	if path, _ := site.Last(t); path != "/tovary/trd-x-7863/frm-x-34/kupit/org-x-27324" {
		t.Errorf("request = %s", path)
	}
}

func TestPharmacyCard(t *testing.T) {
	s, site := newFake(t)
	p, err := s.Pharmacy(context.Background(), 24084)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != 24084 || !p.TakesOrders || p.OrderConditions != "Самовывоз в течение 72 часов" {
		t.Errorf("pharmacy = %+v", p)
	}
	if p.URL != "https://aptekamos.ru/magaziny/org-x-24084" {
		t.Errorf("url = %s", p.URL)
	}
	// coordinates come from the map; the fake map has no 24084, so they stay empty without failing
	if p.Lat != 0 {
		t.Errorf("lat = %v", p.Lat)
	}
	reqs := site.Requests()
	if len(reqs) != 3 {
		t.Errorf("requests = %q", reqs)
	}

	site.Reset()
	k, err := s.Pharmacy(context.Background(), 27324)
	if err != nil || k.TakesOrders || k.OrderConditions != "" {
		t.Errorf("non-ordering pharmacy = %+v, %v", k, err)
	}
	for _, r := range site.Requests() {
		if strings.Contains(r, "usloviya") {
			t.Error("asked order conditions of a pharmacy that takes no orders")
		}
	}
}

func TestPriceList(t *testing.T) {
	s, site := newFake(t)
	res, err := s.PriceList(context.Background(), 27324, "нурофен")
	if err != nil || len(res.Items) != 3 {
		t.Fatalf("items = %+v, %v", res, err)
	}
	path, q := site.Last(t)
	if path != "/magaziny/org-x-27324/price-list" || q.Get("q") != "нурофен" || q.Get("pm") != "modal" {
		t.Errorf("request = %s %v", path, q)
	}
}

func TestPharmacies(t *testing.T) {
	s, site := newFake(t)
	l, err := s.Pharmacies(context.Background(), Filters{Open24h: true, Networks: []int{2}}, "spb")
	if err != nil {
		t.Fatal(err)
	}
	if l.Total != 484 || len(l.Rows) != 30 {
		t.Errorf("listing = %d %d", l.Total, len(l.Rows))
	}
	path, q := site.Last(t)
	if path != "/spb/magaziny/apteki/na-karte" || q.Get("duty") != "1" || q.Get("mn") != "2" {
		t.Errorf("request = %s %v", path, q)
	}
}

func TestSubstitutes(t *testing.T) {
	s, site := newFake(t)
	res, err := s.Substitutes(context.Background(), Target{DrugID: 13270}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Products) != 33 {
		t.Errorf("substitutes = %d", len(res.Products))
	}
	path, q := site.Last(t)
	if path != "/tovary/inn-zheleza-sulfat-326/trd-tardiferon-8826/frm-tabletki-34/med-tardiferon-tabletki-80mg-13270/kupit/zameni" || q.Get("pm") != "modal" {
		t.Errorf("request = %s %v", path, q)
	}
}

func TestShoppingList(t *testing.T) {
	s, site := newFake(t)
	l, err := s.ShoppingList(context.Background(), []Target{{DrugID: 13270}, {BrandID: 7863, FormID: 34}}, Filters{InStock: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Summary == nil || l.Summary.Pharmacies != 4604 || len(l.Rows) != 58 {
		t.Errorf("listing = %+v", l.Summary)
	}
	path, q := site.Last(t)
	if path != "/korzina" || q.Get("aid") != "bsw" || q.Get("ins") != "1" {
		t.Errorf("request = %s %v", path, q)
	}
	keys := strings.Split(q.Get("productids"), ",")
	if len(keys) != 2 || keys[0] != "TDiiyyooJU4=" || keys[1] == "" {
		t.Errorf("productids = %q", keys)
	}
	if _, err := s.ShoppingList(context.Background(), nil, Filters{}, ""); err == nil {
		t.Error("empty list accepted")
	}
}

func TestReference(t *testing.T) {
	s, site := newFake(t)
	places, err := s.Places(context.Background(), "")
	if err != nil || len(places) == 0 || places[0].Name != "Москва" {
		t.Fatalf("places = %v, %v", len(places), err)
	}
	if path, _ := site.Last(t); path != "/modal/regiony" {
		t.Errorf("request = %s", path)
	}
	metro, err := s.Metro(context.Background(), "spb", "")
	if err != nil || len(metro) != 310 {
		t.Errorf("metro = %d, %v", len(metro), err)
	}
	if path, _ := site.Last(t); path != "/spb/modal/metro" {
		t.Errorf("request = %s", path)
	}
	nets, err := s.Networks(context.Background(), "", "здрав")
	if err != nil || len(nets) == 0 {
		t.Fatalf("networks = %v, %v", nets, err)
	}
	for _, n := range nets {
		if !strings.Contains(strings.ToLower(n.Name), "здрав") {
			t.Errorf("filter leaked %q", n.Name)
		}
	}
	streets, err := s.Streets(context.Background(), "", "Августовская")
	if err != nil || len(streets) != 1 || streets[0].ID != 6282 {
		t.Errorf("streets = %v, %v", streets, err)
	}
}

func TestInstruction(t *testing.T) {
	s, site := newFake(t)
	ins, err := s.Instruction(context.Background(), 13270)
	if err != nil || ins.DrugID != 13270 || len(ins.Sections) == 0 {
		t.Fatalf("instruction = %+v, %v", ins, err)
	}
	if path, _ := site.Last(t); path != "/tovary/lekarstva/x-1/x-13270/instrukciya" {
		t.Errorf("request = %s", path)
	}
}

func TestCardWithPacksPricesEachPack(t *testing.T) {
	s, site := newFake(t)
	card, err := s.CardWithPacks(context.Background(), Target{DrugID: 117477}, "")
	if err != nil {
		t.Fatal(err)
	}
	// the unfiltered header spans both packs
	if card.PriceMin != 890 || card.PriceMax != 2990 || card.Pharmacies != 2 || len(card.Packs) != 2 {
		t.Fatalf("card = %+v", card)
	}
	one, four := card.Packs[0], card.Packs[1]
	if one.ID != 1 || one.Units != 1 || one.Pharmacies != 1 || one.PriceMin != 890 || one.PriceMax != 890 || one.UnitPriceMin != 0 {
		t.Errorf("№1 = %+v", one)
	}
	if four.ID != 4 || four.Units != 4 || four.Pharmacies != 2 || four.PriceMin != 2900 || four.PriceMax != 2990 || four.UnitPriceMin != 725 {
		t.Errorf("№4 = %+v", four)
	}
	got := strings.Join(site.Requests(), " ")
	for _, want := range []string{"x-117477/ceni?p=1", "x-117477/ceni?p=4"} {
		if !strings.Contains(got, want) {
			t.Errorf("requests %q miss %q", got, want)
		}
	}

	// the plain Card stays cheap: one request, packs without prices
	site.Reset()
	plain, err := s.Card(context.Background(), Target{DrugID: 117477}, "")
	if err != nil || len(site.Requests()) != 0 && strings.Contains(strings.Join(site.Requests(), " "), "?p=") {
		t.Errorf("plain card fetched packs: %v %v", site.Requests(), err)
	}
	if len(plain.Packs) != 2 || plain.Packs[1].PriceMin != 0 {
		t.Errorf("plain packs = %+v", plain.Packs)
	}

	// a brand in one form mixes dosages: no per-pack requests
	site.Reset()
	tf, err := s.CardWithPacks(context.Background(), Target{BrandID: 40203, FormID: 27}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range site.Requests() {
		if strings.Contains(r, "p=") {
			t.Errorf("tradeform card fetched a pack page: %s", r)
		}
	}
	for _, p := range tf.Packs {
		if p.PriceMin != 0 || p.Pharmacies != 0 {
			t.Errorf("tradeform pack priced: %+v", p)
		}
	}
}

func TestOffersPriceNote(t *testing.T) {
	s, _ := newFake(t)
	cases := []struct {
		name     string
		t        Target
		f        Filters
		wantNote []string // substrings; nil means no note
	}{
		{"drug with two packs", Target{DrugID: 117477}, Filters{}, []string{"№1", "№4", "pack="}},
		{"pack filter", Target{DrugID: 117477}, Filters{Pack: 4}, nil},
		{"brand with several dosages", Target{BrandID: 40203, FormID: 27}, Filters{}, []string{"price_at_pharmacy"}},
		{"single pack drug", Target{DrugID: 13270}, Filters{}, nil},
	}
	for _, tc := range cases {
		l, err := s.Offers(context.Background(), tc.t, tc.f, "")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.wantNote == nil {
			if l.PriceNote != "" {
				t.Errorf("%s: unexpected note %q", tc.name, l.PriceNote)
			}
			continue
		}
		for _, sub := range tc.wantNote {
			if !strings.Contains(l.PriceNote, sub) {
				t.Errorf("%s: note %q misses %q", tc.name, l.PriceNote, sub)
			}
		}
	}
}

func TestProductListPriceNote(t *testing.T) {
	s, _ := newFake(t)
	res, err := s.Search(context.Background(), "нурофен", "")
	if err != nil || !strings.Contains(res.PriceNote, "drug_card") {
		t.Errorf("search note = %q, %v", res.PriceNote, err)
	}
	empty, err := s.Search(context.Background(), "zzz", "")
	if err != nil || empty.PriceNote != "" {
		t.Errorf("empty search note = %q, %v", empty.PriceNote, err)
	}
}

func TestOfferAtCollapsesDuplicates(t *testing.T) {
	s, _ := newFake(t)
	o, err := s.OfferAt(context.Background(), 8528, Target{BrandID: 40203, FormID: 27}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Items) != 5 {
		t.Fatalf("items = %+v", o.Items)
	}
	for _, it := range o.Items {
		if strings.Contains(it.Name, " 5мг ") && (it.Price != 1490 || len(it.Prices) != 2) {
			t.Errorf("5 mg = %+v", it)
		}
	}
}
