# ADR 0018: Garanti BBVA statements

Status: accepted (GLG-694, 2026-10-01); amended 2026-10-01 (GLG-729,
brokerage fee and transfer rows, wrapped descriptions); amended 2026-10-02
(GLG-730, brokerage dividend rows and their transfers; GLG-727, pre-rebrand
brokerage statements); amended 2026-10-03 (GLG-818, a `Banka Takas` deposit
dated on the trade date of the buys it pays for); amended 2026-10-07
(GLG-1115, neutral motivation, statement dates removed); amended 2026-10-09
(GLG-1188, a cut or abbreviated fund name is matched against TEFAS fund
names)

## Context

Garanti BBVA is a common custodian of Turkish fund holdings. Garanti offers
no CSV export and no API, only two monthly PDFs sent by e-mail: one for the bank's
fund account (Garanti BBVA) and one for the brokerage account at Garanti
BBVA Yatırım. Entering a month of fund trades by hand is the work the
statement import of [ADR 0006](0006-broker-statement-import.md) exists to
remove.

Neither PDF has the Midas shape. `pdftext.Rows` returns one row per page for
both, each row hundreds of cells long, so a row is not a table row and the
Midas approach of reading a section's rows does not apply.

This ADR records the decisions of epic GLG-693 (Phase 22), taken with the
user on 2026-10-01. The layouts below were observed that day on two real
statements (kept outside the repository), a bank statement and a brokerage
statement. No customer data from them appears here (ADR 0006, PII).

## Layouts

### Garanti BBVA fund statement

- The period is one month, from a header cell matching
  `^(OCAK|ŞUBAT|MART|NİSAN|MAYIS|HAZİRAN|TEMMUZ|AĞUSTOS|EYLÜL|EKİM|KASIM|ARALIK) (\d{4}) YATIRIM EKSTRESİ$`.
- Numbers are Turkish: `.` for thousands, `,` for decimals. Dates are
  `dd.mm.yyyy`, times `hh:mm:ss`.
- Pages after the first start with 5 boilerplate cells (legal text and
  `Sayfa n / m`) and repeat the `YATIRIM FON HAREKETLERİ` title.

Sections, in order:

1. `YATIRIM İŞLEMLERİNDE KULLANILAN HESAP BAKİYELERİ`: the bank account
   balance. Ignored.
2. `YATIRIM FONU BAKİYELERİ`: header cells `Hesap`, `Fon Adı`, `Fon`,
   `Kodu`, `Para`, `Birimi`, `Adet`, `Fiyat`, `Tutar(YP)`, `Tutar(TL)`,
   then one record per fund until `TOPLAM`. A balance record is:
   - an account number cell, on the first record only;
   - 1 or 2 fund name cells;
   - the fund code, matching `^[A-Z0-9]{3}$`, immediately followed by a
     currency cell `TL`, `USD` or `EUR`;
   - the currency;
   - Adet (quantity), Fiyat (price);
   - one amount (TL) for a TL fund, two (YP and TL) for a foreign currency
     fund.
3. `YATIRIM FON HAREKETLERİ`: after a `Hesap No : ... - TL` cell, header
   cells `Tarih`, `Zaman`, `Fon Adı`, `İşlem`, `Tipi`, `İşlem Detay`,
   `Adet`, `Fiyat`, `Stopaj`, `Tutar`. A movement record is:
   - the date, immediately followed by the time;
   - 1 or 2 fund name cells, up to the type cell;
   - the type, `ALIŞ` or `SATIŞ`;
   - an optional detail cell (`NEMA` observed);
   - Adet, Fiyat, Stopaj, Tutar (gross).

A movement prints the fund's name, not its code, and truncates it at about
30 characters.

### Garanti BBVA Yatırım brokerage statement

- Every page repeats the broker's address, `www.garantibbvayatirim.com.tr`
  (`www.garantiyatirim.com.tr` before the 2019 rebrand), the statement date,
  the customer's name and address and `(Birim : 6)`. These cells are noise
  wherever they appear.
- Numbers are US: `,` for thousands, `.` for decimals, an optional leading
  sign. Dates are `dd/mm/yyyy`.
- Page titles: `Hesap Durumu`, `Menkul Kıymet Hareket Listesi`,
  `Hesap Ekstresi`.

Pages:

