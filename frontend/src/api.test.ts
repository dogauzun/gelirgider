import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiFetch, apiUpload, rowError, shouldRetryQuery, translateError } from './api'

describe('unreachable server', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('turns the fetch rejection into a translated, retryable error but keeps aborts', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    const err = await apiFetch('/api/x').catch((e: unknown) => e)
    expect(err).toMatchObject({
      status: 0,
      message: expect.stringMatching(/^Sunucuya ulaşılamadı/),
    })
    expect(shouldRetryQuery(0, err)).toBe(true)
    await expect(apiUpload('/api/x', new FormData())).rejects.toMatchObject({ status: 0 })

    const abort = new DOMException('The operation was aborted.', 'AbortError')
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(abort))
    await expect(apiFetch('/api/x')).rejects.toBe(abort)
  })

  it('shows the network message when the proxy answers 502/503/504 without a JSON error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('<html>Bad Gateway</html>', { status: 502 })),
    )
    await expect(apiFetch('/api/x')).rejects.toMatchObject({
      status: 502,
      message: expect.stringMatching(/^Sunucuya ulaşılamadı/),
    })
  })
})

describe('rowError', () => {
  it('splits a commit row error into the row index and the field message', () => {
    expect(rowError('rows[3].date: must be within 2025-01')).toEqual({
      index: 3,
      rest: 'date: must be within 2025-01',
    })
    expect(rowError('date: must be within 2025-01')).toBeNull()
  })
})

describe('translateError', () => {
  it('maps known, field-prefixed and unknown server messages', () => {
    expect(translateError('invalid email or password')).toBe('E-posta veya şifre hatalı.')
    expect(translateError('email must be at most 254 characters')).toBe(
      'E-posta adresi en fazla 254 karakter olmalı.',
    )
    expect(translateError('fee: must be a non-negative number')).toMatch(/^Komisyon/)
    expect(
      translateError('fee: must be a non-negative number', 'transactions.withholdingTax'),
    ).toMatch(/^Stopaj/)
    expect(translateError('fee: must not exceed amount', 'transactions.withholdingTax')).toBe(
      'Stopaj tutardan büyük olamaz.',
    )
    expect(translateError('instrument_id: required for buy')).toMatch(/enstrüman/)
    expect(translateError('deposit rows are changed through the deposit routes')).toBe(
      'Mevduat işlemleri Mevduat sekmesinden değiştirilir.',
    )
    expect(translateError('rate: must be greater than 0 and at most 10')).toMatch(/Faiz oranı/)
    expect(translateError('goals depend on the reporting currency')).toBe(
      'Raporlama para birimini değiştirmeden önce hedeflerini sil; tutarları şu anki para birimindedir.',
    )
    // A currency that contradicts the instrument names its currency,
    // in a preview row and in a commit error.
    expect(translateError('currency: must match instrument currency (USD)')).toBe(
      'Para birimi enstrümanın para birimiyle (USD) aynı olmalı.',
    )
    expect(translateError('rows[0].currency: must match instrument currency (USD)')).toBe(
      '1. satır: Para birimi enstrümanın para birimiyle (USD) aynı olmalı.',
    )
    expect(translateError('file: larger than 5 MB')).toBe(
      "Dosya 5 MB'tan büyük. Küçült ya da böl ve tekrar dene.",
    )
    expect(translateError('file: not a PDF')).toBe('Dosya geçerli bir PDF değil.')
    expect(translateError('file: not a CSV')).toBe('Dosya geçerli bir CSV değil.')
    expect(translateError('file: not a readable CSV')).toBe('Dosya geçerli bir CSV değil.')
    expect(translateError('file: not a csv statement')).toBe(
      'Bu dosya bir işlem CSV dosyası değil. Başlık satırında tarih ve işlem türü sütunları olmalı.',
    )
    expect(translateError('file: no rows')).toBe(
      'Dosyada başlık satırının altında hiç işlem satırı yok.',
    )
    expect(translateError('file: more than 5000 rows')).toBe(
      "Dosyada 5000'den fazla işlem satırı var. Dosyayı böl ve parça parça yükle.",
    )
    expect(translateError('file: no period row found: not a Midas statement')).toBe(
      'Bu dosya bir Midas hesap ekstresi değil. Aracı kurum seçimini kontrol et.',
    )
    // Automatic mode names the statements it reads, not a broker the user
    // never chose.
    expect(translateError('file: not a supported statement')).toBe(
      "Bu dosyayı tanıyamadık. Midas, Garanti BBVA veya Garanti BBVA Yatırım hesap ekstresinin PDF'ini yükle.",
    )
    // Upload errors that are not about the file's format name no format or
    // broker: the CSV import sends them too.
    expect(translateError('file: required')).toBe('Bir dosya seç.')
    expect(translateError('file: invalid upload')).toBe('Dosya okunamadı.')
    expect(translateError('file: not a statement of some other broker')).toBe(
      'Bu dosya seçilen aracı kuruma ait bir ekstre değil. Aracı kurum seçimini kontrol et.',
    )
    expect(translateError('rows[1].date: must be within 2025-12-01 – 2025-12-31')).toBe(
      '2. satır: Tarih ekstre dönemi içinde olmalı (2025-12-01 – 2025-12-31).',
    )
    expect(translateError('rows[0].quantity: must be a positive number')).toMatch(
      /^1\. satır: Adet/,
    )
    expect(
      translateError('rows[0].quantity: must have at most 15 integer and 9 fraction digits'),
    ).toBe('1. satır: Adet en fazla 15 tam ve 9 ondalık basamak olabilir.')
    expect(
      translateError(
        'fee: must have at most 15 integer and 9 fraction digits',
        'transactions.withholdingTax',
      ),
    ).toBe('Stopaj en fazla 15 tam ve 9 ondalık basamak olabilir.')
    expect(translateError('close: must have at most 15 integer and 8 fraction digits')).toBe(
      'Fiyat en fazla 15 tam ve 8 ondalık basamak olabilir.',
    )
    expect(translateError('rows[0].amount: must be 0 when price is 0')).toBe(
      '1. satır: Fiyat 0 ise tutar da 0 olmalı.',
    )
    expect(translateError('period_end: must not be in the future')).toBe(
      'Ekstre dönemi henüz bitmemiş. Dönem bittikten sonra içe aktar.',
    )
    expect(translateError('period_start: must be on or after 1990-01-01')).toBe(
      'Ekstre dönemi 01.01.1990 veya sonrasında başlamalı.',
    )
    expect(translateError('period_end: period must be shorter than a year')).toBe(
      'Ekstre dönemi bir yıldan kısa olmalı.',
    )
    expect(translateError('period_end: must not be before period_start')).toBe(
      'Ekstre dönemi geçersiz.',
    )
    expect(translateError('date must not be in the future')).toBe('Tarih gelecekte olamaz.')
    expect(translateError('date: must not be in the future')).toBe('Tarih gelecekte olamaz.')
    expect(translateError('something new')).toBe('Bir hata oluştu. Lütfen tekrar dene.')
    expect(translateError(undefined)).toBe('Bir hata oluştu. Lütfen tekrar dene.')
  })
})
