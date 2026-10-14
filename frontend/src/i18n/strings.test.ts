import { afterEach, describe, expect, it } from 'vitest'
import { getLocale, setLocale, t, tr } from './strings'

describe('i18n', () => {
  // The other tests assert Turkish copy, so leave the default locale behind.
  afterEach(() => setLocale('tr'))

  it('switches t() to the English map after setLocale', () => {
    expect(getLocale()).toBe('tr')
    expect(t('auth.login.title')).toBe('Giriş yap')

    setLocale('en')

    expect(getLocale()).toBe('en')
    expect(t('auth.login.title')).toBe('Log in')
    expect(document.documentElement.lang).toBe('en')
    expect(localStorage.getItem('locale')).toBe('en')
  })

  it('picks the singular English form when count is 1', () => {
    setLocale('en')

    expect(t('transactions.count', { count: '1' })).toBe('1 transaction')
    expect(t('transactions.count', { count: '2' })).toBe('2 transactions')
    expect(t('transactions.count', { count: 1 })).toBe('1 transaction')
    expect(t('transactions.count', { count: 2 })).toBe('2 transactions')
  })

  // The tr UI addresses the user as "sen". The -ınız/-iniz/
  // -unuz/-ünüz ending marks the formal "siz" voice anywhere in a word:
  // "seçiminiz", "hesabınızda", "düzenleyebilirsiniz". No `\b` after it: a JS
  // regex word boundary is ASCII-only and misses "seçiminizi". The
  // formal imperative "-(y)in" reads like a genitive ("hesabın"), so it is
  // caught by a word list instead.
  const formalVoice = /[ıiuü]n[ıiuü]z/
  const formalImperative =
    /(?<!\p{L})(?:deneyin|girin|edin|seçin|bekleyin|tıklayın|ekleyin|yükleyin|yapın|olun|açın|kaydedin|düzenleyin|değiştirin|yazın)(?!\p{L})/iu

  it('keeps every Turkish string in the informal voice', () => {
    const formal = Object.entries(tr).filter(
      ([, text]) => formalVoice.test(text) || formalImperative.test(text),
    )

    expect(formal).toEqual([])
  })
})
