package aptekamos

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// Fixtures search_tirz, inn_tirz, drug_tirz and offer_tirz are live pages saved on 2026-10-28 (a search for
// "тирзепатид", the INN page 17562, the card of drug 117477 and its offer in pharmacy 8528).

func productsFromHTML(t *testing.T, rows ...string) []Product {
	t.Helper()
	html := "<html><body>"
	for _, r := range rows {
		html += r
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html + "</body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	return parseProducts(doc)
}

// row is a product row; ids "brand/form" go into the basket button the way the site does for available rows.
func row(drug int, name, brandForm string) string {
	basket := ""
	if brandForm != "" {
		basket = `<button class="am-pc-product-basket-btn" data-go-to-link="/tovary/trd-x-` + strings.Split(brandForm, "/")[0] +
			`/frm-x-` + strings.Split(brandForm, "/")[1] + `/med-x-` + strconv.Itoa(drug) + `/kupit"></button>`
	} else {
		basket = `<button class="am-pc-product-basket-btn" disabled></button>`
	}
	return `<div class="am-pc-product-type-2-c"><a class="am-pc-product-name" href="/tovary/lekarstva/x-1/x-` + strconv.Itoa(drug) + `/ceni">` +
		name + `</a>` + basket + `</div>`
}

func byDrug(items []Product) map[int]Product {
	m := map[int]Product{}
	for _, p := range items {
		m[p.DrugID] = p
	}
	return m
}

// Rows without a basket link take brand and form from available rows with the same name prefix.
func TestParseProductsFillsIDsFromNeighbours(t *testing.T) {
	items := byDrug(parseProducts(fixture(t, "search_tirz.html")))
	if len(items) != 34 {
		t.Fatalf("items = %d, want 34", len(items))
	}
	for _, c := range []struct{ drug, brand, form int }{
		{117724, 40203, 27}, {117725, 40203, 27}, // Тирвелла 10 and 15 мг
		{116461, 39385, 27}, {116462, 39385, 27}, {116463, 39385, 27}, // Тирзеро Слим 10, 12,5, 15 мг
	} {
		if p := items[c.drug]; p.Available || p.BrandID != c.brand || p.FormID != c.form {
			t.Errorf("drug %d = brand %d form %d available %v, want brand %d form %d", c.drug, p.BrandID, p.FormID, p.Available, c.brand, c.form)
		}
	}
	// Мунжелио and Слимджет have no available neighbour: they stay for the card lookup
	for _, id := range []int{117529, 117508, 117509, 117510} {
		if p := items[id]; p.BrandID != 0 || p.FormID != 0 {
			t.Errorf("drug %d got brand %d form %d from nowhere", id, p.BrandID, p.FormID)
		}
	}
}

func TestParseProductsNeighbourPrefix(t *testing.T) {
	// the prefix runs up to the first token with a digit, so another brand with the same first word stays apart
	items := byDrug(productsFromHTML(t,
		row(1, "Альфа раствор 5мг", "10/27"),
		row(2, "Альфа раствор 10мг", ""),
		row(3, "Альфа таблетки 5мг", "10/34"),
		row(4, "Альфа таблетки 10мг", ""),
		row(5, "Альфа капли", ""),
	))
	if p := items[2]; p.BrandID != 10 || p.FormID != 27 {
		t.Errorf("same prefix = %+v", p)
	}
	if p := items[4]; p.BrandID != 10 || p.FormID != 34 {
		t.Errorf("other form prefix = %+v", p)
	}
	if p := items[5]; p.BrandID != 0 {
		t.Errorf("a prefix of its own matched a neighbour: %+v", p)
	}

	// one prefix leading to two (brand, form) pairs is ambiguous: nothing is guessed
	items = byDrug(productsFromHTML(t,
		row(1, "Бета раствор 5мг", "10/27"),
		row(2, "Бета раствор 7,5мг", "11/27"),
		row(3, "Бета раствор 10мг", ""),
	))
	if p := items[3]; p.BrandID != 0 || p.FormID != 0 {
		t.Errorf("ambiguous prefix guessed %+v", p)
	}
}

// What the neighbours could not tell is read from the card, for the rows handed out only.
func TestFillIDsReadsCardsForTheGivenRows(t *testing.T) {
	s, site := newFake(t)
	res, err := s.Search(context.Background(), "тирзепатид", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Products) != 34 {
		t.Fatalf("products = %d", len(res.Products))
	}
	site.Reset()
	page := res.Products[25:30] // Мунжелио, Слимджет x3, Тирвелла 15 мг
	got := s.FillIDs(context.Background(), page, "")
	for _, p := range got {
		if p.BrandID != 40203 || p.FormID != 27 {
			t.Errorf("%s (%d) = brand %d form %d", p.Name, p.DrugID, p.BrandID, p.FormID)
		}
	}
	reqs := site.Requests()
	if len(reqs) != 4 { // Тирвелла 15 мг already has its ids from a neighbour
		t.Errorf("requests = %q", reqs)
	}
	for _, r := range reqs {
		if !strings.HasSuffix(r, "/ceni") {
			t.Errorf("unexpected request %s", r)
		}
	}

	// a second pass is served from the cache
	site.Reset()
	s.FillIDs(context.Background(), page, "")
	if reqs := site.Requests(); len(reqs) != 0 {
		t.Errorf("second pass asked the site again: %q", reqs)
	}
	// rows that already have ids, or are not exact drugs, cost no request
	site.Reset()
	s.FillIDs(context.Background(), res.Products[:5], "")
	s.FillIDs(context.Background(), []Product{{Kind: KindTradeform, Name: "x"}}, "")
	if reqs := site.Requests(); len(reqs) != 0 {
		t.Errorf("complete rows cost requests: %q", reqs)
	}
	// a card that cannot be read leaves the row as it is
	bad := s.FillIDs(context.Background(), []Product{{Kind: KindDrug, DrugID: 999999999, Name: "x"}}, "")
	if bad[0].BrandID != 0 {
		t.Errorf("unknown drug got %+v", bad[0])
	}
}

// One name for a pharmacy id everywhere, and the page address on the offer.
func TestPharmacyJSONUsesPharmacyID(t *testing.T) {
	for name, v := range map[string]any{"Pharmacy": Pharmacy{ID: 7}, "MapPharmacy": MapPharmacy{ID: 7}} {
		raw, _ := json.Marshal(v)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		if m["pharmacy_id"] != float64(7) {
			t.Errorf("%s: pharmacy_id missing in %s", name, raw)
		}
		if _, has := m["id"]; has {
			t.Errorf("%s: still has id: %s", name, raw)
		}
	}
}

func TestOfferAtPharmacyURL(t *testing.T) {
	s, _ := newFake(t)
	o, err := s.OfferAt(context.Background(), 8528, Target{DrugID: 117477}, "")
	if err != nil {
		t.Fatal(err)
	}
	if o.Pharmacy.ID != 8528 || o.Pharmacy.URL != "https://aptekamos.ru/magaziny/org-x-8528" {
		t.Errorf("pharmacy = %+v", o.Pharmacy)
	}
	if len(o.Items) == 0 {
		t.Error("no items")
	}
}

// A product is not its own substitute.
func TestSubstitutesLeaveOutTheSource(t *testing.T) {
	s, _ := newFake(t)
	res, err := s.Substitutes(context.Background(), Target{DrugID: 117477}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Products) != 30 {
		t.Errorf("substitutes = %d, want 30 (31 on the page minus the source)", len(res.Products))
	}
	if _, has := byDrug(res.Products)[117477]; has {
		t.Error("drug 117477 is among its own substitutes")
	}
}

func TestWithoutSource(t *testing.T) {
	items := []Product{
		{Kind: KindDrug, DrugID: 1, BrandID: 10, FormID: 27},
		{Kind: KindDrug, DrugID: 2, BrandID: 10, FormID: 27},
		{Kind: KindDrug, DrugID: 3, BrandID: 10, FormID: 34},
		{Kind: KindTradeform, BrandID: 10, FormID: 27},
		{Kind: KindTradeform, BrandID: 11, FormID: 27},
	}
	ids := func(ps []Product) (out []int) {
		for _, p := range ps {
			out = append(out, p.DrugID*1000+p.BrandID)
		}
		return
	}
	// a drug: its own row goes, and the tradeform row of its own brand and form
	if got := ids(withoutSource(items, Target{DrugID: 1}, Card{DrugID: 1, BrandID: 10, FormID: 27})); !slices.Equal(got, []int{2010, 3010, 11}) {
		t.Errorf("drug source -> %v", got)
	}
	// a brand in a form: every row of that brand and form
	if got := ids(withoutSource(items, Target{BrandID: 10, FormID: 27}, Card{BrandID: 10, FormID: 27})); !slices.Equal(got, []int{3010, 11}) {
		t.Errorf("tradeform source -> %v", got)
	}
}

// B7a: the map address must not end in a bare & or ?.
func TestNearestURLHasNoDanglingSeparator(t *testing.T) {
	s, _ := newFake(t)
	for name, f := range map[string]Filters{"filters": {InStock: true}, "none": {}} {
		res, err := s.Nearest(context.Background(), &Target{DrugID: 13270}, 55.8188, 37.4975, f, "", 3, 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(res.URL, "&") || strings.HasSuffix(res.URL, "?") || strings.Contains(res.URL, "pm=modal") {
			t.Errorf("%s: url = %s", name, res.URL)
		}
	}
	res, _ := s.Nearest(context.Background(), &Target{DrugID: 13270}, 55.8188, 37.4975, Filters{InStock: true}, "", 3, 0)
	if res.URL != "https://aptekamos.ru/tovary/lekarstva/x-1/x-13270/ceni/na-karte?ins=1" {
		t.Errorf("url = %s", res.URL)
	}
}

// B7b: a missing page says which id is missing, and still is ErrNotFound.
func TestNotFoundNamesTheEntity(t *testing.T) {
	s, _ := newFake(t)
	ctx := context.Background()
	const miss = 999999999
	_, nearErr := s.Nearest(ctx, &Target{DrugID: miss}, 55.8, 37.5, Filters{}, "", 3, 0)
	_, cardErr := s.Card(ctx, Target{DrugID: miss}, "")
	_, brandErr := s.Card(ctx, Target{BrandID: miss, FormID: 27}, "")
	_, offersErr := s.Offers(ctx, Target{DrugID: miss}, Filters{}, "")
	_, subsErr := s.Substitutes(ctx, Target{DrugID: miss}, "")
	_, pharmErr := s.Pharmacy(ctx, miss)
	_, insErr := s.Instruction(ctx, miss)
	_, offerErr := s.OfferAt(ctx, miss, Target{DrugID: 13270}, "")
	_, innErr := s.Browse(ctx, "inn", miss, "")
	_, groupErr := s.Browse(ctx, "group", miss, "")
	_, trdErr := s.Browse(ctx, "brand", miss, "")
	for _, c := range []struct {
		err  error
		want string
	}{
		{cardErr, "drug_id 999999999 not found"},
		{offersErr, "drug_id 999999999 not found"},
		{subsErr, "drug_id 999999999 not found"},
		{nearErr, "drug_id 999999999 not found"},
		{brandErr, "brand_id 999999999 with form_id 27 not found"},
		{pharmErr, "pharmacy_id 999999999 not found"},
		{insErr, "drug_id 999999999 not found"},
		{offerErr, "pharmacy_id 999999999 or drug_id 13270 not found"},
		{innErr, "inn_id 999999999 not found"},
		{groupErr, "group_id 999999999 not found"},
		{trdErr, "brand_id 999999999 not found"},
	} {
		if c.err == nil || c.err.Error() != c.want {
			t.Errorf("error = %v, want %q", c.err, c.want)
		}
		if !errors.Is(c.err, ErrNotFound) {
			t.Errorf("%q is not ErrNotFound", c.want)
		}
	}
}
