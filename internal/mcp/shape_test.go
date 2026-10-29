package mcp

import (
	"context"
	"strings"
	"testing"
)

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

func TestSearchHoistsCommonFieldsAndCompactsRows(t *testing.T) {
	cs, _ := session(t)
	out := mustCall(t, cs, "search_drugs", map[string]any{"query": "тирзепатид", "limit": 100})
	common := obj(out["common"])
	if common["kind"] != "drug" || common["inn"] != "Тирзепатид" || common["inn_id"].(float64) != 17562 || common["group_id"].(float64) != 1159 || common["group"] == "" {
		t.Fatalf("common = %v", common)
	}
	products := list(out, "products")
	if len(products) != 34 {
		t.Fatalf("products = %d", len(products))
	}
	unavailable := 0
	for _, p := range products {
		row := obj(p)
		for _, k := range []string{"kind", "inn", "inn_id", "group", "group_id", "url", "available"} {
			if _, ok := row[k]; ok {
				t.Errorf("row carries %s: %v", k, row)
			}
		}
		if row["status"] != nil {
			unavailable++
			if _, ok := row["pharmacies"]; ok {
				t.Errorf("unavailable row carries pharmacies: %v", row)
			}
			if _, ok := row["price_min"]; ok {
				t.Errorf("unavailable row carries a price: %v", row)
			}
		} else if row["pharmacies"] == nil {
			t.Errorf("available row without pharmacies: %v", row)
		}
	}
	if unavailable != 9 {
		t.Errorf("unavailable rows = %d", unavailable)
	}
	if out["url"] == nil || out["url"] == "" {
		t.Error("top-level url is gone")
	}

	only := mustCall(t, cs, "search_drugs", map[string]any{"query": "тирзепатид", "available_only": true, "limit": 10})
	if only["products_total"].(float64) != 25 || len(list(only, "products")) != 10 {
		t.Errorf("available_only: total %v, rows %d", only["products_total"], len(list(only, "products")))
	}
	for _, p := range list(only, "products") {
		if obj(p)["status"] != nil {
			t.Errorf("unavailable row in available_only: %v", p)
		}
	}
	// the filter runs before paging: the last page holds what is left of the available rows
	last := mustCall(t, cs, "search_drugs", map[string]any{"query": "тирзепатид", "available_only": true, "limit": 10, "offset": 20})
	if len(list(last, "products")) != 5 {
		t.Errorf("last available page = %d", len(list(last, "products")))
	}
}

func TestMixedProductsKeepTheirOwnFields(t *testing.T) {
	cs, _ := session(t)
	out := mustCall(t, cs, "search_drugs", map[string]any{"query": "нурофен", "limit": 100})
	common := obj(out["common"])
	for _, p := range list(out, "products") {
		row := obj(p)
		for _, k := range []string{"kind", "inn", "inn_id", "group", "group_id"} {
			if _, inRow := row[k]; inRow {
				if _, inCommon := common[k]; inCommon {
					t.Errorf("%s is in the row and in common at once: %v %v", k, row, common)
				}
			}
		}
	}
}

func TestBrowseAndSubstitutesShareTheRowShape(t *testing.T) {
	cs, _ := session(t)
	for name, args := range map[string]map[string]any{
		"browse":      {"kind": "inn", "id": 17562, "available_only": true},
		"substitutes": {"drug_id": 13270, "available_only": true},
	} {
		out := mustCall(t, cs, name, args)
		common, ok := out["common"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no common", name)
		}
		if name == "browse" && common["inn_id"] != float64(17562) {
			t.Errorf("browse common inn_id = %v, want 17562", common["inn_id"])
		}
		if name == "substitutes" {
			if common["inn_id"] != float64(326) {
				t.Errorf("substitutes common = %v, want inn_id 326", common)
			}
			if common["group_id"] != float64(308) {
				t.Errorf("substitutes common = %v, want group_id 308", common)
			}
		}
		for _, p := range list(out, "products") {
			row := obj(p)
			if _, ok := row["url"]; ok || row["status"] != nil {
				t.Errorf("%s: row %v", name, row)
			}
		}
	}
}

func TestWhereToBuyProductIsBriefUnlessAsked(t *testing.T) {
	cs, _ := session(t)
	out := mustCall(t, cs, "where_to_buy", map[string]any{"drug_id": 13270, "limit": 1})
	p := obj(out["product"])
	for _, k := range []string{"drug_id", "brand_id", "form_id", "name", "pharmacies", "price_min", "price_max"} {
		if _, ok := p[k]; !ok {
			t.Errorf("brief product misses %s: %v", k, p)
		}
	}
	for _, k := range []string{"dosages", "packs", "countries", "manufacturers", "url", "inn", "group", "kind"} {
		if _, ok := p[k]; ok {
			t.Errorf("brief product carries %s", k)
		}
	}
	full := mustCall(t, cs, "where_to_buy", map[string]any{"drug_id": 13270, "limit": 1, "include_product": true})
	if _, ok := obj(full["product"])["manufacturers"]; !ok {
		t.Errorf("include_product: %v", full["product"])
	}
}