1. `Hesap Durumu`: `Tarih`, `:`, the date; then `Cari Bakiye`, `Mevcudu`,
   `Rezervi (T + 1)` and two numbers, the first being the cash balance.
   After `MEVCUT MENKUL KIYMETLER`, one holding record per position until
   `Hisse toplamı :`. A holding record is:
   - the type, matching `^[A-Z]{2}$` (`HS` is a share);
   - the symbol;
   - the name;
   - three numbers: quantity, market value in TL, reserved quantity.
2. `Menkul Kıymet Hareket Listesi`: `Dönemi`, `:`,
   `dd/mm/yyyy - dd/mm/yyyy`, the statement period.

   > Amended 2026-10-02 (GLG-739, GLG-767): the page also holds one trade
   > record per movement: the symbol, the name (absent on some statements,
   > like the holdings), the settlement date, the signed quantity, the type
   > and the price. Types seen: `HS Alış`, `HS Satış`, `Bedelsiz` (a bonus
   > issue), `HS-Virman Giriş` (a transfer in) and `HS-Virman Çıkış` (a
   > transfer out, also printed for a fractional share cashed out in the
   > ledger, GLG-767).

3. `Hesap Ekstresi`: the TRY ledger, header cells `Tarih`, `Açıklama`,
   `İşlem`, `Kalan`. A ledger record is 4 cells: value date, description,
   signed amount, running balance. Descriptions seen:
   - `Devir ... Cari TRY Bakiye Hareketleri`: the opening balance;
   - `dd/mm/yyyy Ref = n HA <price>x<qty> <SYMBOL>`: a buy, the leading date
     being the trade date;
   - `Matrah x Oran y Komisyon`: a commission;
   - `... Nolu Hesabın Banka Takas İşlemi`,
     `... Nolu Hesabın Banka MKK Masraf Transfer İşlemi`,
     `Diğer Bakiye Transferi-<account>`: a transfer between the bank
     account and the brokerage account;
   - `<AY> YYYY  MKK'ya Ödenen Ücret- ...`,
     `YYYY N. Dönem-Hesap bakım ücreti tahsilatı`, `Borsa Emir İptal ...`:
     a fee when negative, a refund (deposit) when positive.
   - `<SYMBOL>.E Temettü <qty> Adet Temettü Oranı : <rate>` (`.F` for an
     ETF): a cash dividend, amount = qty x rate / 100; the same text
     followed by `-<digits>-<digits>` moves it to the bank account.

   A long description wraps into a second cell; the two are joined with a
   space. Any other ledger date that completes no record, except the page
   header's date after the website cell, is a `bad_row` error quoting only
   the date (GLG-729).

   > Amended 2026-10-01 (GLG-728): the same description with `HS` in place
   > of `HA` is a sell, read like a buy. From May 2024 a commission reads
   > `Matrah x Oran y Komisyon Tutarı ve BSMV`.

   > Amended 2026-10-02 (GLG-740, GLG-768): more descriptions seen:
   >
   > - `n <qty> adet <SYMBOL> Halka Arz Tahsil`: the money for an IPO request
   >   of that quantity, coming in from the bank when positive and blocked for
   >   the request when negative;
   > - `<customer name> n <SYMBOL> için iade`: the part of an IPO request not
   >   allotted, coming back when positive and returning to the bank when
   >   negative. It starts with the customer's name, so it is never quoted;
   > - `<qty> adet <SYMBOL> bedeli kurum küsurat hesabından virman`: a
   >   fractional share cashed out, a sell of the fraction at the printed
   >   amount.
   >
   > The allotment itself is an `HS-Virman Giriş` row of the
   > `Menkul Kıymet Hareket Listesi`, priced from the request's payment and
   > refund (GLG-768).

   > Amended 2026-10-02 (GLG-763): 2018 statements print a capital letter
   > before the reference digits of a buy or a sell, `Ref = A12345`, and
   > move a dividend to the bank account with
   > `Temettü Transferi işlemi - <digits>-<digits> No'lu Hesap`, a transfer
   > like the ones above. The account-keeping fee can print more than one
   > space after the year, `YYYY  N. Dönem-Hesap bakım ücreti tahsilatı`.

## Decision

### Two broker keys, two packages

