package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ilodezis/aptekamos-mcp/internal/aptekamos"
)

// The tools answer in as few tokens as they can: what is the same in every row is said once,
// and a row says only what is true of it.

// productRow is one product of a search, browse or substitutes list. An unavailable product has a
// status and nothing else about stock or price; the site's own link is dropped, the id is enough.
type productRow struct {
	Kind       string  `json:"kind,omitempty"`
	DrugID     int     `json:"drug_id,omitempty"`
	BrandID    int     `json:"brand_id,omitempty"`
	FormID     int     `json:"form_id,omitempty"`
	Name       string  `json:"name"`
	INN        string  `json:"inn,omitempty"`
	INNID      int     `json:"inn_id,omitempty"`
	Group      string  `json:"group,omitempty"`
	GroupID    int     `json:"group_id,omitempty"`
	Pharmacies int     `json:"pharmacies,omitempty"`
	PriceMin   float64 `json:"price_min,omitempty"`
	PriceMax   float64 `json:"price_max,omitempty"`
	Status     string  `json:"status,omitempty"`
}

// productCommon holds the fields every returned row shares.
type productCommon struct {
	Kind    string `json:"kind,omitempty"`
	INN     string `json:"inn,omitempty"`
	INNID   int    `json:"inn_id,omitempty"`
	Group   string `json:"group,omitempty"`
	GroupID int    `json:"group_id,omitempty"`
}

func shapeProducts(ps []aptekamos.Product) ([]productRow, *productCommon) {
	rows := make([]productRow, len(ps))
	for i, p := range ps {
		rows[i] = productRow{Kind: p.Kind, DrugID: p.DrugID, BrandID: p.BrandID, FormID: p.FormID, Name: p.Name,
			INN: p.INN, INNID: p.INNID, Group: p.Group, GroupID: p.GroupID, Status: p.Status}
		if p.Available {
			rows[i].Pharmacies, rows[i].PriceMin, rows[i].PriceMax = p.Pharmacies, p.PriceMin, p.PriceMax
		}
	}
	if len(rows) == 0 {
		return rows, nil
	}
	var c productCommon
	if shared(rows, func(r productRow) any { return r.Kind }) {
		c.Kind = rows[0].Kind
	}
	if shared(rows, func(r productRow) any { return r.INN }) {
		c.INN = rows[0].INN
	}
	if shared(rows, func(r productRow) any { return r.INNID }) {
		c.INNID = rows[0].INNID
	}
	if shared(rows, func(r productRow) any { return r.Group }) {
		c.Group = rows[0].Group
	}
	if shared(rows, func(r productRow) any { return r.GroupID }) {
		c.GroupID = rows[0].GroupID
	}
	for i := range rows {
		rows[i].Kind, rows[i].INN, rows[i].INNID, rows[i].Group, rows[i].GroupID = strip(rows[i], c)
	}
	if c == (productCommon{}) {
		return rows, nil
	}
	return rows, &c
}

// shared reports whether every row has the same non-empty value of a field.
func shared(rows []productRow, get func(productRow) any) bool {
	first := get(rows[0])
	if first == "" || first == 0 {
		return false
	}
	for _, r := range rows[1:] {
		if get(r) != first {
			return false
		}
	}
	return true
}

func strip(r productRow, c productCommon) (kind, inn string, innID int, group string, groupID int) {
	kind, inn, innID, group, groupID = r.Kind, r.INN, r.INNID, r.Group, r.GroupID
	if c.Kind != "" {
		kind = ""
	}
	if c.INN != "" {
		inn = ""
	}
	if c.INNID != 0 {
		innID = 0
	}
	if c.Group != "" {
		group = ""
	}
	if c.GroupID != 0 {
		groupID = 0
	}
	return
}

func onlyAvailable(ps []aptekamos.Product) []aptekamos.Product {
	out := make([]aptekamos.Product, 0, len(ps))
	for _, p := range ps {
		if p.Available {
			out = append(out, p)
		}
	}
	return out
}

// productBrief is the product header of where_to_buy: enough to chain calls and to read the answer.
type productBrief struct {
	DrugID     int     `json:"drug_id,omitempty"`
	BrandID    int     `json:"brand_id,omitempty"`
	FormID     int     `json:"form_id,omitempty"`
	Name       string  `json:"name"`
	Pharmacies int     `json:"pharmacies"`
	PriceMin   float64 `json:"price_min,omitempty"`
	PriceMax   float64 `json:"price_max,omitempty"`
}

