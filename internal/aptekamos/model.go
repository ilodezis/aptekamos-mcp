package aptekamos

// Product kinds. A drug is one exact product (brand, form and dosage, any pack size);
// a tradeform is a brand in one dosage form and covers all of its dosages.
const (
	KindDrug      = "drug"
	KindTradeform = "tradeform"
)

// Row kinds in pharmacy tables. AptekaMos folds the pharmacies of a chain into one network row;
// filtering by its network_id lists them one by one.
const (
	RowPharmacy = "pharmacy"
	RowNetwork  = "network"
)

// Features a pharmacy row can show.
const (
	FeatureInStock     = "in_stock"
	FeatureReserve     = "reserve"
	FeatureDelivery    = "delivery"
	Feature24h         = "open_24h"
	FeatureSale        = "sale"
	FeatureOnlineOrder = "online_order"
)

// Product is one entry of a search, browse or substitutes list.
type Product struct {
	Kind       string  `json:"kind"`
	DrugID     int     `json:"drug_id,omitempty"`
	BrandID    int     `json:"brand_id,omitempty"`
	FormID     int     `json:"form_id,omitempty"`
	Name       string  `json:"name"`
	INN        string  `json:"inn,omitempty"`
	INNID      int     `json:"inn_id,omitempty"`
	Group      string  `json:"group,omitempty"`
	GroupID    int     `json:"group_id,omitempty"`
	Pharmacies int     `json:"pharmacies"`
	PriceMin   float64 `json:"price_min,omitempty"`
	PriceMax   float64 `json:"price_max,omitempty"`
	Available  bool    `json:"available"`
	Status     string  `json:"status,omitempty"`
	URL        string  `json:"url"`
}

// Option is one choice of a filter: a pack size, country, manufacturer, metro station, network or street.
type Option struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// Card is the header of a drug or tradeform page: what the product is and where it is sold.
type Card struct {
	Kind          string   `json:"kind"`
	DrugID        int      `json:"drug_id,omitempty"`
	BrandID       int      `json:"brand_id,omitempty"`
	FormID        int      `json:"form_id,omitempty"`
	Name          string   `json:"name"`
	INN           string   `json:"inn,omitempty"`
	INNID         int      `json:"inn_id,omitempty"`
	Group         string   `json:"group,omitempty"`
	Pharmacies    int      `json:"pharmacies"`
	PriceMin      float64  `json:"price_min,omitempty"`
	PriceMax      float64  `json:"price_max,omitempty"`
	Dosages       []Option `json:"dosages,omitempty"`
	Packs         []Pack   `json:"packs,omitempty"`
	Countries     []Option `json:"countries,omitempty"`
	Manufacturers []Option `json:"manufacturers,omitempty"`
	URL           string   `json:"url"`

	productKey      string // encrypted id the basket takes
	substitutesPath string
}

// Row is one line of a pharmacy table: a single pharmacy or a folded network.
type Row struct {
	Kind       string   `json:"kind"`
	PharmacyID int      `json:"pharmacy_id,omitempty"`
	NetworkID  int      `json:"network_id,omitempty"`
	Name       string   `json:"name"`
	Address    string   `json:"address,omitempty"`
	Metro      string   `json:"metro,omitempty"`
	Phones     []string `json:"phones,omitempty"`
	PriceMin   float64  `json:"price_min,omitempty"`
	PriceMax   float64  `json:"price_max,omitempty"`
	PriceNote  string   `json:"price_note,omitempty"`
	Pharmacies int      `json:"pharmacies,omitempty"`
	Features   []string `json:"features,omitempty"`
	// FeatureCounts says, for a network row, how many of its pharmacies have each feature.
	FeatureCounts map[string]int `json:"feature_counts,omitempty"`
}

// BasketSummary is what the shopping list page says about the whole list.
type BasketSummary struct {
	Items      int     `json:"items"`
	Pharmacies int     `json:"-"` // said once, as the listing's own pharmacies
	PriceMin   float64 `json:"price_min,omitempty"`
	PriceMax   float64 `json:"price_max,omitempty"`
}