`garanti` is the bank's fund statement, parsed by
`backend/internal/statement/garanti` (GLG-696). `garanti_yatirim` is the
brokerage statement, parsed by `backend/internal/statement/garantiyatirim`
(GLG-697). Each is one package and one line in the `Parsers` map, as ADR
0006 laid out. Both are pure and partial: an unreadable record is a
`ParseError` and parsing goes on.

### A flat cell stream with section-title switches

Both parsers concatenate the page rows into one stream of cells and walk it,
switching state when a cell is a section or page title. A record is read by
its anchors, not by a fixed cell count: in the fund balances the code
followed by a currency, in the fund movements a date followed by a time, in
the ledger a date and the 4-cell shape. Header cells and the page
boilerplate are skipped wherever they appear, since pages break inside
sections.

### Fund statement

- **Netting per (date, fund, price).** Movements with the same date, fund
  and price are summed into one buy or sell of the net quantity, gross
  amount and stopaj.
- **One deposit or withdrawal per date.** The bank account the funds are
  bought from is outside the portfolio, so each date's net cash effect of
  the fund trades is booked as one deposit (net buy) or withdrawal (net
  sell) on that date.
- **Name to code only through the same statement's balances.** A movement's
  name is mapped to the fund code of the balance record whose name is equal
  to it or starts with it. The name is never matched across statements or
  against `instruments.name`: fund names change while the code stays, so a
  name is only good inside the document that printed it. A fund sold out
  within the month has no balance record, so its movements cannot be mapped
  and need the user's attention in the preview.

  > Amended 2026-10-01 (GLG-735): the parser still never matches a name
  > across statements. The user's own pick is remembered instead: a commit
  > may carry the instruments picked for unknown symbols, stored per (user,
  > broker, printed name), and a later preview resolves that name with the
  > pick when no instrument matches it, and marks the row
  > `resolved_by: "alias"`. A real match always wins over a stored pick.

  > Amended 2026-10-02 (GLG-766): a name is now matched across statements,
  > but only against the balance names of the user's own committed
  > statements, never against `instruments.name`. Each balance record's name
  > is carried on its `Holding`, and a commit remembers it with the
  > instrument its code resolved to. A later movement named by a name with no
  > pick takes that instrument when the remembered names it equals, starts
  > or is started by all name one instrument (the same-statement rule), with
  > `resolved_by: "name"`. A user's pick always wins. A renamed fund keeps
  > its old name learned until a statement prints the new one; a movement
  > name that matches two funds stays unknown. A statement's own balances
  > win over what was learned: a name they print for two funds resolves
  > nothing in its preview, and its commit forgets that name.

  > Superseded 2026-10-09 (GLG-1188): a name is now also matched against
  > `instruments.name`, for a garanti TEFAS row only, after picks and
  > learned names. A movement name none of those resolves, and that the
  > statement's own balances do not leave unresolved (one of its holding
  > codes, or a name they print for two funds), takes the one TEFAS fund
  > whose name it is cut or abbreviated from: word by word, `GPY` and `GP`
  > read as `GARANTİ PORTFÖY` and `1.`, `2.`, `3.` as `BİRİNCİ`, `İKİNCİ`,
  > `ÜÇÜNCÜ`, and each of its words starts the fund name's word in the same
  > place, case and punctuation ignored. A fund of the same words, else of
  > the same words with a cut last one, wins over the others; two funds of
  > the closest kind leave the row unknown. The row is marked
  > `resolved_by: "name"`, so the user can still override it. The match
  > lives in the shared resolver (`backend/internal/statement/names.go`),
  > so the parser stays pure.

- **`Statement.Market = "TEFAS"`.** Every fund on the statement is a TEFAS
  fund, so the trades carry the market hint and the reconciliation compares
  the balances with the portfolio's TEFAS positions (GLG-695).

### Brokerage statement

- **Trade date inside the period, else value date.** An `HA` row is dated on
  the trade date its description prints when that date is inside the
  statement period, else on the ledger's value date.

  > Amended 2026-10-01 (GLG-728): an `HS` sell is dated the same way.

- **Commission split pro rata.** A `Komisyon` row is split over the buys of
  the same ledger date in proportion to their amounts and added to their
  fees.

  > Amended 2026-10-01 (GLG-728), 2026-10-02 (GLG-747, GLG-753): the
  > `Komisyon Tutarı ve BSMV` row is a commission too. A day with buys and
  > sells has one commission per side, so a commission goes to the buys or
  > the sells of its ledger date whose amounts sum to its `Matrah` within a
  > kuruş, split pro rata among them (GLG-728). When both sides match, a side
  > that has not yet got a commission that day wins, then the one closest to
  > the `Matrah`, then the buys (GLG-747, GLG-753). When neither matches, or
  > the `Matrah` is unreadable, it is split pro rata over every trade of that
  > date. A commission on a date with no trade row stays a fee.

