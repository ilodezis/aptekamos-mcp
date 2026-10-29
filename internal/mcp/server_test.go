package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ilodezis/aptekamos-mcp/internal/aptekamos"
	"github.com/ilodezis/aptekamos-mcp/internal/fakesite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func session(t *testing.T) (*mcp.ClientSession, *fakesite.Site) {
	t.Helper()
	site := fakesite.New(t, "../aptekamos/testdata")
	c := aptekamos.NewClient()
	c.BaseURL = site.URL
	c.MinInterval = 0
	srv := NewServer(aptekamos.NewService(c))

	st, ct := mcp.NewInMemoryTransports()
	ctx := context.Background()
	go func() { _ = srv.MCPServer().Run(ctx, st) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, site
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("%s: empty content", name)
	}
	txt := res.Content[0].(*mcp.TextContent).Text
	if res.IsError {
		return map[string]any{"error": txt}, true
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(txt), &out); err != nil {
		t.Fatalf("%s: not JSON: %v\n%s", name, err, txt)
	}
	return out, false
}

func mustCall(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	out, isErr := call(t, cs, name, args)
	if isErr {
		t.Fatalf("%s(%v) failed: %v", name, args, out["error"])
	}
	return out
}

func list(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

func TestToolsAreListedReadOnly(t *testing.T) {
	cs, _ := session(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		if len(tool.Description) < 40 {
			t.Errorf("%s has a thin description", tool.Name)
		}
	}
	want := []string{"search_drugs", "drug_card", "drug_instruction", "where_to_buy", "nearest_pharmacies", "price_at_pharmacy",
		"pharmacy_card", "pharmacy_price_list", "find_pharmacies", "substitutes", "browse", "shopping_list", "reference"}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v\nwant %v", names, want)
	}
}

func TestSearchAndBrowse(t *testing.T) {
	cs, site := session(t)
	out := mustCall(t, cs, "search_drugs", map[string]any{"query": "нурофен"})
	products := list(out, "products")
	if len(products) != 17 {
		t.Fatalf("products = %d", len(products))
	}
	first := products[0].(map[string]any)
	if first["drug_id"].(float64) != 102376 || first["brand_id"].(float64) != 7658 {
		t.Errorf("first = %v", first)
	}
	if out["products_total"].(float64) != 17 || out["offset"].(float64) != 0 {
		t.Errorf("totals = %v %v", out["products_total"], out["offset"])
	}
	paged := mustCall(t, cs, "search_drugs", map[string]any{"query": "нурофен", "limit": 5, "offset": 15})
	if len(list(paged, "products")) != 2 || paged["products_total"].(float64) != 17 {
		t.Errorf("paged search = %d of %v", len(list(paged, "products")), paged["products_total"])
	}
	if _, isErr := call(t, cs, "search_drugs", map[string]any{"query": "нурофен", "region": "Атлантида"}); !isErr {
		t.Error("unknown region accepted")
	}
	if _, isErr := call(t, cs, "search_drugs", map[string]any{}); !isErr {
		t.Error("missing query accepted")
	}

	out = mustCall(t, cs, "browse", map[string]any{"kind": "group", "id": 308})
	if len(list(out, "products")) != 30 || out["products_total"].(float64) != 36 {
		t.Errorf("group products = %d of %v", len(list(out, "products")), out["products_total"])
	}
	if path, _ := site.Last(t); path != "/tovary/phg-x-308" {
		t.Errorf("request = %s", path)
	}
	if _, isErr := call(t, cs, "browse", map[string]any{"kind": "colour", "id": 1}); !isErr {
		t.Error("bad kind accepted")
	}
}

func TestCardInstructionSubstitutes(t *testing.T) {
	cs, _ := session(t)
	card := mustCall(t, cs, "drug_card", map[string]any{"drug_id": 13270})
	if card["name"] != "Тардиферон таблетки 80мг" || card["brand_id"].(float64) != 8826 || card["group_id"].(float64) != 308 {
		t.Errorf("card = %v", card)
	}
	if _, isErr := call(t, cs, "drug_card", map[string]any{"brand_id": 7863}); !isErr {
		t.Error("brand without form accepted")
	}
	ins := mustCall(t, cs, "drug_instruction", map[string]any{"drug_id": 13270, "sections": []any{"противопоказ"}})
	if len(list(ins, "sections")) != 1 || len(list(ins, "icd10")) != 19 || ins["group_id"].(float64) != 308 {
		t.Errorf("instruction = %v", ins)
	}
	sub := mustCall(t, cs, "substitutes", map[string]any{"drug_id": 13270})
	if len(list(sub, "products")) != 30 || sub["products_total"].(float64) != 33 {
		t.Errorf("substitutes = %d of %v", len(list(sub, "products")), sub["products_total"])
	}
}

