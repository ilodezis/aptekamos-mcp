package aptekamos

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// Fixtures are real AptekaMos pages saved on 2026-10-27 (Moscow region unless the name says otherwise).

func fixture(t *testing.T, name string) *goquery.Document {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestParseProductsSearch(t *testing.T) {
	items := parseProducts(fixture(t, "search.html"))
	if len(items) != 17 {
		t.Fatalf("items = %d, want 17", len(items))
	}
	first := items[0]
	want := Product{
		Kind: KindDrug, DrugID: 102376, BrandID: 7658, FormID: 34,
		Name: "Нурофен таблетки 200мг (для детей с 6 лет)",
		INN:  "Ибупрофен", INNID: 333,
		Group: "Нестероидный противовоспалительный препарат (НПВП)", GroupID: 69,
		Pharmacies: 4800, PriceMin: 110, PriceMax: 252, Available: true,
		URL: "https://aptekamos.ru/tovary/lekarstva/ibuprofen-333/nurofen-tabletki-200mg-dlya-deteyo-s-6-let-102376/ceni",
	}
	if first != want {
		t.Errorf("first item\n got %+v\nwant %+v", first, want)
	}
	gone := items[15]
	if gone.Name != "Нурофен плюс таблетки" || gone.Available || gone.Status != "Нет поставок" || gone.DrugID != 23150 || gone.INNID != 6353 {
		t.Errorf("unavailable item = %+v", gone)
	}
	if gone.BrandID != 0 || gone.Pharmacies != 0 || gone.PriceMin != 0 {
		t.Errorf("unavailable item has offers: %+v", gone)
	}
}

func TestParseProductsEmpty(t *testing.T) {
	if items := parseProducts(fixture(t, "search_empty.html")); len(items) != 0 {
		t.Errorf("items = %d, want 0", len(items))
	}
}

func TestParseProductsTradeforms(t *testing.T) {
	items := parseProducts(fixture(t, "substitutes.html"))
	if len(items) != 33 {
		t.Fatalf("items = %d, want 33", len(items))
	}
	if got := items[0]; got.Kind != KindTradeform || got.BrandID != 7863 || got.FormID != 34 || got.DrugID != 0 ||
		got.Name != "Мексидол таблетки" || got.Pharmacies != 5139 || got.PriceMin != 213 || got.PriceMax != 650 || !got.Available {
		t.Errorf("first = %+v", got)
	}
	if got := items[1]; got.PriceMax != 1121 || got.FormID != 27 {
		t.Errorf("second = %+v", got)
	}
	if got := items[25]; got.Name != "Церекард раствор для инъекций" || got.Available || got.Status != "нет в наличии" || got.BrandID != 20472 || got.FormID != 27 {
		t.Errorf("unavailable tradeform = %+v", got)
	}
}

func TestParseProductsBrowse(t *testing.T) {
	group := parseProducts(fixture(t, "group.html"))
	if len(group) != 36 {
		t.Fatalf("group items = %d, want 36", len(group))
	}
	if got := group[1]; got.DrugID != 51201 || got.PriceMax != 16800 || got.INNID != 323 {
		t.Errorf("group[1] = %+v", got)
	}
	if got := group[33]; got.Status != "Не производится" || got.Available {
		t.Errorf("group[33] = %+v", got)
	}
	brand := parseProducts(fixture(t, "brand.html"))
	if len(brand) != 3 || brand[0].DrugID != 13791 || brand[0].Name != "Мексидол таблетки 125мг" || brand[0].GroupID != 325 {
		t.Errorf("brand = %+v", brand)
	}
	inn := parseProducts(fixture(t, "inn.html"))
	if len(inn) != 1 || inn[0].DrugID != 13270 || inn[0].Pharmacies != 389 || inn[0].PriceMin != 1045 || inn[0].PriceMax != 5500 {
		t.Errorf("inn = %+v", inn)
	}
}

func TestParseCardDrug(t *testing.T) {
	c := parseCard(fixture(t, "drug.html"))
	if c.Name != "Тардиферон таблетки 80мг" || c.INN != "Железа сульфат" || c.Group != "Препараты железа" {
		t.Errorf("names = %+v", c)
	}
	if c.DrugID != 13270 || c.BrandID != 8826 || c.FormID != 34 || c.INNID != 326 {
		t.Errorf("ids = drug %d brand %d form %d inn %d", c.DrugID, c.BrandID, c.FormID, c.INNID)
	}
	if c.Pharmacies != 411 || c.PriceMin != 1045 || c.PriceMax != 5500 {
		t.Errorf("summary = %d %v..%v", c.Pharmacies, c.PriceMin, c.PriceMax)
	}
	if len(c.Packs) != 1 || c.Packs[0].Name != "№30" || c.Packs[0].ID != 30 {
		t.Errorf("packs = %+v", c.Packs)
	}
	if len(c.Countries) != 1 || c.Countries[0] != (Option{Name: "Франция", ID: 9}) {
		t.Errorf("countries = %+v", c.Countries)
	}
	if len(c.Manufacturers) != 1 || c.Manufacturers[0] != (Option{Name: "Пьер Фабр Медикамент", ID: 5890}) {
		t.Errorf("manufacturers = %+v", c.Manufacturers)
	}
	if c.productKey != "TDiiyyooJU4=" {
		t.Errorf("product key = %q", c.productKey)
	}
	if !strings.Contains(c.substitutesPath, "/kupit/zameni") || strings.Contains(c.substitutesPath, "aid=") {
		t.Errorf("substitutes path = %q", c.substitutesPath)
	}
}

func TestParseCardTradeform(t *testing.T) {
	c := parseCard(fixture(t, "where_tradeform.html"))
	if c.Name != "Мексидол таблетки" || c.BrandID != 7863 || c.FormID != 34 || c.DrugID != 0 || c.INNID != 7533 {
		t.Errorf("card = %+v", c)
	}
	if c.Pharmacies != 451 {
		t.Errorf("pharmacies = %d", c.Pharmacies)
	}
	// the dosage selector of a brand+form page lists exact products
	if len(c.Dosages) < 3 {
		t.Fatalf("dosages = %+v", c.Dosages)
	}
	found := false
	for _, d := range c.Dosages {
		if d.ID == 13791 && d.Name == "таблетки 125мг" {
			found = true
		}
	}
	if !found {
		t.Errorf("dosage 13791 missing: %+v", c.Dosages)
	}
	if len(c.Manufacturers) < 4 || c.productKey == "" {
		t.Errorf("manufacturers = %+v key %q", c.Manufacturers, c.productKey)
	}
}

func TestParseRowsMixed(t *testing.T) {
	rows := parseRows(fixture(t, "where_tradeform.html"))
	if len(rows) != 16 {
		t.Fatalf("rows = %d, want 16", len(rows))
	}
	net := rows[0]
	if net.Kind != RowNetwork || net.NetworkID != 56 || net.Name != "Доктор Столетов" || net.Pharmacies != 11 ||
		net.PriceMin != 286 || net.PriceMax != 574 || net.PharmacyID != 0 {
		t.Errorf("network row = %+v", net)
	}
	if !slices.Equal(net.Phones, []string{"+7(495)587-77-77", "+7(800)775-11-00"}) {
		t.Errorf("network phones = %q", net.Phones)
	}
	if !slices.Contains(net.Features, FeatureInStock) || !slices.Contains(net.Features, FeatureReserve) || !slices.Contains(net.Features, Feature24h) {
		t.Errorf("network features = %v", net.Features)
	}
	ph := rows[14]
	if ph.Kind != RowPharmacy || ph.PharmacyID != 27646 || ph.Name != "Дежурная аптека 24 на Ленинском" ||
		ph.Address != "Москва, Ленинский пр-кт, д.95, корп.3" || ph.Metro != "Проспект Вернадского, Новаторская" ||
		ph.PriceMin != 297 || ph.PriceMax != 621 {
		t.Errorf("pharmacy row = %+v", ph)
	}
	if !slices.Contains(ph.Features, FeatureOnlineOrder) || !slices.Contains(ph.Features, Feature24h) {
		t.Errorf("pharmacy features = %v", ph.Features)
	}
	if len(ph.Phones) != 1 || ph.Phones[0] != "+7(499)132-15-84" {
		t.Errorf("pharmacy phones = %q", ph.Phones)
	}
}

func TestParseRowsPharmacies(t *testing.T) {
	rows := parseRows(fixture(t, "where_metro.html"))
	if len(rows) != 35 {
		t.Fatalf("rows = %d, want 35", len(rows))
	}
	if r := rows[0]; r.Kind != RowPharmacy || r.PharmacyID != 8582 || r.Name != `"Эвалар" Аптека на Авиамоторной` || r.Metro != "Авиамоторная" ||
		!slices.Equal(r.Features, []string{FeatureInStock}) {
		t.Errorf("row 0 = %+v", r)
	}
	if r := rows[1]; r.PharmacyID != 18733 || r.PriceMin != 225 || r.PriceMax != 445 ||
		!slices.Contains(r.Features, FeatureReserve) || !slices.Contains(r.Features, FeatureOnlineOrder) {
		t.Errorf("row 1 = %+v", r)
	}
}

func TestParseRowsDrugPage(t *testing.T) {
	rows := parseRows(fixture(t, "drug.html"))
	if len(rows) != 27 {
		t.Fatalf("rows = %d, want 27", len(rows))
	}
	if r := rows[0]; r.Kind != RowNetwork || r.NetworkID != 2 || r.Name != "36,6" || r.PriceMin != 1156 || r.PriceMax != 1156 || r.Pharmacies != 2 {
		t.Errorf("row 0 = %+v", r)
	}
	if r := rows[1]; r.PharmacyID != 28697 || r.Name != `"ЛИЛАНИ+"` || r.Address != "Московская обл., г. Красногорск, Подмосковный б-р, д. 1 а" {
		t.Errorf("row 1 = %+v", r)
	}
	notes := 0
	for _, r := range rows {
		if r.PriceNote != "" {
			notes++
		}
	}
	if notes == 0 {
		t.Error("no price notes parsed")
	}
}

func TestParseRowsPharmacyList(t *testing.T) {
	rows := parseRows(fixture(t, "pharmacies.html"))
	if len(rows) != 30 {
		t.Fatalf("rows = %d, want 30", len(rows))
	}
	if r := rows[0]; r.Kind != RowNetwork || r.NetworkID != 138 || r.Name != "Супераптека" || r.Pharmacies != 3 || r.PriceMin != 0 ||
		!slices.Contains(r.Features, FeatureSale) {
		t.Errorf("row 0 = %+v", r)
	}
	net := parseRows(fixture(t, "pharmacies_network.html"))
	if len(net) != 10 || net[0].PharmacyID != 26833 || net[0].Kind != RowPharmacy || net[0].Address != "Москва, Ленинградское ш., д. 8, корп. 2" {
		t.Errorf("network pharmacies = %+v", net)
	}
	if empty := parseRows(fixture(t, "where_empty.html")); len(empty) != 0 {
		t.Errorf("empty rows = %d", len(empty))
	}
}

func TestParseListTotal(t *testing.T) {
	cases := map[string]int{"pharmacies.html": 484, "pharmacies_network.html": 10, "drug.html": 411, "where_tradeform.html": 451, "where_empty.html": 0}
	for name, want := range cases {
		if got := parseListTotal(fixture(t, name)); got != want {
			t.Errorf("%s: total = %d, want %d", name, got, want)
		}
	}
}

func TestParseBasket(t *testing.T) {
	doc := fixture(t, "basket.html")
	s := parseBasketSummary(doc)
	if s.Items != 2 || s.Pharmacies != 4604 || s.PriceMin != 241 || s.PriceMax != 892 {
		t.Errorf("summary = %+v", s)
	}
	rows := parseRows(doc)
	if len(rows) != 58 {
		t.Fatalf("rows = %d", len(rows))
	}
	if r := rows[2]; r.PharmacyID != 10742 || r.Name != "Аптека Райниса" || r.PriceMin != 402 || r.PriceMax != 501 {
		t.Errorf("row 2 = %+v", r)
	}
}

func TestParseInstruction(t *testing.T) {
	ins := parseInstruction(fixture(t, "instruction.html"))
	if ins.Name != "Тардиферон таблетки 80мг" {
		t.Errorf("name = %q", ins.Name)
	}
	titles := map[string]string{}
	for _, s := range ins.Sections {
		titles[s.Title] = s.Text
	}
	for _, want := range []string{"Состав", "Показания к применению", "Способ применения и дозировка", "Противопоказания", "Фармакологическое действие", "Условия хранения"} {
		if titles[want] == "" {
			t.Errorf("section %q missing or empty; have %v", want, keys(titles))
		}
	}
	if !strings.Contains(titles["Состав"], "железа сульфат") {
		t.Errorf("composition = %q", titles["Состав"])
	}
	if !strings.HasPrefix(titles["Противопоказания"], "Повышенная чувствительность") {
		t.Errorf("contraindications = %q", titles["Противопоказания"])
	}
	if !slices.Equal(ins.ATC, []string{"B03AA07 Железа сульфат"}) {
		t.Errorf("atc = %q", ins.ATC)
	}
	if len(ins.ICD10) != 19 || ins.ICD10[1] != "D50 Железодефицитная анемия" {
		t.Errorf("icd10 = %d %q", len(ins.ICD10), ins.ICD10)
	}
	if !slices.Equal(ins.Conditions, []string{"Анемия"}) {
		t.Errorf("conditions = %q", ins.Conditions)
	}
	if len(ins.Sources) == 0 || !strings.Contains(ins.Sources[0], "ГРЛС") {
		t.Errorf("sources = %q", ins.Sources)
	}
	for _, s := range ins.Sections {
		if s.Title == "Анатомо-терапевтическая-химическая группа" || s.Title == "Источники информации" || s.Title == "Применяется при лечении (МКБ-10)" {
			t.Errorf("structured section %q repeated as text", s.Title)
		}
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestParsePharmacy(t *testing.T) {
	p := parsePharmacy(fixture(t, "pharmacy.html"))
	if p.ID != 27324 || p.Name != "Аптека Хамовники" || p.Address != "Москва, ул. Ефремова, д.10, стр. 1, к. 4/3" ||
		p.Metro != "Фрунзенская, Спортивная" || p.Directions != `м. "Фрунзенская" пешком 7 минут или м. "Спортивная" выход №2` {
		t.Errorf("pharmacy = %+v", p)
	}
	if p.OpenStatus != "Откроется в ср. в 09:00" || p.OpenNow == nil || *p.OpenNow {
		t.Errorf("open = %q %v", p.OpenStatus, p.OpenNow)
	}
	if p.PriceListDate != "2026-10-27T20:00:00+03:00" || p.PriceListItems != 2080 {
		t.Errorf("price list = %q %d", p.PriceListDate, p.PriceListItems)
	}
	if !slices.Equal(p.Hours, []string{"ежедневно с 09:00 до 22:00"}) || !slices.Equal(p.Phones, []string{"+7(495)609-11-29"}) {
		t.Errorf("hours %q phones %q", p.Hours, p.Phones)
	}
	if p.TakesOrders {
		t.Error("Khamovniki does not take orders")
	}

	o := parsePharmacy(fixture(t, "pharmacy_order.html"))
	if o.ID != 24084 || !o.TakesOrders || len(o.Hours) != 3 || o.Hours[0] != "будни с 09:00 до 19:00" || o.PriceListItems != 778 {
		t.Errorf("ordering pharmacy = %+v", o)
	}
	if !slices.Equal(o.OrderOptions, []string{"Забрать в аптеке", "Доставка", "Предзаказ"}) {
		t.Errorf("order options = %q", o.OrderOptions)
	}
}

func TestParseOrderConditions(t *testing.T) {
	if got := parseOrderConditions(fixture(t, "order_conditions.html")); got != "Аптека не работает на заказ" {
		t.Errorf("conditions = %q", got)
	}
	if got := parseOrderConditions(fixture(t, "order_conditions_online.html")); got != "Самовывоз в течение 72 часов" {
		t.Errorf("conditions = %q", got)
	}
}

func TestParseOffer(t *testing.T) {
	items := parseOfferItems(fixture(t, "offer_tradeform.html"))
	if len(items) != 1 || items[0].Name != "Мексидол таблетки 125мг №50 ЗиО-Здоровье Россия" || items[0].Price != 627 || items[0].Units != 50 {
		t.Errorf("tradeform offer = %+v", items)
	}
	items = parseOfferItems(fixture(t, "offer_drug.html"))
	if len(items) != 1 || items[0].Name != "Тардиферон таблетки 80мг №30 Пьер Фабр Медикамент Франция" || items[0].Price != 2250 || items[0].UnitPrice != 75 || items[0].DrugID != 128863 || items[0].Pack != 30 {
		t.Errorf("drug offer = %+v", items)
	}
	if p := parsePharmacy(fixture(t, "offer_drug.html")); p.ID != 27324 || p.Name != "Аптека Хамовники" {
		t.Errorf("offer page pharmacy = %+v", p)
	}
}

func TestParsePriceList(t *testing.T) {
	items := parsePriceList(fixture(t, "price_list.html"))
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Name != "Нурофен для детей суспензия апельсиновая 100мг/5мл 200мл №1 Рекитт Бенкизер Хелскэр Великобритания" || items[0].Price != 327 || items[0].DrugID != 112184 || items[0].Pack != 1 {
		t.Errorf("first = %+v", items[0])
	}
	if items[2].Price != 144 {
		t.Errorf("third = %+v", items[2])
	}
}

func TestParsePriceListDedupesIdenticalRows(t *testing.T) {
	items := parsePriceList(fixture(t, "price_list_dupes.html"))
	if len(items) != 2 {
		t.Fatalf("expected 2 unique items after deduping duplicate rows, got %d: %+v", len(items), items)
	}
	if items[0].Price != 327 || items[0].Prices != nil {
		t.Errorf("first = %+v", items[0])
	}
	if items[1].Price != 144 {
		t.Errorf("second = %+v", items[1])
	}
}

func TestParseMap(t *testing.T) {
	one, err := parseMapPharmacies(fixture(t, "map_one.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 {
		t.Fatalf("pharmacies = %d", len(one))
	}
	p := one[0]
	if p.ID != 27324 || p.Lat != 55.725137 || p.Lng != 37.574207 || p.Name != "Аптека Хамовники" || p.Metro != "Фрунзенская, Спортивная" ||
		p.OpenStatus != "Откроется в ср. в 09:00" || slices.Contains(p.Features, Feature24h) || slices.Contains(p.Features, FeatureOnlineOrder) {
		t.Errorf("map pharmacy = %+v", p)
	}
	if !slices.Equal(p.Phones, []string{"+7(495)609-11-29"}) {
		t.Errorf("phones = %q", p.Phones)
	}
	net, err := parseMapPharmacies(fixture(t, "map_network.html"))
	if err != nil || len(net) != 10 {
		t.Errorf("network map = %d, %v", len(net), err)
	}
}

func TestParseRegions(t *testing.T) {
	regions := parseRegionTree(fixture(t, "regions.html"))
	if len(regions) != 65 {
		t.Fatalf("regions = %d", len(regions))
	}
	if r := regions[0]; r.ID != 1 || r.Name != "Московский регион" {
		t.Errorf("first = %+v", r)
	}
	cities, districts := 0, 0
	var moscow *Place
	for i := range regions {
		for j := range regions[i].Places {
			cities++
			districts += len(regions[i].Places[j].Places)
			if regions[i].Places[j].ID == 4 {
				moscow = &regions[i].Places[j]
			}
		}
	}
	if cities != 509 || districts != 26 {
		t.Errorf("cities %d districts %d", cities, districts)
	}
	if moscow == nil || moscow.Name != "Москва" || len(moscow.Places) == 0 || moscow.Places[0].ID != 420 || moscow.Places[0].Name != "ВАО" {
		t.Errorf("moscow = %+v", moscow)
	}
}

func TestParseFilterLists(t *testing.T) {
	metro := parseFilterList(fixture(t, "metro.html"), ".am-pc-metro")
	if len(metro) != 310 || metro[0] != (Option{Name: "Авиамоторная", ID: 124}) || metro[1].Name != "Авиамоторная МЦК" {
		t.Errorf("metro = %d %+v", len(metro), metro[:2])
	}
	nets := parseFilterList(fixture(t, "networks.html"), ".am-pc-net")
	if len(nets) != 50 || nets[0] != (Option{Name: "120/80", ID: 95}) {
		t.Errorf("networks = %d %+v", len(nets), nets[:1])
	}
	streets := parseFilterList(fixture(t, "streets.html"), ".am-pc-street")
	if len(streets) != 1735 || streets[0] != (Option{Name: "Августовская ул.", ID: 6282}) {
		t.Errorf("streets = %d %+v", len(streets), streets[:1])
	}
}

// The card header describes one pack filter at a time: unfiltered it spans every pack and pharmacy.
func TestParseCardPriceFollowsPackFilter(t *testing.T) {
	cases := []struct {
		fixture    string
		pharmacies int
		min, max   float64
	}{
		{"drug_packs.html", 2, 890, 2990},
		{"drug_packs_p1.html", 1, 890, 890}, // no max element on the page: max equals min, not zero
		{"drug_packs_p4.html", 2, 2900, 2990},
	}
	for _, tc := range cases {
		c := parseCard(fixture(t, tc.fixture))
		if c.Pharmacies != tc.pharmacies || c.PriceMin != tc.min || c.PriceMax != tc.max {
			t.Errorf("%s: %d pharmacies %v..%v, want %d %v..%v", tc.fixture, c.Pharmacies, c.PriceMin, c.PriceMax, tc.pharmacies, tc.min, tc.max)
		}
	}
	c := parseCard(fixture(t, "drug_packs.html"))
	if len(c.Packs) != 2 || c.Packs[0].Name != "№1" || c.Packs[0].ID != 1 || c.Packs[1].Name != "№4" || c.Packs[1].ID != 4 {
		t.Errorf("packs = %+v", c.Packs)
	}
	if got := parseBasketSummary(fixture(t, "basket.html")); got.PriceMax < got.PriceMin {
		t.Errorf("basket = %+v", got)
	}
}

func TestPackUnits(t *testing.T) {
	cases := map[string]int{"№1": 1, "№4": 4, "№30": 30, "№ 50": 50, "Тирвелла 5мг 0,5мл №4 Р-Фарм Россия": 4, "флакон": 0}
	for name, want := range cases {
		if got := unitsFromName(name); got != want {
			t.Errorf("unitsFromName(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestUnitPrice(t *testing.T) {
	if got := unitPrice(2900, 4); got != 725 {
		t.Errorf("2900/4 = %v", got)
	}
	if got := unitPrice(1000, 3); got != 333.33 {
		t.Errorf("1000/3 = %v", got)
	}
	if got := unitPrice(890, 1); got != 0 {
		t.Errorf("single unit = %v, want 0", got)
	}
	if got := unitPrice(890, 0); got != 0 {
		t.Errorf("unknown units = %v, want 0", got)
	}
}

// One pharmacy lists the same pack under several prices (7 rows, 5 distinct items).
func TestParseOfferCollapsesDuplicates(t *testing.T) {
	items := parseOfferItems(fixture(t, "offer_dupes.html"))
	if len(items) != 5 {
		t.Fatalf("items = %d: %+v", len(items), items)
	}
	by := map[string]PriceItem{}
	for _, it := range items {
		by[it.Name] = it
	}
	five := by["Тирвелла раствор для инъекций 5мг 0,5мл №4 Р-Фарм Россия"]
	if five.Price != 1490 || !slices.Equal(five.Prices, []float64{1490, 3990}) || five.Units != 4 || five.UnitPrice != 372.5 {
		t.Errorf("5 mg = %+v", five)
	}
	seven := by["Тирвелла раствор для инъекций 7,5мг 0,5мл №4 Р-Фарм Россия"]
	if seven.Price != 1790 || !slices.Equal(seven.Prices, []float64{1790, 4990}) {
		t.Errorf("7.5 mg = %+v", seven)
	}
	single := by["Тирвелла раствор для инъекций 2,5мг 0,5мл №1 Р-Фарм Россия"]
	if single.Price != 890 || single.Prices != nil || single.Units != 0 || single.UnitPrice != 0 {
		t.Errorf("№1 = %+v", single)
	}
	if two := by["Тирвелла раствор для инъекций 2,5мг 0,5мл №4 Р-Фарм Россия"]; two.Price != 2900 || two.Prices != nil {
		t.Errorf("2.5 mg №4 = %+v", two)
	}
}

// Rows with the same name but different product ids are different goods and stay apart.
func TestGroupItemsKeepsDifferentProducts(t *testing.T) {
	in := []offerRow{
		{PriceItem{Name: "A №2", Price: 10}, "x"},
		{PriceItem{Name: "A №2", Price: 7}, "x"},
		{PriceItem{Name: "A №2", Price: 9}, "y"},
		{PriceItem{Name: "B", Price: 5}, ""},
	}
	out := groupItems(in)
	if len(out) != 3 {
		t.Fatalf("out = %+v", out)
	}
	if out[0].Price != 7 || !slices.Equal(out[0].Prices, []float64{7, 10}) || out[0].Units != 2 || out[0].UnitPrice != 3.5 {
		t.Errorf("merged = %+v", out[0])
	}
	if out[1].Price != 9 || out[1].Prices != nil {
		t.Errorf("other id = %+v", out[1])
	}
}

func TestGroupItemsDeduplicatesIdenticalPrices(t *testing.T) {
	in := []offerRow{
		{PriceItem{Name: "Drug №10", Price: 120}, "id1"},
		{PriceItem{Name: "Drug №10", Price: 120}, "id1"},
	}
	out := groupItems(in)
	if len(out) != 1 {
		t.Fatalf("expected 1 item, got %d", len(out))
	}
	if out[0].Price != 120 {
		t.Errorf("price = %v, want 120", out[0].Price)
	}
	if out[0].Prices != nil {
		t.Errorf("expected Prices to be nil when identical, got %v", out[0].Prices)
	}
	if out[0].Pack != 10 || out[0].Units != 10 {
		t.Errorf("pack/units = %d/%d, want 10", out[0].Pack, out[0].Units)
	}
}

func TestFormatISODate(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"28.10.2026 13:46", "2026-10-28T13:46:00+03:00"},
		{"27.10.2026 20:00:15", "2026-10-27T20:00:15+03:00"},
		{"27.10.2026", "2026-10-27T00:00:00+03:00"},
		{"2026-10-28T13:46:00+03:00", "2026-10-28T13:46:00+03:00"},
		{"", ""},
		{"invalid", "invalid"},
	}
	for _, tc := range cases {
		got := formatISODate(tc.in)
		if got != tc.want {
			t.Errorf("formatISODate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
