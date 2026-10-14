export const trPortfolios = {
  'portfolios.title': 'Portföyler',
  'portfolios.new': 'Yeni portföy',
  'portfolios.create': 'Oluştur',
  'portfolios.name': 'Ad',
  'portfolios.nameHint': 'En fazla 100 karakter. Aynı adda ikinci portföy açılamaz.',
  'portfolios.created': 'Portföy oluşturuldu',
  // The sub-header on the two portfolio pages: the label over the
  // name, the switcher and its menu.
  'portfolio.label': 'Portföy',
  'portfolio.all': 'Tüm portföyler',
  'portfolio.backToList': 'Portföylere dön',
  'portfolio.menu': 'Portföy menüsü',
  'portfolio.tabs': 'Portföy sekmeleri',
  'portfolios.rename': 'Yeniden adlandır',
  'portfolios.renamed': 'Portföy adı değiştirildi',
  'portfolios.delete': 'Sil',
  'portfolios.deleteConfirm': 'Bu portföyü silmek istediğine emin misin?',
  'portfolios.deleted': 'Portföy silindi',
  // Second confirmation, shown only after the server refuses the plain delete
  // and says how many transactions are in the way. It is the last
  // step before the rows are gone for good, so it states the count -- on the
  // button too, so the last click names what it destroys.
  'portfolios.deleteCascadeConfirm':
    'Bu portföyde {count} işlem var. Hepsi silinecek. Bu işlem geri alınamaz. Emin misin?',
  'portfolios.deleteCascadeButton': 'Portföyü ve {count} işlemi sil',
  'portfolios.share': 'Pay',
  'portfolios.trend': 'Eğilim',
  'portfolios.emptyTitle': 'Henüz portföyün yok',
  'portfolios.emptyBody':
    'Bir portföy oluştur, ilk işlemini ekle; değer ve kâr/zarar buradan takip edilir.',
  'portfolios.emptyCta': 'İlk portföyünü oluştur',
  'portfolios.error': 'Portföyler yüklenemedi',
  'portfolio.notFoundExplanation': 'Bu portföy silinmiş ya da hiç var olmamış olabilir.',
} as const