func TestWhereToBuyPagingAndFilters(t *testing.T) {
	cs, site := session(t)
	out := mustCall(t, cs, "where_to_buy", map[string]any{
		"brand_id": 7863, "form_id": 34, "open_24h": true, "metro": []any{124, 32}, "sort": "cheap", "limit": 5, "offset": 10,
	})
	rows := list(out, "rows")
	if len(rows) != 5 || out["rows_total"].(float64) != 16 || out["offset"].(float64) != 10 || out["pharmacies"].(float64) != 451 {
		t.Errorf("paging = rows %d total %v offset %v pharmacies %v", len(rows), out["rows_total"], out["offset"], out["pharmacies"])
	}
	if out["product"].(map[string]any)["name"] != "Мексидол таблетки" {
		t.Errorf("product = %v", out["product"])
	}
	_, q := site.Last(t)
	if q.Get("me") != "124,32" || q.Get("duty") != "1" || q.Get("so") != "1" {
		t.Errorf("query = %v", q)
	}
	if _, isErr := call(t, cs, "where_to_buy", map[string]any{"drug_id": 13270, "limit": 1000}); !isErr {
		t.Error("limit above max accepted")
	}
	if _, isErr := call(t, cs, "where_to_buy", map[string]any{"drug_id": 13270, "sort": "near"}); !isErr {
		t.Error("near without coordinates accepted")
	}
	// offset past the end is an empty page, not an error
	out = mustCall(t, cs, "where_to_buy", map[string]any{"drug_id": 13270, "offset": 500})
	if len(list(out, "rows")) != 0 {
		t.Errorf("rows past the end = %d", len(list(out, "rows")))
	}
}

func TestNearestAndPharmacyTools(t *testing.T) {
	cs, site := session(t)
	out := mustCall(t, cs, "nearest_pharmacies", map[string]any{"lat": 55.8188, "lng": 37.4975, "drug_id": 13270, "limit": 2})
	ph := list(out, "pharmacies")
	if len(ph) != 2 || ph[0].(map[string]any)["pharmacy_id"].(float64) != 26833 {
		t.Errorf("nearest = %v", ph)
	}
	firstNear := ph[0].(map[string]any)
	if _, ok := firstNear["online_order"]; ok {
		t.Errorf("online_order should not be a separate bool: %v", firstNear)
	}
	if _, ok := firstNear["open_24h"]; ok {
		t.Errorf("open_24h should not be a separate bool: %v", firstNear)
	}
	if firstNear["open_now"] != false {
		t.Errorf("open_now = %v, want false", firstNear["open_now"])
	}
	if path, _ := site.Last(t); !strings.HasSuffix(path, "/ceni/na-karte") {
		t.Errorf("request = %s", path)
	}
	if _, isErr := call(t, cs, "nearest_pharmacies", map[string]any{"lat": 155.0, "lng": 37.0}); !isErr {
		t.Error("latitude out of range accepted")
	}

	card := mustCall(t, cs, "pharmacy_card", map[string]any{"pharmacy_id": 24084})
	if card["order_conditions"] != "Самовывоз в течение 72 часов" || card["takes_orders"] != true || len(list(card, "features")) == 0 {
		t.Errorf("pharmacy = %v", card)
	}
	offer := mustCall(t, cs, "price_at_pharmacy", map[string]any{"pharmacy_id": 27324, "brand_id": 7863, "form_id": 34})
	items := list(offer, "items")
	if len(items) != 1 || items[0].(map[string]any)["price"].(float64) != 627 {
		t.Errorf("offer = %v", offer)
	}
	pl := mustCall(t, cs, "pharmacy_price_list", map[string]any{"pharmacy_id": 27324, "query": "нурофен", "limit": 2})
	if len(list(pl, "items")) != 2 || pl["items_total"].(float64) != 3 {
		t.Errorf("price list = %v", pl)
	}
	l := mustCall(t, cs, "find_pharmacies", map[string]any{"region": "spb", "open_24h": true, "limit": 3})
	if len(list(l, "rows")) != 3 || l["pharmacies"].(float64) != 484 {
		t.Errorf("find_pharmacies = %v", l)
	}
	if path, _ := site.Last(t); path != "/spb/magaziny/apteki/na-karte" {
		t.Errorf("request = %s", path)
	}
}

