package aptekamos

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Live tests talk to the real aptekamos.ru. They catch the site changing its markup;
// skip them with OFFLINE=1 or -short.

var liveService *Service

func live(t *testing.T) (*Service, context.Context) {
	t.Helper()
	if os.Getenv("OFFLINE") == "1" || testing.Short() {
		t.Skip("live test; OFFLINE=1 or -short")
	}
	if liveService == nil {
		liveService = NewService(NewClient())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	return liveService, ctx
}

// liveDrug finds a widely sold drug so the tests do not depend on one product staying on sale.
func liveDrug(t *testing.T, s *Service, ctx context.Context, region string) Product {
	t.Helper()
	res, err := s.Search(ctx, "нурофен", region)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Products {
		if p.Available && p.Kind == KindDrug && p.Pharmacies > 50 && p.BrandID > 0 {
			return p
		}
	}
	t.Fatalf("no widely sold Nurofen in %q: %+v", region, res.Products)
	return Product{}
}

func TestLiveSearchCardOffers(t *testing.T) {
	s, ctx := live(t)
	d := liveDrug(t, s, ctx, "")
	if d.INNID == 0 || d.GroupID == 0 || d.PriceMin <= 0 || d.PriceMax < d.PriceMin {
		t.Errorf("search item = %+v", d)
	}

	card, err := s.Card(ctx, Target{DrugID: d.DrugID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if card.Name == "" || card.INN == "" || card.BrandID != d.BrandID || card.FormID != d.FormID || card.Pharmacies == 0 || card.productKey == "" {
		t.Errorf("card = %+v", card)
	}

	l, err := s.Offers(ctx, Target{DrugID: d.DrugID}, Filters{Sort: "cheap"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Total == 0 || len(l.Rows) == 0 {
		t.Fatalf("offers = %+v", l)
	}
	pharmacies, networks := 0, 0
	for _, r := range l.Rows {
		switch r.Kind {
		case RowPharmacy:
			pharmacies++
			if r.PharmacyID == 0 || r.Name == "" || r.PriceMin == 0 {
				t.Errorf("pharmacy row = %+v", r)
			}
		case RowNetwork:
			networks++
			if r.NetworkID == 0 || r.Pharmacies == 0 {
				t.Errorf("network row = %+v", r)
			}
		}
	}
	if pharmacies == 0 || networks == 0 {
		t.Errorf("pharmacies %d networks %d", pharmacies, networks)
	}

	tf, err := s.Offers(ctx, Target{BrandID: d.BrandID, FormID: d.FormID}, Filters{Open24h: true}, "")
	if err != nil || tf.Card == nil || tf.Card.Kind != KindTradeform || len(tf.Card.Dosages) == 0 {
		t.Errorf("tradeform offers = %+v, %v", tf.Card, err)
	}
}

func TestLiveNearestAndOffer(t *testing.T) {
	s, ctx := live(t)
	d := liveDrug(t, s, ctx, "")
	// Red Square
	res, err := s.Nearest(ctx, &Target{DrugID: d.DrugID}, 55.7539, 37.6208, Filters{}, "", 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total < 50 || len(res.Pharmacies) != 5 {
		t.Fatalf("nearest = total %d got %d", res.Total, len(res.Pharmacies))
	}
	near := res.Pharmacies[0]
	if near.DistanceKm > 3 || near.PriceMin == 0 || near.Lat == 0 {
		t.Errorf("nearest pharmacy = %+v", near)
	}

	o, err := s.OfferAt(ctx, near.ID, Target{DrugID: d.DrugID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if o.Pharmacy.ID != near.ID || len(o.Items) == 0 || o.Items[0].Price == 0 {
		t.Errorf("offer = %+v", o)
	}

	ph, err := s.Pharmacy(ctx, near.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ph.Name == "" || ph.Address == "" || ph.Lat == 0 || ph.PriceListItems == 0 {
		t.Errorf("pharmacy = %+v", ph)
	}

	pl, err := s.PriceList(ctx, near.ID, "нурофен")
	if err != nil || len(pl.Items) == 0 {
		t.Errorf("price list = %+v, %v", pl, err)
	}
}

func TestLiveRegions(t *testing.T) {
	s, ctx := live(t)
	res, err := s.Search(ctx, "нурофен", "spb")
	if err != nil || len(res.Products) == 0 || !strings.Contains(res.URL, "/spb/") {
		t.Fatalf("spb search = %d %s %v", len(res.Products), res.URL, err)
	}
	d := liveDrug(t, s, ctx, "spb")
	// Nevsky prospekt
	near, err := s.Nearest(ctx, &Target{DrugID: d.DrugID}, 59.9343, 30.3351, Filters{}, "spb", 3, 0)
	if err != nil || len(near.Pharmacies) == 0 || near.Pharmacies[0].DistanceKm > 5 {
		t.Fatalf("spb nearest = %+v, %v", near, err)
	}
	ph, err := s.Pharmacy(ctx, near.Pharmacies[0].ID)
	if err != nil || !strings.Contains(ph.Address, "Петербург") && !strings.Contains(ph.Address, "Ленинградская") || ph.Lat == 0 {
		t.Errorf("spb pharmacy = %+v, %v", ph, err)
	}
	metro, err := s.Metro(ctx, "spb", "Невский")
	if err != nil || len(metro) == 0 {
		t.Errorf("spb metro = %+v, %v", metro, err)
	}
}

func TestLiveRegionTable(t *testing.T) {
	s, ctx := live(t)
	p, err := s.fetch(ctx, "/modal/regiony?pm=modal", 0)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Doc()
	if err != nil {
		t.Fatal(err)
	}
	tree := parseRegionTree(doc)
	if len(tree) != len(regions) {
		t.Errorf("site has %d regions, table %d", len(tree), len(regions))
	}
	known := map[int]string{}
	for _, r := range regions {
		known[r.ID] = r.Name
	}
	for _, r := range tree {
		if known[r.ID] != r.Name {
			t.Errorf("region %d %q not in the table (table says %q)", r.ID, r.Name, known[r.ID])
		}
	}
}

func TestLiveShoppingListAndSubstitutes(t *testing.T) {
	s, ctx := live(t)
	d := liveDrug(t, s, ctx, "")
	other, err := s.Search(ctx, "парацетамол", "")
	if err != nil {
		t.Fatal(err)
	}
	var second Product
	for _, p := range other.Products {
		if p.Available && p.Pharmacies > 50 {
			second = p
			break
		}
	}
	l, err := s.ShoppingList(ctx, []Target{{DrugID: d.DrugID}, {DrugID: second.DrugID}}, Filters{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Summary == nil || l.Summary.Items != 2 || l.Summary.Pharmacies == 0 || len(l.Rows) == 0 || l.Summary.PriceMin == 0 {
		t.Errorf("shopping list = %+v rows %d", l.Summary, len(l.Rows))
	}

	sub, err := s.Substitutes(ctx, Target{DrugID: d.DrugID}, "")
	if err != nil || len(sub.Products) == 0 {
		t.Errorf("substitutes = %d, %v", len(sub.Products), err)
	}
	ins, err := s.Instruction(ctx, d.DrugID)
	if err != nil || len(ins.Sections) < 3 {
		t.Errorf("instruction = %d sections, %v", len(ins.Sections), err)
	}
}

func TestLivePharmaciesAndReference(t *testing.T) {
	s, ctx := live(t)
	nets, err := s.Networks(ctx, "", "Столички")
	if err != nil || len(nets) != 1 {
		t.Fatalf("networks = %+v, %v", nets, err)
	}
	l, err := s.Pharmacies(ctx, Filters{Networks: []int{nets[0].ID}}, "")
	if err != nil || l.Total == 0 || len(l.Rows) == 0 || l.Rows[0].Kind != RowPharmacy {
		t.Errorf("pharmacies of a network = %+v, %v", l, err)
	}
	all, err := s.Pharmacies(ctx, Filters{Open24h: true}, "")
	if err != nil || all.Total == 0 || len(all.Rows) == 0 {
		t.Errorf("24h pharmacies = %d rows, %v", len(all.Rows), err)
	}
	places, err := s.Places(ctx, "")
	if err != nil || len(places) < 10 {
		t.Errorf("places = %d, %v", len(places), err)
	}
	streets, err := s.Streets(ctx, "", "Тверская")
	if err != nil || len(streets) == 0 {
		t.Errorf("streets = %+v, %v", streets, err)
	}
	near, err := s.Nearest(ctx, nil, 55.7539, 37.6208, Filters{Open24h: true}, "", 3, 0)
	if err != nil || len(near.Pharmacies) != 3 || !slices.Contains(near.Pharmacies[0].Features, Feature24h) {
		t.Errorf("nearest 24h = %+v, %v", near.Pharmacies, err)
	}
}

// Rows nobody sells have no basket link; their ids come from neighbours or the card. The INN page and the
// substitutes list leave out withdrawn products that search shows, so they never list more than search does.
func TestLiveUnavailableRowsHaveIDs(t *testing.T) {
	s, ctx := live(t)
	res, err := s.Search(ctx, "тирзепатид", "")
	if err != nil {
		t.Fatal(err)
	}
	var gone []Product
	for _, p := range res.Products {
		if !p.Available && p.Kind == KindDrug {
			gone = append(gone, p)
		}
	}
	if len(gone) == 0 {
		t.Skip("every tirzepatide product is on sale now")
	}
	for _, p := range s.FillIDs(ctx, gone, "") {
		if p.BrandID == 0 || p.FormID == 0 {
			t.Errorf("%s (%d): brand %d form %d", p.Name, p.DrugID, p.BrandID, p.FormID)
		}
	}
	inn, err := s.Browse(ctx, "inn", res.Products[0].INNID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(inn.Products) > len(res.Products) {
		t.Errorf("INN page lists %d, search %d", len(inn.Products), len(res.Products))
	}
	if _, err := s.Card(ctx, Target{DrugID: 999999999}, ""); err == nil || err.Error() != "drug_id 999999999 not found" || !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown drug: %v", err)
	}
}