- **`Banka Takas` is a deposit or a withdrawal** by its sign.
- **A `Banka Takas` deposit is dated on the earliest trade date of the buys
  it pays for** (GLG-818): the buys of its value date. Dated on the value
  date, it would leave cash negative between the trade date and the
  settlement, and returns are null for any range with negative cash. Its
  amount is unchanged; withdrawals and sells keep their dates.
- **A dividend is booked at its printed amount with tax 0, and its transfer
  row is a withdrawal** (GLG-730). The printed amount looks like the payment
  after withholding, so `Tax: 0` does not mean the dividend was untaxed: no
  gross or tax is derived from a withholding rate, and cash matches the
  account.
- **The opening `Devir` row is skipped.** The cash it carries comes from the
  previous month.
- **Only `HA` is recognized.** The sell code has not been seen on a real
  statement yet, so any other description is a `ParseError` `unknown_row`
  until it is. Guessing it would book a sell as a buy.

  > Amended 2026-10-01 (GLG-728): `HS` is the sell code, seen on a real
  > statement. An `HS` row is read like an `HA` row and booked as a sell.
  > Any other description is still `unknown_row`.

- **Holdings and cash.** The `Hesap Durumu` holdings and cash balance feed
  the reconciliation of ADR 0006; the `Menkul Kıymet Hareket Listesi` gives
  only the period.

  > Amended 2026-10-02 (GLG-739, decision on GLG-731): the
  > `Menkul Kıymet Hareket Listesi` rows are read too, for what the ledger
  > does not show: a `Bedelsiz` row is a buy at price 0 and a priced
  > `HS-Virman Giriş` row a buy at its price. `HS Alış` and
  > `HS Satış` rows are skipped; the ledger books those trades with their
  > commission. Later tickets narrowed this: an `HS-Virman Giriş` of a
  > symbol with a `Halka Arz Tahsil` request in the ledger is the IPO
  > allotment, priced from the ledger's payment and refund, not the printed
  > price (GLG-740, GLG-768), and an `HS-Virman Çıkış` matching a ledger
  > fractional share cash-out is skipped (GLG-767).

## Recorded simplifications

- The fund statement's stopaj goes into the fee of the day's net trade,
  even when the net is a buy.
- The time of day is dropped: transactions have a date and no time column
  (ADR 0006).
- The `Menkul Kıymet Hareket Listesi` rows are not read. The ledger is the
  source of truth for trades and cash.

  > Amended 2026-10-02 (GLG-739): its bonus and transfer-in rows are read,
  > see the brokerage decisions above. The ledger stays the source of truth
  > for trades and cash.

- One portfolio per account is recommended. The brokerage cash
  reconciliation compares the whole portfolio's TRY cash with the
  statement's, so a portfolio that also holds the fund account or another
  broker's TRY cash will show a mismatch.

### Not in this ADR

- Prices taken from a statement. TEFAS publishes NAVs for the Garanti-only
  funds GJH and GNP (checked 2026-10-01), so the TEFAS fetcher prices them.
- The Garanti gold account.
- Assets other than shares at Garanti BBVA Yatırım.
- Remembering a fund's name across statements.

  > Amended 2026-10-01 (GLG-735): the user's pick for a printed name is
  > remembered, see the fund statement decision above.

- Other banks.
- Importing statements from e-mail.

## Consequences

- GLG-694 writes this ADR, the Phase 22 roadmap section and the amendments
  to ADR 0004, 0006 and 0012.
- GLG-695 adds `Statement.Market` to the reconciliation and registers the
  `garanti` and `garanti_yatirim` broker keys.
- GLG-696 and GLG-697 implement the two parsers above.
- GLG-698 keeps a tracked fund's name in step with TEFAS's `fonUnvan`
  (ADR 0004).
- GLG-699 lets the import create a TEFAS fund that TEFAS confirms (ADR 0006).
- GLG-700 adds the two broker options to the import page.
- GLG-701 and GLG-702 add an instrument as a benchmark (ADR 0012).