// Section is one titled part of a drug instruction.
type Section struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// Instruction is the leaflet of a drug as AptekaMos publishes it.
type Instruction struct {
	DrugID     int       `json:"drug_id"`
	Name       string    `json:"name"`
	INN        string    `json:"inn,omitempty"`
	Group      string    `json:"group,omitempty"`
	Sections   []Section `json:"sections"`
	ATC        []string  `json:"atc,omitempty"`
	ICD10      []string  `json:"icd10,omitempty"`
	Conditions []string  `json:"conditions,omitempty"`
	Sources    []string  `json:"sources,omitempty"`
	URL        string    `json:"url"`
}

// Pharmacy is a pharmacy's own card.
type Pharmacy struct {
	ID              int      `json:"pharmacy_id"`
	Name            string   `json:"name"`
	Address         string   `json:"address,omitempty"`
	Metro           string   `json:"metro,omitempty"`
	Directions      string   `json:"directions,omitempty"`
	Phones          []string `json:"phones,omitempty"`
	Hours           []string `json:"hours,omitempty"`
	OpenNow         *bool    `json:"open_now,omitempty"`
	OpenStatus      string   `json:"open_status,omitempty"`
	PriceListDate   string   `json:"price_list_date,omitempty"`
	PriceListItems  int      `json:"price_list_items,omitempty"`
	TakesOrders     bool     `json:"takes_orders"`
	OrderOptions    []string `json:"order_options,omitempty"`
	OrderConditions string   `json:"order_conditions,omitempty"`
	Lat             float64  `json:"lat,omitempty"`
	Lng             float64  `json:"lng,omitempty"`
	Open24h         bool     `json:"open_24h,omitempty"`
	URL             string   `json:"url"`
}

// PriceItem is one line of a pharmacy's price list or offer: the exact pack and its price.
// A pharmacy can hold the same pack at several prices (batches; the site does not say why):
// those collapse into one item with the lowest price and all of them in Prices.
// Units and UnitPrice are set when the name says "№N" with N above one.
type PriceItem struct {
	Name      string    `json:"name"`
	Price     float64   `json:"price"`
	Prices    []float64 `json:"prices,omitempty"`
	Units     int       `json:"units,omitempty"`
	UnitPrice float64   `json:"unit_price,omitempty"`
}

// MapPharmacy is a pharmacy as the site's map knows it: with coordinates, and with the lowest
// price when the map was opened for a product.
type MapPharmacy struct {
	ID            int      `json:"pharmacy_id"`
	Name          string   `json:"name"`
	Address       string   `json:"address,omitempty"`
	Metro         string   `json:"metro,omitempty"`
	Directions    string   `json:"directions,omitempty"`
	Phones        []string `json:"phones,omitempty"`
	Lat           float64  `json:"lat"`
	Lng           float64  `json:"lng"`
	Open24h       bool     `json:"open_24h,omitempty"`
	OpenStatus    string   `json:"open_status,omitempty"`
	PriceListDate string   `json:"price_list_date,omitempty"`
	OnlineOrder   bool     `json:"online_order,omitempty"`
	PriceMin      float64  `json:"price_min,omitempty"`
	DistanceKm    float64  `json:"distance_km,omitempty"`
}

// Place is a region, city or district of the location tree.
type Place struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Places []Place `json:"places,omitempty"`
}

// Pack is one pack size of a product. In drug_card each pack carries its own pharmacy count and
// prices, because the card header spans every pack.
type Pack struct {
	Name         string  `json:"name"`
	ID           int     `json:"id"`
	Units        int     `json:"units,omitempty"`
	Pharmacies   int     `json:"pharmacies,omitempty"`
	PriceMin     float64 `json:"price_min,omitempty"`
	PriceMax     float64 `json:"price_max,omitempty"`
	UnitPriceMin float64 `json:"unit_price_min,omitempty"`
}
