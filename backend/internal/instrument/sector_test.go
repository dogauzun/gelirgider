package instrument_test

import (
	"testing"

	"github.com/dogauzun/gelirgider/backend/internal/instrument"
)

func TestSectorKey(t *testing.T) {
	tests := []struct {
		name, market, raw, want string
	}{
		{"energy", "US", "Energy", "energy"},
		{"materials", "US", "Metals & Mining", "materials"},
		{"industrials", "US", "Machinery", "industrials"},
		{"consumer discretionary", "US", "Automobiles", "consumer_discretionary"},
		{"consumer staples", "US", "Food Products", "consumer_staples"},
		{"health care", "US", "Pharmaceuticals", "health_care"},
		{"financials", "US", "Banking", "financials"},
		{"information technology", "US", "Technology", "information_technology"},
		{"communication services", "US", "Telecommunication", "communication_services"},
		{"utilities", "US", "Utilities", "utilities"},
		{"real estate", "US", "Real Estate", "real_estate"},
		{"case folded", "US", "bANKING", "financials"},
		{"diacritic folded", "US", " BANKİNG ", "financials"},
		{"unmapped", "US", "Space Mining", "unknown"},
		{"empty", "US", "", "unknown"},
		{"market without a map", "CRYPTO", "Banking", "unknown"},
		{"bist energy", "BIST", "Petrol", "energy"},
		{"bist materials", "BIST", "Çimento", "materials"},
		{"bist industrials", "BIST", "Savunma", "industrials"},
		{"bist consumer discretionary", "BIST", "Otomotiv", "consumer_discretionary"},
		{"bist consumer staples", "BIST", "Gıda", "consumer_staples"},
		{"bist health care", "BIST", "Sağlık ve İlaç", "health_care"},
		{"bist financials", "BIST", "Bankacılık", "financials"},
		{"bist information technology", "BIST", "Teknoloji", "information_technology"},
		{"bist communication services", "BIST", "İletişim", "communication_services"},
		{"bist utilities", "BIST", "Elektrik Üretim", "utilities"},
		{"bist real estate", "BIST", "GYO", "real_estate"},
		{"bist trailing space", "BIST", "Meşrubat / İçecek ", "consumer_staples"},
		{"bist diacritics folded", "BIST", "BANKACILIK", "financials"},
		{"bist Diğer", "BIST", "Diğer", "unknown"},
		{"bist unmapped", "BIST", "Uzay Madenciliği", "unknown"},
		{"tefas serbest", "TEFAS", "Serbest Şemsiye Fonu", "fund_other"},
		{"tefas hisse senedi", "TEFAS", "Hisse Senedi Şemsiye Fonu", "fund_equity"},
		{"tefas değişken", "TEFAS", "Değişken Şemsiye Fonu", "fund_mixed"},
		{"tefas katılım", "TEFAS", "Katılım Şemsiye Fonu", "fund_other"},
		{"tefas fon sepeti", "TEFAS", "Fon Sepeti Şemsiye Fonu", "fund_other"},
		{"tefas borçlanma", "TEFAS", "Borçlanma Araçları Şemsiye Fonu", "fund_debt"},
		{"tefas para piyasası", "TEFAS", "Para Piyasası Şemsiye Fonu", "fund_money_market"},
		{"tefas kıymetli madenler", "TEFAS", "Kıymetli Madenler Şemsiye Fonu", "fund_precious_metals"},
		{"tefas karma", "TEFAS", "Karma Şemsiye Fonu", "fund_mixed"},
		{"tefas diacritics folded", "TEFAS", " PARA PİYASASI ŞEMSİYE FONU ", "fund_money_market"},
		{"tefas unlisted", "TEFAS", "Emeklilik Şemsiye Fonu", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instrument.SectorKey(tt.market, tt.raw); got != tt.want {
				t.Errorf("SectorKey(%q, %q) = %q, want %q", tt.market, tt.raw, got, tt.want)
			}
		})
	}
}

// The closed list of the page's sector dropdown (ddlSektor), "Diğer" apart.
func TestSectorKeyMapsEveryIsYatirimSector(t *testing.T) {
	names := []string{
		"Aracı Kurumlar", "Bankacılık", "Bilgisayar Toptancılığı", "Boya", "Cam", "Çimento",
		"Dayanıklı Tüketim", "Demir-Çelik Döküm", "Demir-Çelik Temel", "Deri Giyim",
		"Eğlence Hizmetleri", "Elektrik - Doğalgaz Dağıtım", "Elektrik Enerji Ürt.Teçh/Tesis Kurulum",
		"Elektrik Üretim", "Endüstriyel Makine -Teçhizat Üretim", "Endüstriyel Tekstil",
		"Fin.Kiralama ve Faktoring", "Gıda", "GYO", "Havayolları ve Hizm.", "Hayvancılık", "Holdingler",
		"İletişim", "İletişim Cihazları", "İnşaat Malzemeleri", "İnşaat- Taahhüt", "Kablo",
		"Kağıt Ürünleri", "Kırtasiye", "Kimyasal Ürün", "Madencilik", "Medya", "Meşrubat / İçecek ",
		"Mobilya", "Otomotiv", "Otomotiv Lastiği", "Otomotiv Parçası", "Pazarlama",
		"Perakande - Ticaret", "Petrol", "Sağlık ve İlaç", "Savunma", "Seramik", "Sigorta", "Spor",
		"Tarım Kimyasalları", "Teknoloji", "Tekstil Entegre", "Turizm", "Ulaştırma-Lojistik",
		"Varlık Yönetim", "Yatırım Ortaklıkları",
	}
	if len(names) != 52 {
		t.Fatalf("test list has %d names, want 52 (53 minus Diğer)", len(names))
	}
	for _, n := range names {
		if got := instrument.SectorKey("BIST", n); got == "unknown" {
			t.Errorf("SectorKey(BIST, %q) = unknown", n)
		}
	}
}
