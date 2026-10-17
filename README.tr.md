# gelirgider

English: [README.md](README.md)

Çok kullanıcılı portföy takip aracı. BIST, ABD hisseleri, TEFAS fonları, kripto ve altın.

- Backend: Go, stdlib `net/http`, SQLite (canlı ortamda da veritabanı olarak SQLite kullanılır, bkz.
  [ADR 0002](docs/adr/0002-production-datastore-and-migrations.md))
- Frontend: React + TypeScript + Vite
- Backend ve frontend ayrı ayrı dağıtılır

## Özellikler

- Kullanıcı başına birden fazla portföy, bir de hepsini toplayan Tüm portföyler görünümü.
- Çoklu para birimi: tutarlar TRY, USD ya da EUR olabilir ve her kullanıcının
  seçtiği raporlama para biriminde gösterilir.
- Getiri basit, zaman ağırlıklı (TWR) ya da para ağırlıklı (MWR) olarak hesaplanır.
- Karşılaştırma: bir portföyü USD, EUR, BIST 100, enflasyon (TÜFE), altın ya da
  portföyün kendi pozisyonlarından biriyle karşılaştır.
- Temettüler ve hisse bölünmeleri.
- Midas, Garanti BBVA ve Garanti BBVA Yatırım ekstrelerinden ve genel bir CSV
  dosyasından içe aktarma.
- Hesap menüsünden işlemleri dışa aktarma: portföy başına bir CSV içeren tek bir
  zip. Her CSV'yi içe aktarma sayfasında CSV biçimiyle kendi portföyüne aktar;
  portföyde zaten bulunan satırlar yinelenen olarak işaretlenir ve seçili
  gelmez. 5000 satırı aşan dosya reddedilir; büyük bir portföyü, portföy başına
  CSV dışa aktarımının `from` ve `to` filtreleriyle böl
  (`/api/portfolios/{id}/transactions.csv?from=YYYY-MM-DD&to=YYYY-MM-DD`). Dışa
  aktarım yalnızca işlemleri içerir ve yedek değildir; veritabanı yedeği için
  bkz. [deploy/RUNBOOK.md](deploy/RUNBOOK.md).
- Sağlayıcı API anahtarları, fiyat çekme takvimi, kayıt ve kullanıcılar için bir
  yönetim alanı.

Arayüz Türkçe (`tr`, varsayılan) ve İngilizce (`en`); dil seçiciyle değiştirilir.
Seçim yalnızca tarayıcıda saklanır.

## Ghostfolio ile karşılaştırma

