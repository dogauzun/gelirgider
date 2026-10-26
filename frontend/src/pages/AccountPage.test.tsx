import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AccountPage } from './AccountPage'
import { Toaster } from '../components/ui/Toast'
import { chooseOption } from '../test/select'
import { paste } from '../test/paste'
import { setLocale } from '../i18n/strings'
import { setTheme } from '../theme'

describe('AccountPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  function mockMe(reportingCurrency: string, rules: object = {}) {
    return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      const method = init?.method ?? 'GET'

      if (url === '/api/me' && method === 'GET') {
        return new Response(
          JSON.stringify({ id: 1, email: 'a@b.com', reporting_currency: reportingCurrency }),
          { status: 200 },
        )
      }
      if (url === '/api/me/reporting-currency' && method === 'PUT') {
        const body = JSON.parse(init?.body as string) as { reporting_currency: string }
        if (body.reporting_currency === 'EUR') {
          return new Response(JSON.stringify({ error: 'internal error' }), { status: 500 })
        }
        return new Response(
          JSON.stringify({
            id: 1,
            email: 'a@b.com',
            reporting_currency: body.reporting_currency,
          }),
          { status: 200 },
        )
      }
      if (url === '/api/me/attention-rules' && method === 'GET') {
        return new Response(JSON.stringify(rules), { status: 200 })
      }
      if (url === '/api/me/attention-rules' && method === 'PUT') {
        const body = JSON.parse(init?.body as string) as { cash_share?: { value?: number } }
        if ((body.cash_share?.value ?? 0) > 1) {
          return new Response(
            JSON.stringify({ error: 'cash_share: value must be between 0 and 1' }),
            { status: 400 },
          )
        }
        return new Response(init?.body as string, { status: 200 })
      }
      throw new Error(`unexpected request: ${method} ${url}`)
    })
  }

  // The app shell mounts the Toaster; the page alone does not, so
  // render it here to read back the toast text.
  function renderPage() {
    const queryClient = new QueryClient()
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <AccountPage />
          <Toaster />
        </MemoryRouter>
      </QueryClientProvider>,
    )
    return queryClient
  }

  it('shows the page heading above the sections', async () => {
    mockMe('TRY')
    renderPage()

    // PageHeader's 20 px title, visually hidden below lg where the
    // top bar shows it.
    const heading = await screen.findByRole('heading', { level: 1, name: 'Hesap' })
    expect(heading).toHaveClass('text-[20px]', 'max-lg:sr-only')
  })

  // The portfolio pages' column, not a centred narrow one.
  it('puts the content in the portfolio pages wrapper', async () => {
    mockMe('TRY')
    renderPage()

    const wrapper = (await screen.findByRole('main')).firstElementChild
    expect(wrapper).toHaveClass('mx-auto', 'max-w-6xl', 'px-6')
    expect(wrapper).not.toHaveClass('max-w-2xl')
  })

  // One Tercihler card with three rows and one Şifre card, side by
  // side from lg, with the shared 14 px card title.
  it('groups the settings into a Tercihler card, a Şifre card, a two-step card and a Dikkat kuralları card', async () => {
    mockMe('TRY')
    renderPage()

    const preferencesTitle = await screen.findByRole('heading', { level: 2, name: 'Tercihler' })
    const passwordTitle = screen.getByRole('heading', { level: 2, name: 'Şifre' })
    // The two-step card waits for /api/me.
    await screen.findByRole('heading', { level: 2, name: 'İki adımlı doğrulama' })
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(4)
    for (const title of [preferencesTitle, passwordTitle]) {
      expect(title).toHaveClass('text-[14px]', 'font-semibold', 'text-ink')
    }

    const preferences = preferencesTitle.closest('section')!
    const password = passwordTitle.closest('section')!
    expect(preferences.parentElement).toBe(password.parentElement)
    expect(preferences.parentElement).toHaveClass('lg:grid-cols-2')

    expect(await within(preferences).findByLabelText('Raporlama para birimi')).toBeInTheDocument()
    expect(within(preferences).getByText('Dil')).toBeInTheDocument()
    expect(within(preferences).getByRole('group', { name: 'Dil' })).toBeInTheDocument()
    expect(within(preferences).getByText('Görünüm')).toBeInTheDocument()
    expect(within(preferences).getByRole('radiogroup', { name: 'Görünüm' })).toBeInTheDocument()

    expect(within(password).getByLabelText('Mevcut şifre')).toBeInTheDocument()
    expect(within(password).getByRole('button', { name: 'Şifreyi değiştir' })).toBeInTheDocument()
  })

  function attentionPut(fetchMock: ReturnType<typeof mockMe>) {
    return fetchMock.mock.calls.filter(
      ([url, init]) => url === '/api/me/attention-rules' && init?.method === 'PUT',
    )
  }

  it('gives a rule input a fixed narrow width so the card fits a phone', async () => {
    mockMe('TRY')
    renderPage()

    // w-auto left the browser's 214 px default, which widened /account to 414 px at 375 px.
    const cash = await screen.findByRole('spinbutton', { name: 'Nakit payı' })
    expect(cash).toHaveClass('w-24')
    expect(cash).not.toHaveClass('w-auto')
  })

  it('sends only the changed attention rule on save', async () => {
    const fetchMock = mockMe('TRY')
    renderPage()

    const cash = await screen.findByRole('spinbutton', { name: 'Nakit payı' })
    expect(cash).toHaveValue(40)
    fireEvent.change(cash, { target: { value: '55' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    await waitFor(() => expect(attentionPut(fetchMock)).toHaveLength(1))
    expect(JSON.parse(attentionPut(fetchMock)[0][1]?.body as string)).toEqual({
      cash_share: { value: 0.55 },
    })
    await waitFor(() =>
      expect(within(screen.getByRole('status')).getByText('Kaydedildi')).toBeInTheDocument(),
    )
  })

  // isPending reaches the button a tick after the click, so a double click
  // sent the PUT twice and toasted twice.
  it('sends the attention rules once on a double click of save', async () => {
    const fetchMock = mockMe('TRY')
    renderPage()

    const cash = await screen.findByRole('spinbutton', { name: 'Nakit payı' })
    fireEvent.change(cash, { target: { value: '55' } })
    const save = screen.getByRole('button', { name: 'Kaydet' })
    fireEvent.click(save)
    fireEvent.click(save)

    await waitFor(() => expect(screen.getAllByText('Kaydedildi')).toHaveLength(1))
    expect(attentionPut(fetchMock)).toHaveLength(1)
  })

  it('drops an override set back to its default and sends a switched-off rule', async () => {
    const fetchMock = mockMe('TRY', { cash_share: { value: 0.55 } })
    renderPage()

    const cash = await screen.findByRole('spinbutton', { name: 'Nakit payı' })
    expect(cash).toHaveValue(55)
    fireEvent.change(cash, { target: { value: '40' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Günün hareketlileri' }))
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    await waitFor(() => expect(attentionPut(fetchMock)).toHaveLength(1))
    expect(JSON.parse(attentionPut(fetchMock)[0][1]?.body as string)).toEqual({
      movers: { enabled: false },
    })
  })

  it('does not send a value for a cleared input of a switched-off rule', async () => {
    const fetchMock = mockMe('TRY')
    renderPage()

    const cash = await screen.findByRole('spinbutton', { name: 'Nakit payı' })
    fireEvent.change(cash, { target: { value: '' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Nakit payı' }))
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    await waitFor(() => expect(attentionPut(fetchMock)).toHaveLength(1))
    expect(JSON.parse(attentionPut(fetchMock)[0][1]?.body as string)).toEqual({
      cash_share: { enabled: false },
    })
  })

  it('lists the toggle rule without an input and sends only its enabled flag', async () => {
    const fetchMock = mockMe('TRY')
    renderPage()

    const hedge = await screen.findByRole('checkbox', { name: 'Döviz veya altın koruması yok' })
    expect(screen.queryByRole('spinbutton', { name: 'Döviz veya altın koruması yok' })).toBeNull()
    fireEvent.click(hedge)
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    await waitFor(() => expect(attentionPut(fetchMock)).toHaveLength(1))
    expect(JSON.parse(attentionPut(fetchMock)[0][1]?.body as string)).toEqual({
      no_hedge: { enabled: false },
    })
  })

  it('shows a rejected attention rule under the card', async () => {
    mockMe('TRY')
    renderPage()

    const cash = await screen.findByRole('spinbutton', { name: 'Nakit payı' })
    fireEvent.change(cash, { target: { value: '150' } })
    fireEvent.click(screen.getByRole('button', { name: 'Kaydet' }))

    expect(await screen.findByText(/Paylar % 0-100/)).toHaveAttribute('role', 'alert')
  })

  it('toasts on a successful currency change', async () => {
    mockMe('TRY')
    renderPage()

    const select = await screen.findByLabelText('Raporlama para birimi')
    await chooseOption(select, 'Amerikan Doları (USD)')

    await waitFor(() =>
      expect(within(screen.getByRole('status')).getByText('Kaydedildi')).toBeInTheDocument(),
    )
  })

  // The portfolio list carries values in the reporting currency, and the
  // sidebar showing it stays mounted on this page.
  it('refreshes the portfolio list after a currency change', async () => {
    mockMe('TRY')
    const queryClient = renderPage()
    const spy = vi.spyOn(queryClient, 'invalidateQueries')

    const select = await screen.findByLabelText('Raporlama para birimi')
    await chooseOption(select, 'Amerikan Doları (USD)')

    await waitFor(() => expect(spy).toHaveBeenCalledWith({ queryKey: ['portfolios'], exact: true }))
  })

  // Chromium blurs a focused control the moment it is disabled, so a
  // keyboard user who picked a currency lost focus to <body> while it saved.
  // jsdom does not blur on disable, so the test checks the trigger stays
  // enabled during the save as well as focused after it.
  it('keeps focus on the currency select during and after a keyboard save', async () => {
    const fetchMock = mockMe('TRY')
    const me = fetchMock.getMockImplementation()!
    let release!: () => void
    const held = new Promise<void>((resolve) => (release = resolve))
    fetchMock.mockImplementation(async (input, init) => {
      if (String(input) === '/api/me/reporting-currency') {
        await held
      }
      return me(input, init)
    })
    renderPage()

    const trigger = await screen.findByLabelText('Raporlama para birimi')
    trigger.focus()
    fireEvent.keyDown(trigger, { key: 'ArrowDown' })
    const usd = await screen.findByRole('option', { name: 'Amerikan Doları (USD)' })
    fireEvent.keyDown(screen.getByRole('listbox'), { key: 'ArrowDown' })
    await waitFor(() => expect(usd).toHaveFocus())
    fireEvent.keyDown(usd, { key: 'Enter' })

    expect(await screen.findByText('Kaydediliyor...')).toBeInTheDocument()
    expect(trigger).toBeEnabled()
    await waitFor(() => expect(trigger).toHaveFocus())

    release()
    await waitFor(() => expect(trigger).toHaveTextContent('Amerikan Doları (USD)'))
    expect(trigger).toHaveFocus()
  })

  it('shows the translated error message under the field when the save fails', async () => {
    mockMe('TRY')
    renderPage()

    const select = await screen.findByLabelText('Raporlama para birimi')
    await chooseOption(select, 'Euro (EUR)')

    expect(await screen.findByText(/Sunucu hatası/)).toBeInTheDocument()
    await waitFor(() => expect(select).toHaveAttribute('aria-invalid', 'true'))
  })

  // Password managers tie the new password to the account through a
  // username field in the same form.
  it('carries the account email as the password form username', async () => {
    mockMe('TRY')
    renderPage()

    const form = (await screen.findByLabelText('Mevcut şifre')).closest('form')!
    await waitFor(() =>
      expect(form.querySelector('input[autocomplete="username"]')).toHaveValue('a@b.com'),
    )
  })

  it('does not send the change when the two new passwords differ', async () => {
    const fetchMock = mockMe('TRY')
    renderPage()

    fireEvent.change(await screen.findByLabelText('Mevcut şifre'), {
      target: { value: 'oldpassword' },
    })
    fireEvent.change(screen.getByLabelText('Yeni şifre'), { target: { value: 'newpassword' } })
    fireEvent.change(screen.getByLabelText('Yeni şifre (tekrar)'), {
      target: { value: 'newpasswrod' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi değiştir' }))

    expect(await screen.findByText('Yeni şifreler eşleşmiyor.')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input) === '/api/account/password')).toBe(
      false,
    )
  })

  it('clears the mismatch error once the new passwords are edited', async () => {
    mockMe('TRY')
    renderPage()

    fireEvent.change(await screen.findByLabelText('Mevcut şifre'), {
      target: { value: 'oldpassword' },
    })
    fireEvent.change(screen.getByLabelText('Yeni şifre'), { target: { value: 'short' } })
    fireEvent.change(screen.getByLabelText('Yeni şifre (tekrar)'), { target: { value: 'shrot' } })
    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi değiştir' }))
    expect(await screen.findByText('Yeni şifreler eşleşmiyor.')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('Yeni şifre (tekrar)'), { target: { value: 'short' } })

    expect(screen.queryByText('Yeni şifreler eşleşmiyor.')).not.toBeInTheDocument()
  })

  // Chromium blurs a focused button the moment it is disabled, so the
  // running change marks the button pending instead; a second submit sends
  // nothing.
  it('keeps the password button focused and pending while the change runs', async () => {
    const fetchMock = mockMe('TRY')
    const me = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation((input, init) =>
      String(input) === '/api/account/password' ? new Promise<Response>(() => {}) : me(input, init),
    )
    renderPage()

    fireEvent.change(await screen.findByLabelText('Mevcut şifre'), {
      target: { value: 'oldpassword' },
    })
    fireEvent.change(screen.getByLabelText('Yeni şifre'), { target: { value: 'newpassword' } })
    fireEvent.change(screen.getByLabelText('Yeni şifre (tekrar)'), {
      target: { value: 'newpassword' },
    })
    const submit = screen.getByRole('button', { name: 'Şifreyi değiştir' })
    submit.focus()
    fireEvent.click(submit)

    await waitFor(() => expect(submit).toHaveAttribute('aria-disabled', 'true'))
    expect(submit).toBeEnabled()
    expect(submit).toHaveFocus()
    fireEvent.click(submit)
    const changes = fetchMock.mock.calls.filter(([url]) => url === '/api/account/password')
    expect(changes).toHaveLength(1)
  })

  it('shows a rejected new password under the new-password field', async () => {
    const fetchMock = mockMe('TRY')
    const me = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation(async (input, init) => {
      if (String(input) === '/api/account/password') {
        return new Response(JSON.stringify({ error: 'password must not be only whitespace' }), {
          status: 400,
        })
      }
      return me(input, init)
    })
    renderPage()

    const current = await screen.findByLabelText('Mevcut şifre')
    const next = screen.getByLabelText('Yeni şifre')
    fireEvent.change(current, { target: { value: 'oldpassword' } })
    fireEvent.change(next, { target: { value: '         ' } })
    fireEvent.change(screen.getByLabelText('Yeni şifre (tekrar)'), {
      target: { value: '         ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi değiştir' }))

    await waitFor(() => expect(next).toHaveAttribute('aria-invalid', 'true'))
    expect(next).toHaveAccessibleDescription('Şifre yalnızca boşluktan oluşamaz.')
    expect(current).not.toHaveAttribute('aria-invalid')
  })

  // A password over 72 bytes pasted into both fields is the same
  // password, so the server's length rule shows, not a mismatch.
  it('shows the 72-byte limit, not a mismatch, for a long pasted password', async () => {
    const fetchMock = mockMe('TRY')
    const me = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation(async (input, init) => {
      if (String(input) === '/api/account/password') {
        return new Response(JSON.stringify({ error: 'password must be at most 72 bytes' }), {
          status: 400,
        })
      }
      return me(input, init)
    })
    renderPage()

    const long = 'Ab1-'.repeat(20)
    fireEvent.change(await screen.findByLabelText('Mevcut şifre'), {
      target: { value: 'oldpassword' },
    })
    const next = screen.getByLabelText('Yeni şifre')
    paste(next, long)
    paste(screen.getByLabelText('Yeni şifre (tekrar)'), long)
    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi değiştir' }))

    await waitFor(() => expect(next).toHaveAccessibleDescription('Şifre en fazla 72 bayt olmalı.'))
    expect(screen.queryByText('Yeni şifreler eşleşmiyor.')).not.toBeInTheDocument()
  })

  // A server error is about the values sent, so editing the form
  // drops it, as on the signed-out forms.
  it('clears the wrong current password error once the field is edited', async () => {
    const fetchMock = mockMe('TRY')
    const me = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation(async (input, init) => {
      if (String(input) === '/api/account/password') {
        return new Response(JSON.stringify({ error: 'current password is incorrect' }), {
          status: 401,
        })
      }
      return me(input, init)
    })
    renderPage()

    const current = await screen.findByLabelText('Mevcut şifre')
    fireEvent.change(current, { target: { value: 'wrongpass1' } })
    fireEvent.change(screen.getByLabelText('Yeni şifre'), { target: { value: 'newpassword1' } })
    fireEvent.change(screen.getByLabelText('Yeni şifre (tekrar)'), {
      target: { value: 'newpassword1' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Şifreyi değiştir' }))
    await waitFor(() => expect(current).toHaveAttribute('aria-invalid', 'true'))
    expect(screen.getByText('Mevcut şifre hatalı.')).toBeInTheDocument()

    fireEvent.change(current, { target: { value: 'password123' } })

    expect(screen.queryByText('Mevcut şifre hatalı.')).not.toBeInTheDocument()
    expect(current).not.toHaveAttribute('aria-invalid')
  })

  // /api/me answers with the current totp_enabled; the totp endpoints reply
  // as `reply` says and are recorded.
  function mockTotp(reply: (url: string, body: Record<string, string>) => Response) {
    let enabled = false
    const calls: { method: string; url: string; body: Record<string, string> }[] = []
    const fetchMock = mockMe('TRY')
    const me = fetchMock.getMockImplementation()!
    fetchMock.mockImplementation(async (input, init) => {
      const url = String(input)
      if (url === '/api/me') {
        return new Response(
          JSON.stringify({
            id: 1,
            email: 'a@b.com',
            reporting_currency: 'TRY',
            totp_enabled: enabled,
          }),
        )
      }
      if (!url.startsWith('/api/account/totp')) return me(input, init)
      const body = JSON.parse(init?.body as string) as Record<string, string>
      calls.push({ method: init?.method ?? 'GET', url, body })
      const response = reply(url, body)
      if (response.ok && url === '/api/account/totp/confirm') enabled = true
      if (response.ok && init?.method === 'DELETE') enabled = false
      return response
    })
    return calls
  }

  const secret = 'JBSWY3DPEHPK3PXP'
  const uri = `otpauth://totp/Gelirgider:a%40b.com?secret=${secret}&issuer=Gelirgider`
  const setupOk = () => new Response(JSON.stringify({ secret, uri }))
  const recoveryCodes = Array.from({ length: 10 }, (_, i) => `ABCD${i}-EFGH${i}`)
  const codesOk = (codes = recoveryCodes) => new Response(JSON.stringify({ recovery_codes: codes }))
  function totpOk(url: string) {
    if (url === '/api/account/totp/setup') return setupOk()
    return url === '/api/account/totp' ? new Response(null, { status: 204 }) : codesOk()
  }

  // The password card has a field of the same label.
  async function twoStepCard() {
    const title = await screen.findByRole('heading', { level: 2, name: 'İki adımlı doğrulama' })
    return title.closest('section')!
  }

  async function startEnrolment(password: string) {
    fireEvent.click(await screen.findByRole('button', { name: 'Etkinleştir' }))
    fireEvent.change(within(await twoStepCard()).getByLabelText('Mevcut şifre'), {
      target: { value: password },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Devam' }))
  }

  it('shows the QR code and secret after setup, and the state as on after a confirmed code', async () => {
    const calls = mockTotp((url) => totpOk(url))
    renderPage()
    await startEnrolment('password123')

    expect(await screen.findByDisplayValue(secret)).toHaveAttribute('readonly')
    expect(screen.getByDisplayValue(uri)).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'İki adımlı doğrulama QR kodu' })).toBeInTheDocument()
    expect(calls[0]).toMatchObject({
      url: '/api/account/totp/setup',
      body: { password: 'password123' },
    })

    fireEvent.change(screen.getByLabelText('Uygulamadaki 6 haneli kod'), {
      target: { value: '123456' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Doğrula ve aç' }))

    // Açık is also a theme name: look in the card.
    const card = await twoStepCard()
    expect(await within(card).findByText('Açık')).toBeInTheDocument()
    expect(calls[1]).toMatchObject({ url: '/api/account/totp/confirm', body: { code: '123456' } })
    expect(screen.queryByDisplayValue(secret)).not.toBeInTheDocument()
    expect(screen.getByText('Bu kodlar bir daha gösterilmeyecek')).toBeInTheDocument()
  })

  it('shows a wrong password inline during enrolment', async () => {
    mockTotp(
      () =>
        new Response(JSON.stringify({ error: 'current password is incorrect' }), { status: 401 }),
    )
    renderPage()
    await startEnrolment('wrongpass1')
    expect(await screen.findByText('Mevcut şifre hatalı.')).toBeInTheDocument()
  })

  it('shows a wrong confirm code inline and keeps the secret', async () => {
    mockTotp((url) =>
      url === '/api/account/totp/setup'
        ? setupOk()
        : new Response(JSON.stringify({ error: 'invalid code' }), { status: 400 }),
    )
    renderPage()
    await startEnrolment('password123')
    fireEvent.change(await screen.findByLabelText('Uygulamadaki 6 haneli kod'), {
      target: { value: '000000' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Doğrula ve aç' }))

    expect(await screen.findByText('Kod hatalı.')).toBeInTheDocument()
    expect(screen.getByDisplayValue(secret)).toBeInTheDocument()
  })

  it('turns two-step off with the password and a code', async () => {
    const calls = mockTotp((url) => totpOk(url))
    renderPage()
    await startEnrolment('password123')
    fireEvent.change(await screen.findByLabelText('Uygulamadaki 6 haneli kod'), {
      target: { value: '123456' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Doğrula ve aç' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Kodları sakladım' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Kapat' }))
    fireEvent.change(within(await twoStepCard()).getByLabelText('Mevcut şifre'), {
      target: { value: 'password123' },
    })
    fireEvent.change(screen.getByLabelText('6 haneli kod veya bir kurtarma kodu'), {
      target: { value: '654321' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'İki adımlı doğrulamayı kapat' }))

    expect(await within(await twoStepCard()).findByText('Kapalı')).toBeInTheDocument()
    expect(calls[2]).toMatchObject({
      method: 'DELETE',
      url: '/api/account/totp',
      body: { password: 'password123', code: '654321' },
    })
  })

  async function enrol() {
    await startEnrolment('password123')
    fireEvent.change(await screen.findByLabelText('Uygulamadaki 6 haneli kod'), {
      target: { value: '123456' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Doğrula ve aç' }))
  }

  it('shows the 10 recovery codes once after a confirmed code, until they are dismissed', async () => {
    mockTotp(totpOk)
    renderPage()
    await enrol()

    const list = await screen.findByRole('list', { name: 'Kurtarma kodları' })
    expect(
      within(list)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(recoveryCodes)
    expect(screen.getByText('Bu kodlar bir daha gösterilmeyecek')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Kapat' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Kodları sakladım' }))

    expect(screen.queryByRole('list', { name: 'Kurtarma kodları' })).not.toBeInTheDocument()
    expect(screen.queryByText(recoveryCodes[0])).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Kapat' })).toBeInTheDocument()
  })

  it('drops the recovery codes when the card remounts', async () => {
    mockTotp(totpOk)
    renderPage()
    await enrol()
    await screen.findByRole('list', { name: 'Kurtarma kodları' })

    cleanup()
    renderPage()

    expect(await screen.findByRole('button', { name: 'Kapat' })).toBeInTheDocument()
    expect(screen.queryByText(recoveryCodes[0])).not.toBeInTheDocument()
  })

  it('copies the recovery codes, one per line', async () => {
    mockTotp(totpOk)
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    renderPage()
    await enrol()

    fireEvent.click(await screen.findByRole('button', { name: 'Kopyala' }))

    await waitFor(() => expect(writeText).toHaveBeenCalledWith(recoveryCodes.join('\n')))
    vi.unstubAllGlobals()
  })

  it('regenerates the recovery codes with the password and a recovery code', async () => {
    const fresh = recoveryCodes.map((c) => c.replace('ABCD', 'WXYZ'))
    const calls = mockTotp((url) =>
      url === '/api/account/totp/recovery-codes' ? codesOk(fresh) : totpOk(url),
    )
    renderPage()
    await enrol()
    fireEvent.click(await screen.findByRole('button', { name: 'Kodları sakladım' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Kurtarma kodlarını yenile' }))
    fireEvent.change(within(await twoStepCard()).getByLabelText('Mevcut şifre'), {
      target: { value: 'password123' },
    })
    fireEvent.change(screen.getByLabelText('6 haneli kod veya bir kurtarma kodu'), {
      target: { value: 'ABCDE-FGHIJ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Yeni kodlar üret' }))

    const list = await screen.findByRole('list', { name: 'Kurtarma kodları' })
    expect(
      within(list)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(fresh)
    expect(calls[2]).toMatchObject({
      method: 'POST',
      url: '/api/account/totp/recovery-codes',
      body: { password: 'password123', code: 'ABCDE-FGHIJ' },
    })
  })

  it('turns two-step off with a recovery code in place of the code', async () => {
    const calls = mockTotp(totpOk)
    renderPage()
    await enrol()
    fireEvent.click(await screen.findByRole('button', { name: 'Kodları sakladım' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Kapat' }))
    fireEvent.change(within(await twoStepCard()).getByLabelText('Mevcut şifre'), {
      target: { value: 'password123' },
    })
    fireEvent.change(screen.getByLabelText('6 haneli kod veya bir kurtarma kodu'), {
      target: { value: 'ABCDE-FGHIJ' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'İki adımlı doğrulamayı kapat' }))

    expect(await within(await twoStepCard()).findByText('Kapalı')).toBeInTheDocument()
    expect(calls[2]).toMatchObject({ method: 'DELETE', body: { code: 'ABCDE-FGHIJ' } })
  })

  it('switches the interface language from the segmented control', async () => {
    mockMe('TRY')
    renderPage()

    const english = await screen.findByRole('button', { name: 'English' })
    expect(screen.getByRole('button', { name: 'Türkçe' })).toHaveAttribute('aria-pressed', 'true')

    fireEvent.click(english)

    await waitFor(() => expect(english).toHaveAttribute('aria-pressed', 'true'))
    // The other tests assert Turkish copy, so leave the default locale behind.
    setLocale('tr')
  })

  // One Tab stop, and the arrow keys move and select.
  it('moves the appearance choice with the arrow keys', async () => {
    mockMe('TRY')
    renderPage()

    const system = await screen.findByRole('radio', { name: 'Sistem' })
    expect(system).toHaveAttribute('tabindex', '0')
    system.focus()
    fireEvent.keyDown(system, { key: 'ArrowRight' })

    const light = screen.getByRole('radio', { name: 'Açık' })
    expect(light).toHaveAttribute('aria-checked', 'true')
    expect(light).toHaveFocus()
    expect(system).toHaveAttribute('tabindex', '-1')
    setTheme('system')
  })
})