func TestShoppingListAndReference(t *testing.T) {
	cs, site := session(t)
	out := mustCall(t, cs, "shopping_list", map[string]any{
		"items": []any{map[string]any{"drug_id": 13270, "pack": 30}, map[string]any{"brand_id": 7863, "form_id": 34}}, "limit": 4,
	})
	if out["pharmacies"].(float64) != 4604 || len(list(out, "rows")) != 4 {
		t.Errorf("shopping list = %v", out)
	}
	if path, q := site.Last(t); path != "/korzina" || len(strings.Split(q.Get("productids"), ",")) != 2 {
		t.Errorf("request = %s %v", path, q)
	}
	if _, isErr := call(t, cs, "shopping_list", map[string]any{"items": []any{}}); !isErr {
		t.Error("empty list accepted")
	}
	if _, isErr := call(t, cs, "shopping_list", map[string]any{"items": []any{map[string]any{"brand_id": 1}}}); !isErr {
		t.Error("item without form accepted")
	}

	regions := mustCall(t, cs, "reference", map[string]any{"kind": "regions"})
	if len(list(regions, "items")) != 65 {
		t.Errorf("regions = %d", len(list(regions, "items")))
	}
	metro := mustCall(t, cs, "reference", map[string]any{"kind": "metro", "query": "авиа"})
	if len(list(metro, "items")) != 2 {
		t.Errorf("metro = %v", metro)
	}
	places := mustCall(t, cs, "reference", map[string]any{"kind": "places", "query": "Москва"})
	if len(list(places, "items")) == 0 {
		t.Errorf("places = %v", places)
	}
	if _, isErr := call(t, cs, "reference", map[string]any{"kind": "planets"}); !isErr {
		t.Error("bad kind accepted")
	}
}

func TestPriceNotesAndPackPrices(t *testing.T) {
	cs, _ := session(t)

	card := mustCall(t, cs, "drug_card", map[string]any{"drug_id": 117477})
	packs := list(card, "packs")
	if len(packs) != 2 {
		t.Fatalf("packs = %v", packs)
	}
	four := packs[1].(map[string]any)
	if four["units"].(float64) != 4 || four["price_min"].(float64) != 2900 || four["unit_price_min"].(float64) != 725 || four["pharmacies"].(float64) != 2 {
		t.Errorf("pack №4 = %v", four)
	}

	out := mustCall(t, cs, "where_to_buy", map[string]any{"drug_id": 117477})
	if note, _ := out["price_note"].(string); !strings.Contains(note, "№4") {
		t.Errorf("where_to_buy note = %q", note)
	}
	out = mustCall(t, cs, "where_to_buy", map[string]any{"drug_id": 117477, "pack": 4})
	if _, has := out["price_note"]; has {
		t.Errorf("note with a pack filter: %v", out["price_note"])
	}

	for _, tool := range []struct {
		name string
		args map[string]any
	}{
		{"search_drugs", map[string]any{"query": "нурофен"}},
		{"substitutes", map[string]any{"drug_id": 13270}},
		{"browse", map[string]any{"kind": "group", "id": 308}},
	} {
		out := mustCall(t, cs, tool.name, tool.args)
		if note, _ := out["price_note"].(string); note == "" {
			t.Errorf("%s has no price_note", tool.name)
		}
		for _, p := range list(out, "products") {
			if _, has := p.(map[string]any)["price_note"]; has {
				t.Errorf("%s: price_note repeated per product", tool.name)
			}
		}
	}

	offer := mustCall(t, cs, "price_at_pharmacy", map[string]any{"pharmacy_id": 8528, "brand_id": 40203, "form_id": 27})
	var five map[string]any
	for _, it := range list(offer, "items") {
		m := it.(map[string]any)
		if strings.Contains(m["name"].(string), " 5мг ") {
			five = m
		}
	}
	if five == nil || five["price"].(float64) != 1490 || len(five["prices"].([]any)) != 2 || five["units"].(float64) != 4 || five["unit_price"].(float64) != 372.5 {
		t.Errorf("collapsed 5 mg = %v", five)
	}
}

func TestBrowseDescriptionExplainsGroups(t *testing.T) {
	cs, _ := session(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		switch tool.Name {
		case "browse":
			if !strings.Contains(tool.Description, "group_id") || !strings.Contains(tool.Description, "own node") {
				t.Errorf("browse description: %s", tool.Description)
			}
		case "price_at_pharmacy":
			if !strings.Contains(tool.Description, "several prices") {
				t.Errorf("price_at_pharmacy description: %s", tool.Description)
			}
		}
	}
}
