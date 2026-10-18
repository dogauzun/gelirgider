export const trSummary = {
  'summary.title': 'Özet',
  'summary.totalValue': 'Toplam değer',
  'summary.cash': 'Nakit',
  'summary.unrealizedPl': 'Gerçekleşmemiş K/Z',
  'summary.realizedPl': 'Gerçekleşmiş K/Z',
  // A closed position's realized P/L over the cost of the shares it sold.
  'summary.realizedPlPercent': 'K/Z %',
  // The currency move on foreign cash and on the cost of foreign positions,
  // which neither P/L figure holds.
  'summary.fxPl': 'Kur farkı',
  // The card under the return card: the range's return by cause.
  'summary.returnSplit.title': 'Getiri dağılımı',
  'summary.returnSplit.error': 'Getiri dağılımı yüklenemedi',
  'summary.returnSplit.price': 'Fiyat',
  'summary.returnSplit.income': 'Gelir',
  'summary.returnSplit.incomeHint': 'Net temettü ve faiz',
  'summary.returnSplit.fx': 'Kur farkı',
  'summary.returnSplit.cashFx': 'Nakit kur farkı',
  'summary.value': 'Değer',
  // Qualifies the percentage next to it: the return is measured against
  // what the positions cost, not against the total value.
  'summary.vsCost': 'maliyete göre',
  'summary.cost': 'Maliyet {amount}',
  'summary.positions': 'Pozisyonlar',
  // The positions table's switch between open and closed positions: closed
  // ones are sold down to zero, or paid a dividend with no holding.
  'summary.positionsView': 'Gösterilen pozisyonlar',
  'summary.open': 'Açık',
  'summary.closed': 'Kapalı',
  'summary.noOpenPositions': 'Açık pozisyon yok.',
  // The positions card's filter on symbol and name.
  'summary.searchPositions': 'Pozisyon ara',
  'summary.noMatchingPositions': 'Aramayla eşleşen pozisyon yok.',
  // A position row's menu: the transaction dialog prefilled,
  // the price dialog, and removing a manual price.
  'summary.rowMenu': '{symbol} işlemleri',
  'summary.buy': 'Al',
  'summary.sell': 'Sat',
  'summary.total': 'Toplam',
  'summary.positionsCount': '{count} pozisyon, {currency} olarak',
  'summary.symbol': 'Enstrüman',
  'summary.quantity': 'Adet',
  'summary.avgCost': 'Ort. maliyet',
  'summary.lastPrice': 'Fiyat',
  // Share of the total including cash, and the unrealized P/L column.
  'summary.weight': 'Ağırlık',
  'summary.pl': 'K/Z',
  // The move since the previous close: a positions column, a line
  // under the total value, and the tooltip naming the two dates it compares.
  'summary.dayChange': 'Günlük',
  'summary.dayChangeTitle': 'Önceki kapanış {from} → {to}',
  // The hint on Özet's Günlük cell: the date of the newest price it covers.
  'summary.dayAsOf': '{date} fiyatlarıyla',
  'summary.negativeQuantity': 'Negatif',
  // Badge on a position changed by a stock split; the label lists each split
  // as "12.05.2026 · ×2".
  'summary.split': 'Bölünme',
  'summary.splitLabel': 'Bölünme: {splits}',
  // Names the currencies left out of the totals, not the currency whose rate
  // is absent -- with reporting USD and no USD rate it is TRY that gets
  // excluded, and TRY never needs a rate of its own.
  'summary.missingRates':
    'Kur bulunamadığı için şu para birimlerindeki tutarlar {currency} toplamlarına dahil edilmedi: {currencies}',
  // The statistics card under the positions: what went in and came
  // out, and how long ago the first transaction was.
  'summary.stats.title': 'İstatistikler',
  'summary.stats.netContribution': 'Net katkı',
  'summary.stats.netContributionDetail': 'yatırılan {deposits} · çekilen {withdrawals}',
  'summary.stats.dividends': 'Temettü',
  'summary.stats.dividendsDetail': 'brüt {gross} · stopaj {tax}',
  'summary.stats.interest': 'Faiz',
  'summary.stats.fees': 'Komisyon',
  'summary.stats.firstTransaction': 'İlk işlem',
  'summary.stats.years': '{count} yıl',
  'summary.stats.months': '{count} ay',
  'summary.stats.thisMonth': 'bu ay',
  // The positions card's copy button: the holdings as a markdown
  // table and a prompt, to paste into any AI assistant.
  'summary.copyAi.button': 'AI için kopyala',
  'summary.copyAi.copied': 'Panoya kopyalandı',
  'summary.copyAi.failed': 'Panoya kopyalanamadı',
  'summary.copyAi.symbol': 'Sembol',
  'summary.copyAi.market': 'Piyasa',
  'summary.copyAi.quantity': 'Adet',
  'summary.copyAi.avgCost': 'Ort. maliyet',
  'summary.copyAi.lastPrice': 'Son fiyat',
  'summary.copyAi.value': 'Değer ({currency})',
  'summary.copyAi.weight': 'Ağırlık %',
  'summary.copyAi.pl': 'K/Z %',
  'summary.copyAi.cash': 'Nakit',
  'summary.copyAi.totals':
    'Toplam değer: {total} {currency} · Gerçekleşmemiş K/Z: {unrealized} {currency} · Gerçekleşmiş K/Z: {realized} {currency}',
  'summary.copyAi.prompt':
    'Tarafsız bir portföy analisti gibi davran. Yukarıdaki portföyü değerlendir: yoğunlaşmayı, para birimi ve piyasa dağılımını özetle, öne çıkan riskleri belirt ve portföy sahibinin düşünmesi gereken soruları sırala. Alım ya da satım önerisi verme. Yanıtını Türkçe yaz.',
  'summary.empty': 'Henüz pozisyonun veya nakit bakiyen yok.',
  'summary.emptyLink': 'İşlem eklemeye başla',
  'summary.emptyImport': 'Ekstre içe aktar',
  'summary.loading': 'Özet yükleniyor...',
  'summary.error': 'Özet yüklenemedi',
  'goal.set': 'Hedef koy',
  'goal.title': 'Hedef',
  'goal.amount': 'Hedef tutar',
  'goal.unit': 'Birim',
  'goal.unit.nominal': '{currency}',
  'goal.unit.real': 'TRY, bugünkü fiyatlarla',
  'goal.unit.usd': 'USD',
  'goal.date': 'Hedef tarih',
  'goal.save': 'Kaydet',
  'goal.delete': 'Hedefi sil',
  'goal.deleteConfirm': 'Hedef silinsin mi?',
  'goal.saved': 'Hedef kaydedildi',
  'goal.deleted': 'Hedef silindi',
  'goal.loading': 'Hedef yükleniyor...',
  'goal.loadError': 'Hedef yüklenemedi',
  'goal.error.amount': 'Tutar 0’dan büyük bir sayı olmalı.',
  'goal.error.targetDate': 'Hedef tarih bugünden sonraki bir ayda olmalı.',
  'goal.error.unit': 'Bu birim, raporlama para birimiyle kullanılamaz.',
  'goal.error.notFound': 'Hedef bulunamadı.',
  // The goal card on Özet.
  'projection.title': 'Hedef',
  'projection.reached': 'Bu hızla hedefe {month} içinde ulaşılır.',
  'projection.gap': 'Hedef için ayda {amount} daha gerekiyor.',
  'projection.tooLittleHistory': 'Tahmin için en az 6 aylık geçmiş gerekir.',
  'projection.targetPassed': 'Hedef tarih geçti. Tahmin görmek için yeni bir hedef koy.',
  'projection.error': 'Hedef tahmini yüklenemedi',
} as const
