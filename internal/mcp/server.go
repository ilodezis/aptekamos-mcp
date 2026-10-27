// Package mcp exposes the AptekaMos service as MCP tools.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ilodezis/aptekamos-mcp/internal/aptekamos"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ServerInstructions = "AptekaMos (aptekamos.ru) is a Russian drug-availability service: prices and stock of " +
	"thousands of pharmacies in Moscow and 64 other regions (Moscow is the default; pass region for others). " +
	"Ids: drug_id is one exact product (brand + form + dosage); brand_id + form_id is a brand in one form " +
	"across all its dosages; pharmacy_id, network_id and the filter ids come from the tools' answers and from " +
	"reference. Typical flow: search_drugs -> drug_card (dosages, packs, makers) -> where_to_buy or " +
	"nearest_pharmacies -> price_at_pharmacy / pharmacy_card. substitutes and browse find other brands of the " +
	"same substance; shopping_list finds pharmacies that have a whole list. Prices are in roubles and come " +
	"from the pharmacies' own price lists, so they can be hours old. The leaflet text is the site's reference, " +
	"not medical advice: do not pick prescription drugs or doses for anyone."

type Server struct {
	svc       *aptekamos.Service
	mcpServer *mcp.Server
}

func NewServer(svc *aptekamos.Service) *Server {
	s := &Server{
		svc: svc,
		mcpServer: mcp.NewServer(&mcp.Implementation{Name: "aptekamos", Version: "0.1.0"},
			&mcp.ServerOptions{Instructions: ServerInstructions}),
	}
	s.registerTools()
	return s
}

func (s *Server) MCPServer() *mcp.Server { return s.mcpServer }

func readOnly() *mcp.ToolAnnotations {
	no, yes := false, true
	return &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &yes}
}

// Schema building blocks.

type props map[string]any