[Ghostfolio](https://github.com/ghostfolio/ghostfolio) ile üç fark, README ve CHANGELOG dosyalarında yazdığı gibi.

| Özellik            | gelirgider                                                                                                 | Ghostfolio                                                                                                                                                                                 |
| ------------------ | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Getiri             | Basit, TWR ve MWR yan yana ([ADR 0011](docs/adr/0011-portfolio-returns.md))                                | README (3.88.0) ROAI'yi (ortalama yatırım üzerinden getiri) anar; CHANGELOG, 3.88.0'da ROI'yi deneysel olarak ekler                                                                        |
| Veritabanı         | SQLite ve tek bir backend ikili dosyası ([ADR 0002](docs/adr/0002-production-datastore-and-migrations.md)) | README (3.88.0) PostgreSQL ve Redis'i anar                                                                                                                                                 |
| Ekstre içe aktarma | Midas, Garanti BBVA ve Garanti BBVA Yatırım PDF ekstreleri ve genel CSV                                    | README (3.88.0) JSON içe aktarmayı belgeler; CHANGELOG, 3.84.2'de diğer aracı kurumların CSV dosyalarını ve CSV, PDF ile görsel dosyalarının Ghostfolio AI ile okunmasını (deneysel) ekler |

Ghostfolio bilgileri 2026-10-18 tarihinde, 3.88.0 sürümünde kontrol edildi.

gelirgider BIST'i, TEFAS fonlarını[^tefas], gram altını ve TÜFE karşılaştırmasını kapsar; bölünmeler ve temettüler otomatiktir[^auto].

[^tefas]: TEFAS varsayılan olarak kapalıdır, bkz. [Veri kaynakları](#veri-kaynakları).

[^auto]: BIST için İş Yatırım'dan, yalnızca içinde bulunulan takvim yılı; eski bedelsiz sermaye artırımları `-add-split` operatör bayrağıyla girilir ([ADR 0008](docs/adr/0008-splits.md)). ABD için Tiingo'dan, kurulumun Tiingo anahtarı gerekir. TEFAS, kripto ve altın için yok.

## Ekran görüntüleri

`-demo` örnek verisiyle oluşturulan Demo portföyü (bkz. [Geliştirme](#geliştirme)).

![Demo portföyünün Özet sayfası: getiri grafiği, toplamlar ve pozisyonlar](docs/screenshots/summary.png)

Özet: seçilen aralıktaki getiri, portföy toplamları ve pozisyonlar.

![Demo portföyünün Performans sayfası: aylık getiri ısı haritası ve zirveden düşüş grafiği](docs/screenshots/performance.png)

Performans: aylara ve yıllara göre getiriler ve zaman içinde zirveden düşüş.

![Demo portföyünün İşlemler sayfası: aylara göre gruplanmış alım satımlar, temettüler ve para yatırmalar](docs/screenshots/transactions.png)

İşlemler: her alım satım, temettü ve nakit hareketi; filtreler ve CSV dışa aktarımıyla.

## Belgeler

- [docs/ROADMAP.md](docs/ROADMAP.md): süren işler, sıradakiler ve kapsam dışı olanlar
- [docs/PRINCIPLES.md](docs/PRINCIPLES.md): yol gösterici ilkeler
- [docs/adr/](docs/adr/): mimari karar kayıtları
- [deploy/README.md](deploy/README.md): Docker Compose ile canlı ortama kurulum
- [deploy/RUNBOOK.md](deploy/RUNBOOK.md): çalışan bir kurulumu işletmek
- [frontend/README.md](frontend/README.md): frontend notları
- [CONTRIBUTING.md](CONTRIBUTING.md): nasıl katkı verilir, kontroller nasıl çalıştırılır ve pull request nasıl biçimlendirilir
- [SECURITY.md](SECURITY.md): güvenlik açığı bildirmek
- [CLAUDE.md](CLAUDE.md): bakımcıların ajan iş akışı kuralları

## Geliştirme

Gereksinimler: Go 1.26+ ve Node.js 22.22.2 ya da sonraki bir 22.x sürümü, 24.15.0
ya da sonraki bir 24.x sürümü veya 26.0.0 ve üstü. Node 23 ve 25 desteklenmez;
frontend test ve lint araçları Node 20'de çalışmaz.

ABD hisse fiyatları için Finnhub (son kapanış) ve Tiingo (günlük kapanış geçmişi)
API anahtarları, BIST 100 karşılaştırması için de bir TCMB EVDS API anahtarı
gerekir (<https://evds3.tcmb.gov.tr> adresinde ücretsiz hesap, anahtar profil
sayfasında). Kurulumdan sonra bunları Yönetim / Administration sayfasına gir;
geri kalan her şey anahtarsız çalışır.
TEFAS fon fiyatları varsayılan olarak kapalıdır: TEFAS uç noktası tefas.gov.tr'nin
robots.txt dosyasında yasaklıdır. Aynı yerden `tefas` kaynağını açana kadar günlük
takvim, açılıştaki eksik tamamlama ve geçmiş doldurma, `server -backfill-prices`,
"Tümünü doldur", bir işlem kaydedildikten ya da içe aktarıldıktan sonraki fiyat
çekme ve ekstre içe aktarmanın fon araması TEFAS'ı atlar. Kaynak kapalıyken onu
adıyla istemek yine de TEFAS'tan çeker: kaynağın "Şimdi çalıştır" ve "Geçmişi
doldur" düğmeleri, `server -fetch tefas` ve `go run ./cmd/symbols -market tefas`.

```
make setup
make dev
```

`make setup` frontend bağımlılıklarını `npm ci` ile kurar
(`frontend/package-lock.json` değiştiğinde yeniden çalıştır; bir git worktree'de
lockfile aynıysa ana checkout'un `node_modules` klasörünü kopyalar). `make dev` iki geliştirme sunucusunu da başlatır;
backend fiyatları yerleşik zamanlayıcısıyla çeker. Ctrl+C ikisini de durdurur.

- Backend: http://localhost:8080
- Frontend: http://localhost:5173
- API belgeleri (Swagger UI, yalnızca geliştirmede): http://localhost:8080/api/docs/

Geliştirme backend'i yalnızca `127.0.0.1` adresini dinler (`make dev` `HOST`
değişkenini ayarlar), bu yüzden yerel ağdaki diğer cihazlar ona doğrudan ulaşamaz.

İlk `make dev` geride iki dosya bırakır: `.env` dosyasına eklenen, üretilmiş bir
`SECRET_KEY` (sakla, kayıtlı sağlayıcı anahtarlarını o şifreler) ve
`backend/gelirgider.db` konumundaki geliştirme veritabanı. İkisi de gitignore'dadır.

`backend/apidocs/` içindeki API tanımı depoda hazır bulunur, bu yüzden `make dev`
onu olduğu gibi sunar. `swag` aracını yalnızca `make swagger` (tanımı yeniden
üretir, sonra değişikliği commit etmen gerekir) ve `make lint` (güncel olduğunu denetler) çalıştırır;
ilk çalıştırmalarında aracı indirmek için ağ erişimi gerekir.

Backend'i başka bir portta çalıştırmak için, örneğin ikinci bir checkout'un
yanında, `PORT` değişkenini ikisi için de ayarla: `PORT=8081 make dev`. Frontend
geliştirme sunucusu `/api` isteklerini o porta yönlendirir; kendi portu bir
sonraki boş porta kayar.

Boş bir veritabanında her sayfa `/setup` adresine yönlenir: yönetici hesabını
oluşturmak için backend çıktısında yazan `setup_token` değerini gir.

Ağ ya da API anahtarı olmadan dolu bir veritabanı için `make dev` öncesinde
`(cd backend && go run ./cmd/server -demo)` komutunu çalıştır: son iki yılı kapsayan
sentetik alım satımlar, temettüler, fiyatlar ve döviz kurlarıyla bir Demo portföyü
olan `demo@example.com` yöneticisini oluşturur ve şifreyi yazdırır. Zaten kullanıcısı
olan bir veritabanını reddeder.

`make screenshots`, [Ekran görüntüleri](#ekran-görüntüleri) altındaki resimleri
aynı demo örnek verisinden, fiyat çekmeleri kapalı, geçici bir veritabanında yeniden
üretir. Google Chrome (varsayılan yerde değilse `CHROME` değişkenini onun
çalıştırılabilir dosyasına ayarla) ve `sqlite3` komutu gerekir.

Göndermeden önce `make test`, `make lint` ve `make build-frontend` çalıştır; her birinin tam olarak ne çalıştırdığı
[docs/PRINCIPLES.md](docs/PRINCIPLES.md) #6'da yazar. Tüm hedefler için `make help`
çalıştır.

## Veri kaynakları

gelirgider piyasa verisini bu sağlayıcılardan çeker. Onlardan alınan sembol
listeleri ve test verileri `backend/` altında depoda bulunur.

| Sağlayıcı                                      | Ne için                                               | Erişim                                                                                                   |
| ---------------------------------------------- | ----------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Borsa İstanbul günlük bülteni                  | BIST kapanışları, BIST sembol listesi                 | Herkese açık                                                                                             |
| İş Yatırım sermaye artırımı ve temettü sayfası | BIST bölünmeleri ve temettüleri                       | Herkese açık                                                                                             |
| TEFAS                                          | Fon fiyatları, fon listesi                            | Varsayılan olarak kapalı: tefas.gov.tr'nin robots.txt dosyası uç noktayı yasaklar, yönetici açmayı seçer |
| TCMB günlük döviz kuru bülteni                 | USD ve EUR kurları                                    | Herkese açık                                                                                             |
| TCMB EVDS                                      | BIST 100 karşılaştırması                              | Ücretsiz hesap; her kurulum kendi anahtarını girer                                                       |
| TÜİK veri portalı                              | Enflasyon (TÜFE) karşılaştırması                      | Herkese açık; TÜİK kaynak gösterilmesini ister: Kaynak / Source: TÜİK                                    |
| Finnhub                                        | ABD son kapanış                                       | Her kurulum kendi anahtarını girer; kişisel kullanım için ücretsiz katman                                |
| Tiingo                                         | ABD günlük kapanış geçmişi                            | Her kurulum kendi anahtarını girer; kişisel kullanım için ücretsiz katman                                |
| BtcTurk                                        | Kripto ve gram altın fiyatları, kripto parite listesi | Herkese açık                                                                                             |
| Nasdaq Trader sembol dizini                    | ABD sembol listesi                                    | Herkese açık                                                                                             |

Altın sembol listesi elle yazılmıştır. Bir kurulumu çalıştıran kişi bu veriyi kendi
adına çeker ve açtığı ya da anahtarını kabul ettiği her sağlayıcının koşullarına
bağlıdır.

## Lisans

Copyright (C) 2026 Doğa Uzuncukoğlu

gelirgider özgür yazılımdır; GNU Affero General Public License, yalnızca 3. sürüm
(AGPL-3.0-only) ile lisanslanmıştır. Bkz. [LICENSE](LICENSE). Pakete dahil üçüncü
taraf yazılımların lisansları [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
dosyasındadır; `make notices` onu yeniden üretir.
