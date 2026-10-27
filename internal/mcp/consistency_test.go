package mcp

import (
	"slices"
	"strings"
	"testing"
)

// Unavailable rows come out with brand_id and form_id on every page.
func TestSearchFillsIDsOfUnavailableRows(t *testing.T) {
	cs, site := session(t)
	// only the rows that go out cost a card request: a page of available rows costs none
	mustCall(t, cs, "search_drugs", map[string]any{"query": "тирзепатид", "limit": 5})
	if reqs := cardRequests(site.Requests()); len(reqs) != 0 {
		t.Errorf("first page requests = %q", reqs)
	}
	// Мунжелио and Слимджет: no neighbour, the card tells; Тирвелла 15 мг is filled from its neighbours
	out := mustCall(t, cs, "search_drugs", map[string]any{"query": "тирзепатид", "limit": 6, "offset": 25})
	if cards := cardRequests(site.Requests()); len(cards) != 4 {
		t.Errorf("card requests = %q", cards)
	}
	for _, p := range list(out, "products") {
		if row := p.(map[string]any); row["brand_id"] != float64(40203) {
			t.Errorf("%v = brand %v", row["name"], row["brand_id"])
		}
	}

	out = mustCall(t, cs, "search_drugs", map[string]any{"query": "тирзепатид", "limit": 100})
	gone := 0
	for _, p := range list(out, "products") {
		row := p.(map[string]any)
		if row["status"] == nil {
			continue
		}
		gone++
		if row["brand_id"] == nil || row["form_id"] == nil {
			t.Errorf("%v has no brand_id/form_id", row["name"])
		}
	}
	if gone != 9 {
		t.Errorf("unavailable rows = %d, want 9", gone)
	}
}

func TestBrowseFillsIDsOfUnavailableRows(t *testing.T) {
	cs, _ := session(t)
	out := mustCall(t, cs, "browse", map[string]any{"kind": "inn", "id": 17562, "limit": 100})
	rows := list(out, "products")
	if len(rows) != 31 {
		t.Fatalf("rows = %d", len(rows))
	}
	for _, p := range rows {
		if row := p.(map[string]any); row["brand_id"] == nil || row["form_id"] == nil {
			t.Errorf("%v has no brand_id/form_id", row["name"])
		}
	}
}

// One name for a pharmacy id, and the pharmacy page on an offer.
func TestPharmacyIDIsOneName(t *testing.T) {
	cs, _ := session(t)
	card := mustCall(t, cs, "pharmacy_card", map[string]any{"pharmacy_id": 24084})
	if card["pharmacy_id"] != float64(24084) || card["id"] != nil {
		t.Errorf("pharmacy_card = %v", card)
	}
	offer := mustCall(t, cs, "price_at_pharmacy", map[string]any{"pharmacy_id": 8528, "drug_id": 117477})
	ph := offer["pharmacy"].(map[string]any)
	if ph["pharmacy_id"] != float64(8528) || ph["id"] != nil || ph["url"] != "https://aptekamos.ru/magaziny/org-x-8528" {
		t.Errorf("price_at_pharmacy.pharmacy = %v", ph)
	}
	near := mustCall(t, cs, "nearest_pharmacies", map[string]any{"lat": 55.8188, "lng": 37.4975, "drug_id": 13270, "limit": 2})
	first := list(near, "pharmacies")[0].(map[string]any)
	if first["pharmacy_id"] != float64(26833) || first["id"] != nil {
		t.Errorf("nearest_pharmacies = %v", first)
	}
	if u, _ := near["url"].(string); strings.HasSuffix(u, "&") || strings.HasSuffix(u, "?") {
		t.Errorf("url = %s", u)
	}
}

// A product is not its own substitute.
func TestSubstitutesTool(t *testing.T) {
	cs, _ := session(t)
	out := mustCall(t, cs, "substitutes", map[string]any{"drug_id": 117477, "limit": 100})
	if out["products_total"] != float64(30) {
		t.Errorf("products_total = %v", out["products_total"])
	}
	for _, p := range list(out, "products") {
		if p.(map[string]any)["drug_id"] == float64(117477) {
			t.Error("the source is among its substitutes")
		}
	}
}

// B7b: the error names the missing id.
func TestNotFoundErrors(t *testing.T) {
	cs, _ := session(t)
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"drug_card", map[string]any{"drug_id": 999999999}, "drug_id 999999999 not found"},
		{"drug_card", map[string]any{"brand_id": 999999999, "form_id": 27}, "brand_id 999999999 with form_id 27 not found"},
		{"pharmacy_card", map[string]any{"pharmacy_id": 999999999}, "pharmacy_id 999999999 not found"},
		{"drug_instruction", map[string]any{"drug_id": 999999999}, "drug_id 999999999 not found"},
		{"browse", map[string]any{"kind": "group", "id": 999999999}, "group_id 999999999 not found"},
	} {
		out, isErr := call(t, cs, c.tool, c.args)
		if !isErr || out["error"] != c.want {
			t.Errorf("%s%v = %v, want %q", c.tool, c.args, out["error"], c.want)
		}
	}
}

// B7c: the number of pharmacies is said once.
func TestShoppingListSaysPharmaciesOnce(t *testing.T) {
	cs, _ := session(t)
	out := mustCall(t, cs, "shopping_list", map[string]any{
		"items": []any{map[string]any{"drug_id": 13270}, map[string]any{"brand_id": 7863, "form_id": 34}}, "limit": 2,
	})
	sum := out["list"].(map[string]any)
	if _, dup := sum["pharmacies"]; dup {
		t.Errorf("list repeats pharmacies: %v", sum)
	}
	if out["pharmacies"] != float64(4604) || !slices.Contains(keys(sum), "items") {
		t.Errorf("shopping_list = %v", out)
	}
}

func cardRequests(reqs []string) (out []string) {
	for _, r := range reqs {
		if strings.HasSuffix(r, "/ceni") {
			out = append(out, r)
		}
	}
	return
}

func keys(m map[string]any) (out []string) {
	for k := range m {
		out = append(out, k)
	}
	return
}
