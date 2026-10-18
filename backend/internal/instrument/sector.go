package instrument

import (
	"slices"
	"strings"
)

// sectorKeys are the 11 sector keys, without unknown: unknown is what a
// position shows when nothing is set, not something an admin pins.
var sectorKeys = []string{
	"energy", "materials", "industrials", "consumer_discretionary",
	"consumer_staples", "health_care", "financials", "information_technology",
	"communication_services", "utilities", "real_estate",
}

// IsSectorKey reports whether s is one of the 11 sector keys.
func IsSectorKey(s string) bool {
	return slices.Contains(sectorKeys, s)
}

// sectorUnknown is the key of an instrument with no sector data or a raw
// string no map knows.
const sectorUnknown = "unknown"

// sectorMaps maps a market's folded, lower-case raw sector strings to a sector
// key. A market without a map has no sector source yet and answers unknown.
var sectorMaps = map[string]map[string]string{
	"US":   finnhubSectors,
	"BIST": foldKeys(isYatirimSectors),
}

// foldKeys returns m with its keys folded the way SectorKey folds a raw
// string, so a map can be written with the source's own spelling.
func foldKeys(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(turkishFold.Replace(strings.TrimSpace(k)))] = v
	}
	return out
}

// isYatirimSectors maps the "Sektör" column of İş Yatırım's "Temel Değerler ve
// Oranlar" summary table (ADR 0008) to the 11 sector keys. The page has a
// closed list of 53 names; "Diğer" is left out on purpose and answers
// unknown. Holdingler, Pazarlama and Perakande - Ticaret are judgment calls.
var isYatirimSectors = map[string]string{
	"Petrol": "energy",

	"Boya":                "materials",
	"Cam":                 "materials",
	"Çimento":             "materials",
	"Demir-Çelik Döküm":   "materials",
	"Demir-Çelik Temel":   "materials",
	"Kağıt Ürünleri":      "materials",
	"Kimyasal Ürün":       "materials",
	"Madencilik":          "materials",
	"Seramik":             "materials",
	"Tarım Kimyasalları":  "materials",
	"Endüstriyel Tekstil": "materials",

	"Endüstriyel Makine -Teçhizat Üretim":    "industrials",
	"Elektrik Enerji Ürt.Teçh/Tesis Kurulum": "industrials",
	"Kablo":                "industrials",
	"İnşaat Malzemeleri":   "industrials",
	"İnşaat- Taahhüt":      "industrials",
	"Savunma":              "industrials",
	"Ulaştırma-Lojistik":   "industrials",
	"Havayolları ve Hizm.": "industrials",
	"Holdingler":           "industrials",
	"Pazarlama":            "industrials",

	"Dayanıklı Tüketim":  "consumer_discretionary",
	"Deri Giyim":         "consumer_discretionary",
	"Eğlence Hizmetleri": "consumer_discretionary",
	"Kırtasiye":          "consumer_discretionary",
	"Mobilya":            "consumer_discretionary",
	"Otomotiv":           "consumer_discretionary",
	"Otomotiv Lastiği":   "consumer_discretionary",
	"Otomotiv Parçası":   "consumer_discretionary",
	"Spor":               "consumer_discretionary",
	"Tekstil Entegre":    "consumer_discretionary",
	"Turizm":             "consumer_discretionary",

	"Gıda":                "consumer_staples",
	"Meşrubat / İçecek":   "consumer_staples",
	"Hayvancılık":         "consumer_staples",
	"Perakande - Ticaret": "consumer_staples",

	"Sağlık ve İlaç": "health_care",

	"Bankacılık":                "financials",
	"Aracı Kurumlar":            "financials",
	"Fin.Kiralama ve Faktoring": "financials",
	"Sigorta":                   "financials",
	"Varlık Yönetim":            "financials",
	"Yatırım Ortaklıkları":      "financials",

	"Teknoloji":               "information_technology",
	"Bilgisayar Toptancılığı": "information_technology",
	"İletişim Cihazları":      "information_technology",

	"İletişim": "communication_services",
	"Medya":    "communication_services",

	"Elektrik Üretim":             "utilities",
	"Elektrik - Doğalgaz Dağıtım": "utilities",

	"GYO": "real_estate",
}

// finnhubSectors maps the finnhubIndustry values of Finnhub's company profile
// (https://finnhub.io/docs/api/company-profile2) to the 11 sector keys. The
// values are Finnhub's own list; a value it adds later falls to unknown.
var finnhubSectors = map[string]string{
	"energy": "energy",

	"chemicals":                        "materials",
	"metals & mining":                  "materials",
	"paper & forest products":          "materials",
	"packaging":                        "materials",
	"aerospace & defense":              "industrials",
	"airlines":                         "industrials",
	"building":                         "industrials",
	"commercial services & supplies":   "industrials",
	"construction":                     "industrials",
	"electrical equipment":             "industrials",
	"industrial conglomerates":         "industrials",
	"logistics & transportation":       "industrials",
	"machinery":                        "industrials",
	"marine":                           "industrials",
	"professional services":            "industrials",
	"road & rail":                      "industrials",
	"trading companies & distributors": "industrials",
	"transportation infrastructure":    "industrials",

	"auto components":                  "consumer_discretionary",
	"automobiles":                      "consumer_discretionary",
	"distributors":                     "consumer_discretionary",
	"diversified consumer services":    "consumer_discretionary",
	"hotels, restaurants & leisure":    "consumer_discretionary",
	"household durables":               "consumer_discretionary",
	"leisure products":                 "consumer_discretionary",
	"retail":                           "consumer_discretionary",
	"textiles, apparel & luxury goods": "consumer_discretionary",

	"beverages":         "consumer_staples",
	"consumer products": "consumer_staples",
	"food products":     "consumer_staples",
	"tobacco":           "consumer_staples",

	"biotechnology":                  "health_care",
	"health care":                    "health_care",
	"life sciences tools & services": "health_care",
	"pharmaceuticals":                "health_care",

	"banking":            "financials",
	"financial services": "financials",
	"insurance":          "financials",

	"semiconductors": "information_technology",
	"technology":     "information_technology",

	"communications":    "communication_services",
	"media":             "communication_services",
	"telecommunication": "communication_services",

	"utilities": "utilities",

	"real estate": "real_estate",
}

// SectorKey maps a provider's raw sector string of a market to one of the 11
// sector keys, or unknown for an empty, unmapped or source-less input. It
// matches case-insensitively with Turkish diacritics folded. The mapping is
// code, not data: the stored string stays raw (ADR 0026).
func SectorKey(market, raw string) string {
	key := strings.ToLower(turkishFold.Replace(strings.TrimSpace(raw)))
	if k, ok := sectorMaps[market][key]; ok {
		return k
	}
	return sectorUnknown
}