func object(p props, required ...string) string {
	sch := map[string]any{"type": "object", "properties": p, "additionalProperties": false}
	if len(required) > 0 {
		sch["required"] = required
	}
	raw, err := json.Marshal(sch)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func merge(parts ...props) props {
	out := props{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

func intProp(desc string, min int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": min}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func idsProp(desc string) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": map[string]any{"type": "integer", "minimum": 1}, "maxItems": 50}
}

func pageProps(def, max int) props {
	return props{
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": max, "default": def, "description": fmt.Sprintf("Rows to return, %d at most.", max)},
		"offset": map[string]any{"type": "integer", "minimum": 0, "default": 0, "description": "Rows to skip."},
	}
}

var (
	regionProps = props{"region": strProp("Region: name, slug or id (\"spb\", \"Самарская область\", \"Екатеринбург\"); default Moscow and its oblast. See reference kind=regions.")}

	productProps = props{
		"drug_id":  intProp("Exact product (brand, form and dosage) from search_drugs or drug_card.", 1),
		"brand_id": intProp("Brand; with form_id covers every dosage of that form.", 1),
		"form_id":  intProp("Dosage form, used together with brand_id.", 1),
	}

	coordProps = props{
		"lat": map[string]any{"type": "number", "minimum": -90, "maximum": 90, "description": "Latitude of the point."},
		"lng": map[string]any{"type": "number", "minimum": -180, "maximum": 180, "description": "Longitude of the point."},
	}

	pharmacyFilterProps = props{
		"places":         idsProp("City or district ids (reference kind=places)."),
		"metro":          idsProp("Metro station ids (reference kind=metro)."),
		"networks":       idsProp("Pharmacy chain ids (reference kind=networks, or network_id of a network row)."),
		"streets":        idsProp("Street ids (reference kind=streets)."),
		"reserve":        boolProp("Only pharmacies that take reservations."),
		"delivery":       boolProp("Only pharmacies that deliver."),
		"open_24h":       boolProp("Only round-the-clock pharmacies."),
		"sale":           boolProp("Only pharmacies running a promotion."),
		"pharmacy_query": strProp("Part of a pharmacy name or address."),
	}

	offerFilterProps = merge(pharmacyFilterProps, props{
		"in_stock":     boolProp("Only offers in stock now (drop order-only ones)."),
		"preorder":     boolProp("Only pharmacies that bring it to the pharmacy on order."),
		"pack":         intProp("Pack size id from drug_card packs (e.g. 30 for №30).", 1),
		"manufacturer": intProp("Manufacturer id from drug_card manufacturers.", 1),
		"country":      intProp("Country id from drug_card countries.", 1),
		"price_min":    intProp("Lowest price, roubles.", 1),
		"price_max":    intProp("Highest price, roubles.", 1),
	})

	availableOnlyProp = props{"available_only": boolProp("Leave out products nobody sells now (those with a status); products_total then counts only what is left.")}

	sortProp = props{"sort": map[string]any{"type": "string", "enum": []string{"cheap", "expensive", "near", "name"},
		"description": "Row order. near needs lat and lng and sorts by distance (use nearest_pharmacies to get the distances)."}}
)

type productArgs struct {
	DrugID  int `json:"drug_id"`
	BrandID int `json:"brand_id"`
	FormID  int `json:"form_id"`
}

func (p productArgs) target() aptekamos.Target {
	return aptekamos.Target{DrugID: p.DrugID, BrandID: p.BrandID, FormID: p.FormID}
}

func (p productArgs) given() bool { return p.DrugID != 0 || p.BrandID != 0 || p.FormID != 0 }

type filterArgs struct {
	Places        []int   `json:"places"`
	Metro         []int   `json:"metro"`
	Networks      []int   `json:"networks"`
	Streets       []int   `json:"streets"`
	InStock       bool    `json:"in_stock"`
	Reserve       bool    `json:"reserve"`
	Delivery      bool    `json:"delivery"`
	Preorder      bool    `json:"preorder"`
	Open24h       bool    `json:"open_24h"`
	Sale          bool    `json:"sale"`
	Pack          int     `json:"pack"`
	Manufacturer  int     `json:"manufacturer"`
	Country       int     `json:"country"`
	PriceMin      int     `json:"price_min"`
	PriceMax      int     `json:"price_max"`
	PharmacyQuery string  `json:"pharmacy_query"`
	Sort          string  `json:"sort"`
	Lat           float64 `json:"lat"`
	Lng           float64 `json:"lng"`
}

func (f filterArgs) filters() aptekamos.Filters {
	return aptekamos.Filters{
		Places: f.Places, Metro: f.Metro, Networks: f.Networks, Streets: f.Streets,
		InStock: f.InStock, Reserve: f.Reserve, Delivery: f.Delivery, Preorder: f.Preorder, Open24h: f.Open24h, Sale: f.Sale,
		Pack: f.Pack, Manufacturer: f.Manufacturer, Country: f.Country, PriceMin: f.PriceMin, PriceMax: f.PriceMax,
		PharmacyQuery: f.PharmacyQuery, Sort: f.Sort, Lat: f.Lat, Lng: f.Lng,
	}
}

type pageArgs struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func page[T any](items []T, p pageArgs) []T {
	if p.Offset >= len(items) {
		return []T{}
	}
	end := p.Offset + p.Limit
	if end > len(items) {
		end = len(items)
	}
	return items[p.Offset:end]
}

// listingPage is a pharmacy table cut to the requested rows.
type listingPage struct {
	Product   any                      `json:"product,omitempty"`
	List      *aptekamos.BasketSummary `json:"list,omitempty"`
	Total     int                      `json:"pharmacies"`
	PriceNote string                   `json:"price_note,omitempty"`
	RowsTotal int                      `json:"rows_total"`
	Offset    int                      `json:"offset"`
	Rows      []aptekamos.Row          `json:"rows"`
	URL       string                   `json:"url"`
}

func pageListing(l aptekamos.Listing, p pageArgs, includeProduct bool) listingPage {
	product := briefProduct(l.Card)
	if includeProduct && l.Card != nil {
		product = l.Card
	}
	return listingPage{Product: product, List: l.Summary, Total: l.Total, PriceNote: l.PriceNote, RowsTotal: len(l.Rows), Offset: p.Offset, Rows: page(l.Rows, p), URL: l.URL}
}

// productsPage is a product list cut to the requested items.
type productsPage struct {
	Total     int            `json:"products_total"`
	Offset    int            `json:"offset"`
	Common    *productCommon `json:"common,omitempty"`
	PriceNote string         `json:"price_note,omitempty"`
	Products  []productRow   `json:"products"`
	Hint      string         `json:"hint,omitempty"`
	URL       string         `json:"url"`
}

// pageProducts cuts a product list to the requested rows. available_only filters before paging,
// so products_total counts what is given out; only the rows of the page get their missing brand_id/form_id read.
func (s *Server) pageProducts(ctx context.Context, l aptekamos.ProductList, err error, p pageArgs, region string, availableOnly bool, hint string) (any, error) {
	if err != nil {
		return nil, err
	}
	products := l.Products
	if availableOnly {
		products = onlyAvailable(products)
		if len(products) == 0 && len(l.Products) > 0 && hint == "" {
			hint = fmt.Sprintf("All %d products are unavailable now; drop available_only to see them.", len(l.Products))
		}
	}
	rows, common := shapeProducts(s.svc.FillIDs(ctx, page(products, p), region))
	return productsPage{Total: len(products), Offset: p.Offset, Common: common, PriceNote: l.PriceNote, Products: rows, Hint: hint, URL: l.URL}, nil
}

// productsNote is the tail of the descriptions of the tools that list products.
const productsNote = " Fields every row shares (kind, inn, inn_id, group, group_id) come once in common and are left out " +
	"of the rows. A row with a status (\"нет в наличии\"...) is a product nobody sells now: it has no " +
	"pharmacies or prices; pass available_only=true to leave such rows out. Rows carry no link: a product page is " +
	"https://aptekamos.ru/tovary/lekarstva/x-1/x-<drug_id>/ceni (the site redirects to the canonical address; " +
	"checked for drugs, supplements and medical devices)."

const rowsNote = "Rows are single pharmacies (pharmacy_id) or folded chains (network_id, with how many of its " +
	"pharmacies match and feature_counts); pass a network_id in networks to list that chain's pharmacies. " +
	"The site shows at most 250 rows per table; narrow with filters or use nearest_pharmacies for a location."

func (s *Server) registerTools() {
	addTool(s, &mcp.Tool{
		Name: "search_drugs",
		Description: "Search drugs and other pharmacy goods by trade name or active substance, in Russian " +
			"(\"нурофен\", \"ибупрофен\"). Returns exact products with drug_id, brand_id/form_id, active substance " +
			"(inn_id), group (group_id), how many pharmacies sell each and the price range. A name typed in Latin " +
			"letters only is retried in Cyrillic (the hint says so)." + productsNote,
	}, object(merge(props{"query": strProp("Name or substance, in Russian.")}, regionProps, availableOnlyProp, pageProps(30, 100)), "query"),
		func(ctx context.Context, a struct {
			pageArgs
			Query         string `json:"query"`
			Region        string `json:"region"`
			AvailableOnly bool   `json:"available_only"`
		}) (any, error) {
			l, hint, err := s.search(ctx, a.Query, a.Region)
			return s.pageProducts(ctx, l, err, a.pageArgs, a.Region, a.AvailableOnly, hint)
		})

	addTool(s, &mcp.Tool{
		Name: "drug_card",
		Description: "What a product is and where it stands: names, active substance, group, other dosages " +
			"(their drug_ids), pack sizes, countries and manufacturers (ids for where_to_buy filters), how many " +
			"pharmacies sell it and the price range. For an exact drug_id each pack size comes with its own pharmacy " +
			"count, price_min/price_max, units and unit_price_min (price per unit, packs of 2+), because the card's " +
			"top-level range spans all packs. Give drug_id, or brand_id + form_id for all dosages of a form.",
	}, object(merge(productProps, regionProps)),
		func(ctx context.Context, a struct {
			productArgs
			Region string `json:"region"`
		}) (any, error) {
			return s.svc.CardWithPacks(ctx, a.target(), a.Region)
		})

	addTool(s, &mcp.Tool{
		Name: "drug_instruction",
		Description: "The leaflet of a drug as AptekaMos publishes it: composition, indications, dosage, " +
			"contraindications, side effects, interactions, storage, ATC code, ICD-10 codes and sources. " +
			"A leaflet is 5-6k tokens, so by default you get an overview: every section's title and its first " +
			"300 characters (full_chars tells how long a shortened one really is). Ask for the full text of " +
			"the sections you need with sections=[...]; ICD-10 codes, conditions and sources come with it " +
			"(the overview has them only when short). Reference text, not medical advice.",
	}, object(props{
		"drug_id": intProp("Exact product.", 1),
		"sections": map[string]any{"type": "array", "maxItems": 12, "items": map[string]any{"type": "string", "minLength": 1},
			"description": "Words of section titles, case-insensitive (\"дозиров\", \"противопоказ\", \"побочн\"): the full text of every section whose title contains one of them."},
		"max_chars": intProp("Cut every section to this many characters (the overview shows 300 by default).", 1),
	}, "drug_id"),
		func(ctx context.Context, a struct {
			DrugID   int      `json:"drug_id"`
			Sections []string `json:"sections"`
			MaxChars int      `json:"max_chars"`
		}) (any, error) {
			ins, err := s.svc.Instruction(ctx, a.DrugID)
			if err != nil {
				return nil, err
			}
			return shapeInstruction(ins, a.Sections, a.MaxChars), nil
		})

	addTool(s, &mcp.Tool{
		Name: "where_to_buy",
		Description: "Pharmacies selling a product, with price, address, metro, phones and features " +
			"(in_stock, reserve, delivery, open_24h, sale, online_order). Filters: location, metro, chains, " +
			"streets, features, pack, manufacturer, country, price; sort by price or name. Without a pack filter " +
			"the prices cover every pack size (price_note lists them). product is a short header (ids, name, " +
			"pharmacy count, price range); include_product=true gives the full card (dosages, packs, makers). " + rowsNote,
	}, object(merge(productProps, regionProps, offerFilterProps, sortProp, coordProps, pageProps(20, 100),
		props{"include_product": boolProp("Give the whole product card (dosages, packs, countries, manufacturers) as product; default is a short header.")})),
		func(ctx context.Context, a struct {
			productArgs
			filterArgs
			pageArgs
			Region         string `json:"region"`
			IncludeProduct bool   `json:"include_product"`
		}) (any, error) {
			l, err := s.svc.Offers(ctx, a.target(), a.filters(), a.Region)
			if err != nil {
				return nil, err
			}
			return pageListing(l, a.pageArgs, a.IncludeProduct), nil
		})

	addTool(s, &mcp.Tool{
		Name: "nearest_pharmacies",
		Description: "Pharmacies closest to a point, with distance in km, address, metro, phones, opening " +
			"status and, when a product is given, its lowest price there. Covers every matching pharmacy, not just " +
			"the 250 rows of a table. Without a product: any pharmacy, with filters (open_24h, chains...).",
	}, object(merge(coordProps, productProps, regionProps, pharmacyFilterProps, props{
		"in_stock":     boolProp("Only offers in stock now."),
		"pack":         intProp("Pack size id from drug_card packs.", 1),
		"manufacturer": intProp("Manufacturer id from drug_card.", 1),
		"radius_km":    map[string]any{"type": "number", "minimum": 0, "maximum": 200, "description": "Drop pharmacies farther than this."},
		"limit":        map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 10, "description": "Pharmacies to return, 50 at most."},
	}), "lat", "lng"),
		func(ctx context.Context, a struct {
			productArgs
			filterArgs
			Region   string  `json:"region"`
			RadiusKm float64 `json:"radius_km"`
			Limit    int     `json:"limit"`
		}) (any, error) {
			var t *aptekamos.Target
			if a.productArgs.given() {
				tt := a.target()
				t = &tt
			}
			f := a.filters()
			return s.svc.Nearest(ctx, t, a.Lat, a.Lng, f, a.Region, a.Limit, a.RadiusKm)
		})

	addTool(s, &mcp.Tool{
		Name: "price_at_pharmacy",
		Description: "The exact packs of a product one pharmacy has (name with pack size, maker, country) " +
			"and their prices, with the pharmacy's contacts and hours. A pharmacy can hold one pack at several prices " +
			"(the site does not say why, probably different batches): such an item is listed once, price is the lowest " +
			"and prices has all of them. units and unit_price are given for packs of 2+.",
	}, object(merge(props{"pharmacy_id": intProp("Pharmacy.", 1)}, productProps, regionProps), "pharmacy_id"),
		func(ctx context.Context, a struct {
			productArgs
			PharmacyID int    `json:"pharmacy_id"`
			Region     string `json:"region"`
		}) (any, error) {
			return s.svc.OfferAt(ctx, a.PharmacyID, a.target(), a.Region)
		})

	addTool(s, &mcp.Tool{
		Name: "pharmacy_card",
		Description: "A pharmacy's card: address, metro, how to get there, phones, opening hours and whether " +
			"it is open now, price list date and size, whether it takes orders and on what terms, coordinates.",
	}, object(props{"pharmacy_id": intProp("Pharmacy.", 1)}, "pharmacy_id"),
		func(ctx context.Context, a struct {
			PharmacyID int `json:"pharmacy_id"`
		}) (any, error) {
			return s.svc.Pharmacy(ctx, a.PharmacyID)
		})

	addTool(s, &mcp.Tool{
		Name: "pharmacy_price_list",
		Description: "Search one pharmacy's whole price list by product name: every matching pack with its " +
			"price (items held at several prices are merged as in price_at_pharmacy). Handy to check a pharmacy for " +
			"several things at once. The search is by trade name, not by active substance: query=\"тирзепатид\" " +
			"finds nothing, \"седжаро\" finds the packs (get trade names from search_drugs).",
	}, object(merge(props{"pharmacy_id": intProp("Pharmacy.", 1), "query": strProp("Product name, in Russian.")}, pageProps(50, 200)), "pharmacy_id", "query"),
		func(ctx context.Context, a struct {
			pageArgs
			PharmacyID int    `json:"pharmacy_id"`
			Query      string `json:"query"`
		}) (any, error) {
			res, err := s.svc.PriceList(ctx, a.PharmacyID, a.Query)
			if err != nil {
				return nil, err
			}
			return map[string]any{"pharmacy_id": res.PharmacyID, "items_total": len(res.Items), "offset": a.Offset,
				"items": page(res.Items, a.pageArgs), "url": res.URL}, nil
		})

	addTool(s, &mcp.Tool{
		Name: "find_pharmacies",
		Description: "Pharmacies of a region without a product: by location, metro, chain, street, name or " +
			"address, and features (open_24h, reserve, delivery, sale). " + rowsNote,
	}, object(merge(regionProps, pharmacyFilterProps, props{"sort": map[string]any{"type": "string", "enum": []string{"name", "near"},
		"description": "Row order; near needs lat and lng."}}, coordProps, pageProps(20, 100))),
		func(ctx context.Context, a struct {
			filterArgs
			pageArgs
			Region string `json:"region"`
		}) (any, error) {
			l, err := s.svc.Pharmacies(ctx, a.filters(), a.Region)
			if err != nil {
				return nil, err
			}
			return pageListing(l, a.pageArgs, false), nil
		})

	addTool(s, &mcp.Tool{
		Name: "substitutes",
		Description: "Other brands and forms with the same active substance as a product (what the site " +
			"offers as a replacement; the product itself is left out), each with brand_id/form_id, pharmacy count " +
			"and price range. Like the site's own substance page, it does not list products withdrawn from " +
			"circulation (status \"Отсутствует в обороте\"), which search_drugs still shows." + productsNote,
	}, object(merge(productProps, regionProps, availableOnlyProp, pageProps(30, 100))),
		func(ctx context.Context, a struct {
			productArgs
			pageArgs
			Region        string `json:"region"`
			AvailableOnly bool   `json:"available_only"`
		}) (any, error) {
			l, err := s.svc.Substitutes(ctx, a.target(), a.Region)
			return s.pageProducts(ctx, l, err, a.pageArgs, a.Region, a.AvailableOnly, "")
		})

	addTool(s, &mcp.Tool{
		Name: "browse",
		Description: "All products of an active substance (kind=inn, id from inn_id), a pharmacological group " +
			"(kind=group, group_id) or a brand (kind=brand, brand_id: its dosages and forms). A group is its own node " +
			"of the site's tree and holds only the products filed under it (group 1159, for one, has tirzepatide and two " +
			"gliflozins, while metformin sits in other groups), so it is not a full list of analogues: take group_id from " +
			"a product (search_drugs, drug_card), do not guess it; for the same substance use kind=inn. The site's " +
			"substance page does not list products withdrawn from circulation (status \"Отсутствует в обороте\"), so " +
			"kind=inn can hold a few products fewer than search_drugs." + productsNote,
	}, object(merge(props{
		"kind": map[string]any{"type": "string", "enum": []string{"inn", "group", "brand"}, "description": "What id is."},
		"id":   intProp("inn_id, group_id or brand_id.", 1),
	}, regionProps, availableOnlyProp, pageProps(30, 100)), "kind", "id"),
		func(ctx context.Context, a struct {
			pageArgs
			Kind          string `json:"kind"`
			ID            int    `json:"id"`
			Region        string `json:"region"`
			AvailableOnly bool   `json:"available_only"`
		}) (any, error) {
			l, err := s.svc.Browse(ctx, a.Kind, a.ID, a.Region)
			return s.pageProducts(ctx, l, err, a.pageArgs, a.Region, a.AvailableOnly, "")
		})

	addTool(s, &mcp.Tool{
		Name: "shopping_list",
		Description: "Pharmacies that have every product of a list, with the price of the whole list there " +
			"and a summary (how many pharmacies, total price range). Each item is a drug_id or brand_id + form_id. " + rowsNote,
	}, object(merge(props{"items": map[string]any{"type": "array", "minItems": 1, "maxItems": 10,
		"description": "Products of the list.", "items": map[string]any{"type": "object", "properties": productProps, "additionalProperties": false}}},
		regionProps, offerFilterProps, sortProp, coordProps, pageProps(20, 100)), "items"),
		func(ctx context.Context, a struct {
			filterArgs
			pageArgs
			Items  []productArgs `json:"items"`
			Region string        `json:"region"`
		}) (any, error) {
			targets := make([]aptekamos.Target, len(a.Items))
			for i, it := range a.Items {
				targets[i] = it.target()
			}
			l, err := s.svc.ShoppingList(ctx, targets, a.filters(), a.Region)
			if err != nil {
				return nil, err
			}
			return pageListing(l, a.pageArgs, false), nil
		})

	addTool(s, &mcp.Tool{
		Name: "reference",
		Description: "Ids for filters: regions (all of them), places (cities and districts of a region), metro " +
			"stations, pharmacy chains (networks) and streets of a region. query narrows by part of the name.",
	}, object(merge(props{
		"kind":  map[string]any{"type": "string", "enum": []string{"regions", "places", "metro", "networks", "streets"}, "description": "What to list."},
		"query": strProp("Part of a name to keep."),
	}, regionProps), "kind"),
		func(ctx context.Context, a struct {
			Kind   string `json:"kind"`
			Query  string `json:"query"`
			Region string `json:"region"`
		}) (any, error) {
			return s.reference(ctx, a.Kind, a.Region, a.Query)
		})
}

func (s *Server) reference(ctx context.Context, kind, region, query string) (any, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	keep := func(name string) bool { return query == "" || strings.Contains(strings.ToLower(name), query) }
	var items any
	var err error
	switch kind {
	case "regions":
		out := []aptekamos.Region{}
		for _, r := range aptekamos.Regions() {
			if keep(r.Name) || keep(r.Slug) {
				out = append(out, r)
			}
		}
		items = out
	case "places":
		var all []aptekamos.Place
		all, err = s.svc.Places(ctx, region)
		out := []aptekamos.Place{}
		for _, p := range all {
			if keep(p.Name) {
				out = append(out, p)
				continue
			}
			var districts []aptekamos.Place
			for _, d := range p.Places {
				if keep(d.Name) {
					districts = append(districts, d)
				}
			}
			if len(districts) > 0 {
				out = append(out, aptekamos.Place{ID: p.ID, Name: p.Name, Places: districts})
			}
		}
		items = out
	case "metro":
		items, err = s.svc.Metro(ctx, region, query)
	case "networks":
		items, err = s.svc.Networks(ctx, region, query)
	case "streets":
		items, err = s.svc.Streets(ctx, region, query)
	default:
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	if err != nil {
		return nil, err
	}
	res := map[string]any{"kind": kind, "items": items}
	if kind == "networks" && reflect.ValueOf(items).Len() == 0 {
		res["hint"] = hintNoNetwork
	}
	return res, nil
}

// search looks a product up; a name in Latin letters only that finds nothing is tried once more
// spelled in Cyrillic. The hint says what happened when the answer is empty or was reached that way.
func (s *Server) search(ctx context.Context, query, region string) (aptekamos.ProductList, string, error) {
	l, err := s.svc.Search(ctx, query, region)
	if err != nil || len(l.Products) > 0 {
		return l, "", err
	}
	if aptekamos.IsLatinOnly(query) {
		spelled := aptekamos.Transliterate(strings.TrimSpace(query))
		if again, err := s.svc.Search(ctx, spelled, region); err == nil && len(again.Products) > 0 {
			return again, searchedAsTranslit(strings.TrimSpace(query), spelled), nil
		}
	}
	if aptekamos.IsLatinOnly(query) {
		return l, hintCyrillic, nil
	}
	return l, hintNotFound, nil
}

// addTool registers a tool whose arguments are validated against its schema (with defaults applied)
// and decoded into A before the handler runs. A panic in a handler becomes a tool error.
func addTool[A any](s *Server, t *mcp.Tool, schema string, h func(context.Context, A) (any, error)) {
	var sch jsonschema.Schema
	if err := json.Unmarshal([]byte(schema), &sch); err != nil {
		panic(fmt.Sprintf("tool %s: bad input schema: %v", t.Name, err))
	}
	resolved, err := sch.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
	if err != nil {
		panic(fmt.Sprintf("tool %s: bad input schema: %v", t.Name, err))
	}
	t.InputSchema = json.RawMessage(schema)
	t.Annotations = readOnly()

	s.mcpServer.AddTool(t, func(ctx context.Context, req *mcp.CallToolRequest) (res *mcp.CallToolResult, err error) {
		defer func() {
			if r := recover(); r != nil {
				res, err = toolError(fmt.Errorf("internal error in %s: %v", t.Name, r))
			}
		}()
		var args any
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return toolError(fmt.Errorf("invalid arguments: %w", err))
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		if err := resolved.ApplyDefaults(&args); err != nil {
			return toolError(fmt.Errorf("invalid arguments: %w", err))
		}
		if err := resolved.Validate(args); err != nil {
			return toolError(fmt.Errorf("invalid arguments: %w", err))
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return toolError(err)
		}
		var a A
		if err := json.Unmarshal(raw, &a); err != nil {
			return toolError(fmt.Errorf("invalid arguments: %w", err))
		}
		out, err := h(ctx, a)
		if err != nil {
			return toolError(err)
		}
		return toolJSON(out)
	})
}

func toolError(err error) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
}

func toolJSON(data any) (*mcp.CallToolResult, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return toolError(err)
	}
	return &mcp.CallToolResult{StructuredContent: json.RawMessage(raw), Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
}
