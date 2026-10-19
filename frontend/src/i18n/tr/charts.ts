export const trCharts = {
  // The Performans tab: the periodic returns heatmap.
  'performance.title': 'Performans',
  'performance.periods.title': 'Dönemsel getiri',
  'performance.periods.view': 'Gösterim',
  'performance.periods.portfolio': 'Portföy',
  'performance.periods.excess': 'Fark',
  'performance.periods.real': 'Reel',
  'performance.periods.year': 'Yıl',
  'performance.periods.partial': 'Kısmi dönem',
  'performance.periods.noData': 'Veri yok',
  'performance.periods.error': 'Dönemsel getiri yüklenemedi',
  // Every return is null (ADR 0011): no positive base, or cash below zero.
  'performance.periods.undefined':
    'Getiri hesaplanamıyor: dönem değersiz başlıyor ya da nakit bir gün eksiye düşüyor. Alımları karşılayan para girişlerini kaydet.',
  // The cash gaps behind a null return: the first day a
  // portfolio's cash went below zero and the shortfall per currency, and the
  // dialog that records them as deposits. noValue is the other reason, V_0 <= 0.
  'performance.cashGaps.line': 'Nakit {date} tarihinde eksiye düştü: {amounts} eksik.',
  'performance.cashGaps.cause':
    'Getiri hesaplanamıyor: alımları karşılayan para girişleri kayıtlı değil.',
  'performance.cashGaps.noValue': 'Getiri hesaplanamıyor: dönem değersiz başlıyor.',
  'performance.cashGaps.add': 'Eksik para girişlerini ekle',
  'performance.cashGaps.body':
    'Bu para girişleri kaydedilecek. İstersen İşlemler sayfasından tek tek silebilirsin.',
  'performance.cashGaps.confirm': 'Ekle',
  // Stored as the deposits' note, so the user can find them later.
  'performance.cashGaps.note': 'Eksik para girişi (otomatik)',
  'allocation.title': 'Dağılım',
  'allocation.count': '{count} kalem · nakit dahil',
  'allocation.countNoCash': '{count} kalem',
  'allocation.unavailable': 'Dağılım hesaplanamadı',
  'allocation.other': 'Diğer ({count})',
  'allocation.total': 'Toplam',
  'allocation.missingRates': '{count} pozisyon kur eksikliği nedeniyle dahil değil',
  'allocation.missingRatesCash': '{currencies} nakdi kur eksikliği nedeniyle dahil değil',
  'allocation.view.label': 'Dağılım görünümü',
  'allocation.view.symbol': 'Sembol',
  'allocation.view.market': 'Pazar',
  'allocation.view.currency': 'Para birimi',
  'allocation.view.assetClass': 'Varlık sınıfı',
  'allocation.view.sector': 'Sektör',
  'allocation.assetClass.equity': 'Hisse senedi',
  'allocation.assetClass.fund': 'Fon',
  'allocation.assetClass.commodity': 'Emtia',
  'allocation.assetClass.crypto': 'Kripto',
  'allocation.assetClass.cash': 'Nakit',
  'allocation.assetClass.deposit': 'Mevduat',
  'allocation.assetClass.unknown': 'Bilinmiyor',
  'allocation.setTargets': 'Hedef belirle',
  'allocation.points': 'p',
  'allocation.target': 'Hedef',
  'allocation.drift': 'Sapma',
  'allocation.targets.title': 'Hedef dağılım',
  'allocation.targets.hint': 'Her sınıf için yüzde gir; boş alan 0 sayılır. Toplam 100 olmalı.',
  'allocation.targets.sum': 'Toplam: %{sum}',
  'allocation.targets.invalid': 'Geçerli bir yüzde gir',
  'allocation.planContribution': 'Katkı planla',
  'sector.energy': 'Enerji',
  'sector.materials': 'Hammadde',
  'sector.industrials': 'Sanayi',
  'sector.consumer_discretionary': 'Tüketici ürünleri (zorunlu olmayan)',
  'sector.consumer_staples': 'Temel tüketim',
  'sector.health_care': 'Sağlık',
  'sector.financials': 'Finans',
  'sector.information_technology': 'Bilgi teknolojileri',
  'sector.communication_services': 'İletişim hizmetleri',
  'sector.utilities': 'Kamu hizmetleri',
  'sector.real_estate': 'Gayrimenkul',
  'sector.fund_money_market': 'Para piyasası fonu',
  'sector.fund_equity': 'Hisse senedi fonu',
  'sector.fund_debt': 'Borçlanma araçları fonu',
  'sector.fund_precious_metals': 'Kıymetli madenler fonu',
  'sector.fund_mixed': 'Karma / değişken fon',
  'sector.fund_other': 'Diğer fon',
  'sector.unknown': 'Bilinmiyor',
  // The Katkı card: profit and loss per position.
  'contribution.title': 'Katkı',
  'contribution.subtitle': 'pozisyon başına K/Z',
  'contribution.mode.label': 'Katkı ölçüsü',
  'contribution.mode.total': 'Toplam',
  'contribution.mode.day': 'Bugün',
  'contribution.mode.range': 'Dönem',
  'contribution.showAll': 'Tümünü göster',
  'contribution.empty': 'Gösterilecek kâr veya zarar yok',
  'contribution.other': 'Diğer / satılan',
  // The drawdown card on the Performans tab.
  'drawdown.title': 'Zirveden düşüş',
  'drawdown.subtitle': 'para giriş çıkışları hariç',
  'drawdown.max': 'En büyük',
  'drawdown.current': 'Şu an',
  'drawdown.error': 'Zirveden düşüş yüklenemedi',
  'drawdown.chart.summary': '{from} – {to} zirveden düşüş, en büyük {value}',
  // The goal projection chart: the 75th, 50th and 25th percentile paths.
  'projection.high': 'İyi senaryo',
  'projection.mid': 'Orta senaryo',
  'projection.low': 'Kötü senaryo',
  'projection.chart.summary': '{from} – {to} hedef tahmini, sonunda {value}',
  'chart.totalValue': 'Toplam değer',
  'chart.costBasis': 'Maliyet',
  'chart.invested': 'Yatırılan',
  'chart.empty': 'Grafik için yeterli veri yok.',
  'chart.error': 'Grafik yüklenemedi',
  // BarChart's aria-label.
  'chart.bars.summary': '{count} çubuk, en yüksek {label}: {value}',
  // ReturnBars: its aria-label, a null month in its tooltip, and
  // the Dönemsel getiri card's switch between the table and the bars.
  'chart.returnBars.summary': '{from} – {to} aylık getiri: {series}',
  'chart.returnBars.undefined': 'tanımsız',
  'chart.returnBars.view': 'Biçim',
  'chart.returnBars.view.heatmap': 'Isı haritası',
  'chart.returnBars.view.bars': 'Çubuk',
  // LineChart's aria-label: the date range, then the value line's
  // first and last value and the change between them; range alone when the
  // value line has no point.
  'chart.line.summary': '{from} – {to}, {label} {first} → {last} ({change})',
  'chart.line.range': '{from} – {to}',
  // Tooltip rows for trade markers on the position chart.
  'chart.marker.buy': 'Alış',
  'chart.marker.sell': 'Satış',
  'chart.marker.dividend': 'Temettü',
  'chart.marker.split': 'Bölünme',
  // Tooltip mark on benchmark points past the last published TÜFE month
  // (ADR 0012).
  'chart.benchmark.estimated': 'tahmini',
  // The chart header's benchmark selector and figures (ADR 0012). Against
  // TÜFE the excess is the inflation-adjusted return, so it reads "reel".
  'chart.benchmark.label': 'Karşılaştır',
  'chart.benchmark.none': 'Karşılaştırma yok',
  'chart.benchmark.USD': 'Dolar',
  'chart.benchmark.EUR': 'Euro',
  'chart.benchmark.XU100': 'BIST 100',
  'chart.benchmark.TUFE': 'Enflasyon (TÜFE)',
  'chart.benchmark.GOLD': 'Altın (gram)',
  'chart.benchmark.positions': 'Pozisyonlar',
  'chart.benchmark.excess': 'fark',
  'chart.benchmark.real': 'reel',
  'chart.benchmark.noData': 'bu aralık için veri yok',
  // Like summary.missingRates, but only some days in the range are affected,
  // and it names the currencies left out rather than the one whose rate is
  // absent.
  'chart.missingRates':
    'Kur bulunamadığı için bazı günlerde şu para birimlerindeki tutarlar {currency} toplamlarına dahil edilmedi: {currencies}',
  // Days before an instrument's first price: valued at cost there.
  'chart.missingPrices':
    'Fiyat bilinmiyor: bazı günlerde şu varlıklar ilk fiyatları gelene kadar maliyetinden sayıldı: {symbols}',
  // The period the chart header's change is measured over.
  'chart.rangeDelta.1m': 'son 1 ay',
  'chart.rangeDelta.3m': 'son 3 ay',
  'chart.rangeDelta.1y': 'son 1 yıl',
  'chart.rangeDelta.all': 'başlangıçtan beri',
  'chart.rangeDelta.ytd': 'bu yıl',
  // The chart header's return method selector and its percentage's tooltip
  // (ADR 0011).
  'chart.return.method': 'Getiri yöntemi',
  // The return card's heading, before the range in words.
  'chart.return.title': 'Getiri',
  'chart.return.simple': 'Basit',
  'chart.return.twr': 'Zaman ağırlıklı',
  'chart.return.mwr': 'Para ağırlıklı',
  'chart.return.simple.hint': 'Kazanç ÷ yatırılan para; para giriş çıkışları hariç',
  'chart.return.twr.hint':
    'Günlük getirilerin zincirlenmesi; para giriş çıkışlarının zamanlamasından bağımsız',
  'chart.return.mwr.hint': 'İç verim oranı; para giriş çıkışlarının zamanlaması dahil',
  'chart.return.undefined': 'Bu aralık için tanımsız',
  'chart.return.real': 'Reel getiri',
  'chart.return.realHint': "TÜFE'ye göre, TWR",
  // The return card's "% | ₺" switch and its percent line's legend entry:
  // the line is always time-weighted, so with another method it
  // says so and why.
  'chart.return.unit': 'Grafik birimi',
  'chart.return.portfolio': 'Portföy',
  'chart.return.portfolioTwr': 'Portföy (zaman ağırlıklı)',
  'chart.return.portfolioTwr.hint':
    'Çizgi para giriş çıkışlarından etkilenmeyen zaman ağırlıklı getiridir, bu yüzden seçilen yöntemle hesaplanan getiriden farklı bir değerde bitebilir.',
  // The chart header's return annualized, for ranges of a year or more.
  'chart.annualized': 'yıllık {value}',
  'chart.range.1m': '1A',
  'chart.range.3m': '3A',
  'chart.range.1y': '1Y',
  'chart.range.all': 'Tümü',
  'chart.range.ytd': 'YBİ',
  'chart.range.label': 'Grafik aralığı',
  'chart.range.custom': 'Özel',
  'chart.range.clear': 'Özel aralığı temizle',
  'chart.range.from': 'Başlangıç',
  'chart.range.to': 'Bitiş',
} as const