func TestInstructionOverviewAndSections(t *testing.T) {
	cs, _ := session(t)
	full := mustCall(t, cs, "drug_instruction", map[string]any{"drug_id": 13270, "sections": []any{"ПРОТИВОПОКАЗ", "способ"}})
	got := list(full, "sections")
	if len(got) != 2 {
		t.Fatalf("matched sections = %v", got)
	}
	if len(list(full, "icd10")) != 19 {
		t.Errorf("full mode drops icd10: %v", full["icd10"])
	}
	long := 0
	for _, s := range got {
		if n := len([]rune(obj(s)["text"].(string))); n > long {
			long = n
		}
	}
	if long <= 300 {
		t.Fatalf("full text is only %d runes", long)
	}

	over := mustCall(t, cs, "drug_instruction", map[string]any{"drug_id": 13270})
	if len(list(over, "sections")) < 5 || over["hint"] == nil || over["atc"] == nil {
		t.Fatalf("overview = %v", over)
	}
	cut := false
	for _, s := range list(over, "sections") {
		row := obj(s)
		if n := len([]rune(row["text"].(string))); n > 301 {
			t.Errorf("overview section %v holds %d runes", row["title"], n)
		}
		if row["full_chars"] != nil {
			cut = true
		}
	}
	if !cut {
		t.Error("nothing was shortened in the overview")
	}
	if over["icd10"] != nil {
		t.Errorf("a long icd10 list leaked into the overview: %v", over["icd10"])
	}

	capped := mustCall(t, cs, "drug_instruction", map[string]any{"drug_id": 13270, "sections": []any{"способ"}, "max_chars": 50})
	s := obj(list(capped, "sections")[0])
	if n := len([]rune(s["text"].(string))); n > 51 || s["full_chars"] == nil {
		t.Errorf("max_chars: %d runes, %v", n, s)
	}

	none := mustCall(t, cs, "drug_instruction", map[string]any{"drug_id": 13270, "sections": []any{"нет такого"}})
	if _, ok := none["sections"].([]any); !ok || len(list(none, "sections")) != 0 || none["hint"] == nil {
		t.Errorf("no match = %v", none)
	}
}

func TestEmptyListsAreArraysAndEmptyAnswersCarryHints(t *testing.T) {
	cs, _ := session(t)
	for _, kind := range []string{"regions", "places", "metro", "networks", "streets"} {
		out := mustCall(t, cs, "reference", map[string]any{"kind": kind, "query": "qqqzzz-no-such-name"})
		items, ok := out["items"].([]any)
		if !ok || len(items) != 0 {
			t.Errorf("%s: items = %#v", kind, out["items"])
		}
	}
	out := mustCall(t, cs, "reference", map[string]any{"kind": "networks", "query": "qqqzzz-no-such-name"})
	if h, _ := out["hint"].(string); !strings.Contains(h, "find_pharmacies") {
		t.Errorf("networks hint = %q", h)
	}
	out = mustCall(t, cs, "reference", map[string]any{"kind": "metro", "query": "авиа"})
	if out["hint"] != nil {
		t.Errorf("hint on a non-empty answer: %v", out["hint"])
	}

	if p, ok := mustCall(t, cs, "search_drugs", map[string]any{"query": "нурофен", "offset": 99})["products"].([]any); !ok || len(p) != 0 {
		t.Errorf("products past the end = %#v", p)
	}
}

func TestLatinSearchIsTransliterated(t *testing.T) {
	cs, site := session(t)
	out := mustCall(t, cs, "search_drugs", map[string]any{"query": "sedzharo"})
	if len(list(out, "products")) == 0 {
		t.Fatalf("no results: %v", out)
	}
	if h, _ := out["hint"].(string); !strings.Contains(h, "седжаро") {
		t.Errorf("hint = %q", h)
	}
	// one search as typed, one transliterated; the card requests after them fill ids of unavailable rows
	var searches []string
	for _, r := range site.Requests() {
		if strings.HasPrefix(r, "/tovary?") {
			searches = append(searches, r)
		}
	}
	if len(searches) != 2 {
		t.Errorf("search requests = %v", searches)
	}

	none := mustCall(t, cs, "search_drugs", map[string]any{"query": "zzzqq"})
	if p, ok := none["products"].([]any); !ok || len(p) != 0 {
		t.Errorf("products = %#v", none["products"])
	}
	if h, _ := none["hint"].(string); !strings.Contains(h, "Cyrillic") {
		t.Errorf("hint = %q", h)
	}

	nuro := mustCall(t, cs, "search_drugs", map[string]any{"query": "nurofen"})
	if h, _ := nuro["hint"].(string); !strings.Contains(h, "нурофен") {
		t.Errorf("nurofen hint = %q, want Cyrillic suggestion", h)
	}
}

func TestToolDescriptionsExplainTheNewShape(t *testing.T) {
	cs, _ := session(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	desc := map[string]string{}
	for _, tool := range res.Tools {
		desc[tool.Name] = tool.Description
	}
	for _, n := range []string{"search_drugs", "browse", "substitutes"} {
		d := desc[n]
		if !strings.Contains(d, "x-1/x-<drug_id>/ceni") || !strings.Contains(d, "common") || !strings.Contains(d, "available_only") {
			t.Errorf("%s description: %s", n, d)
		}
		if strings.Contains(d, "available=false") {
			t.Errorf("%s still explains available=false", n)
		}
	}
	if d := desc["pharmacy_price_list"]; !strings.Contains(d, "trade name") || !strings.Contains(d, "not by active substance") {
		t.Errorf("pharmacy_price_list description: %s", d)
	}
}