func briefProduct(c *aptekamos.Card) any {
	if c == nil {
		return nil
	}
	return &productBrief{DrugID: c.DrugID, BrandID: c.BrandID, FormID: c.FormID, Name: c.Name,
		Pharmacies: c.Pharmacies, PriceMin: c.PriceMin, PriceMax: c.PriceMax}
}

// Leaflet shaping.

const (
	overviewChars = 300 // how much of each section the overview shows
	smallListJSON = 300 // icd10, conditions and sources join the overview when they weigh less than this
)

type instructionSection struct {
	Title     string `json:"title"`
	Text      string `json:"text"`
	FullChars int    `json:"full_chars,omitempty"` // the real length, when Text was cut
}

type instructionView struct {
	DrugID     int                  `json:"drug_id"`
	Name       string               `json:"name"`
	INN        string               `json:"inn,omitempty"`
	Group      string               `json:"group,omitempty"`
	GroupID    int                  `json:"group_id,omitempty"`
	ATC        []string             `json:"atc,omitempty"`
	ICD10      []string             `json:"icd10,omitempty"`
	Conditions []string             `json:"conditions,omitempty"`
	Sources    []string             `json:"sources,omitempty"`
	Sections   []instructionSection `json:"sections"`
	Hint       string               `json:"hint,omitempty"`
	URL        string               `json:"url"`
}

func cut(text string, limit int) (string, int) {
	r := []rune(text)
	if limit <= 0 || len(r) <= limit {
		return text, 0
	}
	return strings.TrimSpace(string(r[:limit])) + "…", len(r)
}

func small(list []string) bool {
	raw, _ := json.Marshal(list)
	return len([]rune(string(raw))) <= smallListJSON
}

// shapeInstruction gives an overview (every section shortened) when no sections are asked for,
// and the full text of the sections whose title contains one of the asked-for words otherwise.
func shapeInstruction(ins aptekamos.Instruction, sections []string, maxChars int) instructionView {
	v := instructionView{DrugID: ins.DrugID, Name: ins.Name, INN: ins.INN, Group: ins.Group, GroupID: ins.GroupID, ATC: ins.ATC, URL: ins.URL,
		Sections: []instructionSection{}}
	if len(sections) == 0 {
		limit := overviewChars
		if maxChars > 0 {
			limit = maxChars
		}
		var left []string
		for _, s := range ins.Sections {
			text, full := cut(s.Text, limit)
			v.Sections = append(v.Sections, instructionSection{Title: s.Title, Text: text, FullChars: full})
		}
		for _, l := range []struct {
			name string
			list []string
			dst  *[]string
		}{{"icd10", ins.ICD10, &v.ICD10}, {"conditions", ins.Conditions, &v.Conditions}, {"sources", ins.Sources, &v.Sources}} {
			switch {
			case len(l.list) == 0:
			case small(l.list):
				*l.dst = l.list
			default:
				left = append(left, l.name)
			}
		}
		v.Hint = "Sections are shortened (full_chars is the real length of a cut one). Ask for the full text with " +
			"sections=[...]: a word of the title is enough, e.g. sections=[\"дозиров\", \"противопоказ\"]."
		if len(left) > 0 {
			v.Hint += " " + strings.Join(left, ", ") + " come with sections=[...]."
		}
		return v
	}

	v.ICD10, v.Conditions, v.Sources = ins.ICD10, ins.Conditions, ins.Sources
	var titles []string
	for _, s := range ins.Sections {
		titles = append(titles, s.Title)
		for _, want := range sections {
			if want = strings.ToLower(strings.TrimSpace(want)); want != "" && strings.Contains(strings.ToLower(s.Title), want) {
				text, full := cut(s.Text, maxChars)
				v.Sections = append(v.Sections, instructionSection{Title: s.Title, Text: text, FullChars: full})
				break
			}
		}
	}
	if len(v.Sections) == 0 {
		v.Hint = fmt.Sprintf("No section title contains %q. Titles: %s.", strings.Join(sections, `", "`), strings.Join(titles, "; "))
	}
	return v
}

// Hints for answers that came back empty.

const hintCyrillic = "Nothing found. Write the product name in Russian (Cyrillic): the site does not understand Latin spellings."

const hintNotFound = "Nothing found. Check the spelling, or search by the active substance or another trade name."

func searchedAsTranslit(typed, spelled string) string {
	return fmt.Sprintf("Nothing matched %q as typed, so these results are for the Cyrillic spelling %q. "+
		"Write the product name in Russian to search exactly.", typed, spelled)
}

const hintNoNetwork = "No pharmacy chain matches. A lone pharmacy is not a chain: look it up with " +
	"find_pharmacies(pharmacy_query=...)."
