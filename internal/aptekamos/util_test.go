package aptekamos

import (
	"encoding/base64"
	"testing"
)

func TestDecodeLink(t *testing.T) {
	enc := base64.StdEncoding.EncodeToString([]byte("https://aptekamos.ru/tovary/trd-meksidol-7863/frm-tabletki-34/kupit?so=1"))
	cases := map[string]string{
		enc: "/tovary/trd-meksidol-7863/frm-tabletki-34/kupit?so=1",
		"https://aptekamos.ru/magaziny/apteki/na-karte?mn=2&amp;duty=1": "/magaziny/apteki/na-karte?mn=2&duty=1",
		"/korzina?productids=0UeOe%2B%2B%2FD7k%3D":                      "/korzina?productids=0UeOe%2B%2B%2FD7k%3D",
		"tel:+74956091129": "tel:+74956091129",
		"":                 "",
		// base64 that does not decode to a URL stays as it is
		"TDiiyyooJU4=": "TDiiyyooJU4=",
		// base64 alphabet has "/" in it; such links must decode too
		"aHR0cHM6Ly9hcHRla2Ftb3MucnUvdG92YXJ5L2lubi16aGVsZXphLXN1bGZhdC0zMjYvdHJkLXRhcmRpZmVyb24tODgyNi9mcm0tdGFibGV0a2ktMzQvbWVkLXRhcmRpZmVyb24tdGFibGV0a2ktODBtZy0xMzI3MC9rdXBpdC96YW1lbmk/cG09bW9kYWwmYWlkPXJwYyZzc2lkPXNzaw==": "/tovary/inn-zheleza-sulfat-326/trd-tardiferon-8826/frm-tabletki-34/med-tardiferon-tabletki-80mg-13270/kupit/zameni?pm=modal&aid=rpc&ssid=ssk",
	}
	for in, want := range cases {
		if got := decodeLink(in); got != want {
			t.Errorf("decodeLink(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSegmentID(t *testing.T) {
	path := "/tovary/inn-zheleza-sulfat-326/trd-tardiferon-8826/frm-tabletki-34/med-tardiferon-tabletki-80mg-13270/kupit/zameni?pm=modal"
	for prefix, want := range map[string]int{"inn": 326, "trd": 8826, "frm": 34, "med": 13270, "org": 0, "phg": 0} {
		if got := segmentID(path, prefix); got != want {
			t.Errorf("segmentID(%s) = %d, want %d", prefix, got, want)
		}
	}
	if got := segmentID("/magaziny/org-apteka-xamovniki-27324/otzyv?pm=modal", "org"); got != 27324 {
		t.Errorf("org id = %d", got)
	}
	// a slug that merely contains the prefix inside it must not match
	if got := segmentID("/tovary/phg-preparaty-zheleza-308", "zheleza"); got != 0 {
		t.Errorf("inner match = %d", got)
	}
}

func TestDrugIDFromPath(t *testing.T) {
	cases := map[string]int{
		"/tovary/lekarstva/ibuprofen-333/nurofen-tabletki-200mg-11562/ceni":                    11562,
		"https://aptekamos.ru/tovary/bad/bak-set-12929/bak-set-forte-kapsuly-210mg-74953/ceni": 74953,
		"/tovary/lekarstva/zheleza-sulfat-326/tardiferon-tabletki-80mg-13270/instrukciya":      13270,
		"/korzina/trd-nurofen-7658/frm-tabletki-34/med-nurofen-tabletki-200mg-11562":           11562,
		"/tovary/trd-meksidol-7863/frm-tabletki-34/kupit":                                      0,
	}
	for in, want := range cases {
		if got := drugIDFromPath(in); got != want {
			t.Errorf("drugIDFromPath(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestQueryInt(t *testing.T) {
	link := "/tovary/trd-meksidol-7863/frm-tabletki-34/kupit/na-karte?rsrv=1&o=27646&duty=1&pm=modal"
	if got := queryInt(link, "o"); got != 27646 {
		t.Errorf("o = %d", got)
	}
	if got := queryInt(link, "mn"); got != 0 {
		t.Errorf("mn = %d", got)
	}
	if got := queryInt("/korzina?mn=131&aid=bsw", "mn"); got != 131 {
		t.Errorf("mn = %d", got)
	}
}

func TestParsePrices(t *testing.T) {
	cases := []struct {
		in       string
		min, max float64
	}{
		{"627р", 627, 627},
		{"286 ... 574р", 286, 574},
		{"1 045 ... 5 500", 1045, 5500},
		{"1 156р", 1156, 1156},
		{"2 250.00", 2250, 2250},
		{"от 1 045р", 1045, 1045},
		{"Нет в наличии", 0, 0},
		{"", 0, 0},
	}
	for _, c := range cases {
		min, max := parsePriceRange(c.in)
		if min != c.min || max != c.max {
			t.Errorf("parsePriceRange(%q) = %v, %v; want %v, %v", c.in, min, max, c.min, c.max)
		}
	}
}

func TestParseCount(t *testing.T) {
	for in, want := range map[string]int{"5 117": 5117, "Аптеки (842)": 842, "4800": 4800, "": 0, "—": 0} {
		if got := parseCount(in); got != want {
			t.Errorf("parseCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSplitPhones(t *testing.T) {
	got := splitPhones("+7(499)136-02-33;+7(495)231-16-97 доб. 4880...")
	want := []string{"+7(499)136-02-33", "+7(495)231-16-97 доб. 4880"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("splitPhones = %q, want %q", got, want)
	}
	if got := splitPhones(""); len(got) != 0 {
		t.Errorf("empty = %q", got)
	}
}

func TestDistanceKm(t *testing.T) {
	// Kremlin to the Khamovniki pharmacy on Efremova street: about 4.3 km.
	d := distanceKm(55.7520, 37.6175, 55.725137, 37.574207)
	if d < 4.0 || d > 4.6 {
		t.Errorf("distance = %v km", d)
	}
	if d := distanceKm(55.75, 37.61, 55.75, 37.61); d != 0 {
		t.Errorf("zero distance = %v", d)
	}
}

func TestTransliterate(t *testing.T) {
	for in, want := range map[string]string{
		"sedzharo": "седжаро", "Nurofen": "нурофен", "ozempic": "оземпик", "aspirin": "аспирин", "mexidol 5": "мексидол 5",
		"citramon": "цитрамон", "shchi": "щи",
	} {
		if got := Transliterate(in); got != want {
			t.Errorf("Transliterate(%q) = %q, want %q", in, got, want)
		}
	}
	if !IsLatinOnly("sedzharo 5mg") || IsLatinOnly("седжаро") || IsLatinOnly("Sedzharo седжаро") || IsLatinOnly("123") {
		t.Error("IsLatinOnly")
	}
}
