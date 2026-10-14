// Captures the README screenshots (GLG-1381): logs in as the demo user and
// saves the Demo portfolio's summary, performance and transactions pages as
// PNGs, with headless Chrome driven over the DevTools protocol. Plain Node 22+
// (global fetch and WebSocket), no dependency. `make screenshots` runs it
// against a freshly seeded backend; by hand:
//
//   node docs/screenshots/capture.mjs <frontend-url> [output-dir]
//
// The frontend URL serves the app and proxies /api to the backend; the output
// dir defaults to this file's directory. CHROME names the browser binary.
import { spawn, spawnSync } from 'node:child_process'
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const [front, outDir = import.meta.dirname] = process.argv.slice(2)
if (!front) {
  console.error('usage: node capture.mjs <frontend-url> [output-dir]')
  process.exit(2)
}

// The demo seed's login (backend/internal/demo/demo.go).
const email = 'demo@example.com'
const password = 'demo-password'

// Each page's path under /portfolios/<id> and the DOM condition that says its
// data has rendered: a drawn chart series or table rows.
const pages = [
  ['summary', '', `document.querySelector('.recharts-surface path')`],
  ['performance', '/performance', `document.querySelector('.recharts-surface path')`],
  ['transactions', '/transactions', `document.querySelector('tbody tr')`],
]

function findChrome() {
  const mac = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
  const candidates = process.env.CHROME ? [process.env.CHROME] : [mac, 'google-chrome', 'chromium']
  return candidates.find((c) => spawnSync(c, ['--version']).status === 0)
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))

async function waitFor(what, check, timeout = 30000) {
  for (const end = Date.now() + timeout; Date.now() < end; await sleep(250)) {
    if (await check().catch(() => false)) return
  }
  throw new Error(`timed out waiting for ${what}`)
}

const chromeBin = findChrome()
if (!chromeBin) {
  console.error('Google Chrome not found: set CHROME to the path of its binary')
  process.exit(1)
}

const profile = mkdtempSync(join(tmpdir(), 'gelirgider-chrome-'))
const chrome = spawn(chromeBin, [
  '--headless=new',
  '--remote-debugging-port=0',
  `--user-data-dir=${profile}`,
  '--no-first-run',
  '--no-default-browser-check',
  '--hide-scrollbars',
  // Software rendering and a fixed color profile make two runs on the same
  // day write the same bytes, so a regeneration only changes what changed.
  '--disable-gpu',
  '--force-color-profile=srgb',
  'about:blank',
])
let failed = false
try {
  const wsUrl = await new Promise((resolve, reject) => {
    let err = ''
    chrome.stderr.on('data', (chunk) => {
      err += chunk
      const m = err.match(/DevTools listening on (ws:\/\/\S+)/)
      if (m) resolve(m[1])
    })
    chrome.on('exit', (code) => reject(new Error(`chrome exited with ${code}: ${err}`)))
    chrome.on('error', reject)
  })

  const ws = new WebSocket(wsUrl)
  await new Promise((resolve, reject) => {
    ws.onopen = resolve
    ws.onerror = () => reject(new Error(`cannot connect to ${wsUrl}`))
  })
  let nextId = 0
  const pending = new Map()
  ws.onmessage = ({ data }) => {
    const msg = JSON.parse(data)
    const p = pending.get(msg.id)
    if (!p) return
    pending.delete(msg.id)
    if (msg.error) p.reject(new Error(`${p.method}: ${msg.error.message}`))
    else p.resolve(msg.result)
  }
  const send = (method, params = {}, sessionId) =>
    new Promise((resolve, reject) => {
      const id = ++nextId
      pending.set(id, { method, resolve, reject })
      ws.send(JSON.stringify({ id, method, params, sessionId }))
    })

  const { targetId } = await send('Target.createTarget', { url: 'about:blank' })
  const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true })
  const page = (method, params) => send(method, params, sessionId)
  const evaluate = async (expression) => {
    const r = await page('Runtime.evaluate', {
      expression,
      awaitPromise: true,
      returnByValue: true,
    })
    if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description)
    return r.result.value
  }

  await page('Page.enable')
  await page('Emulation.setDeviceMetricsOverride', {
    width: 1440,
    height: 900,
    deviceScaleFactor: 2,
    mobile: false,
  })
  // Reduced motion makes Recharts draw its final frame without the entry
  // animation, whose first frames look empty.
  await page('Emulation.setEmulatedMedia', {
    features: [{ name: 'prefers-reduced-motion', value: 'reduce' }],
  })
  await page('Emulation.setTimezoneOverride', { timezoneId: 'Europe/Istanbul' })
  await page('Emulation.setLocaleOverride', { locale: 'en-US' })
  // English UI and light theme, read by i18n/strings.ts and theme.ts.
  await page('Page.addScriptToEvaluateOnNewDocument', {
    source: `try { localStorage.setItem('locale', 'en'); localStorage.setItem('theme', 'light') } catch {}`,
  })

  const navigate = async (url) => {
    await page('Page.navigate', { url })
    await waitFor(url, () => evaluate(`document.readyState === 'complete'`))
  }

  await navigate(`${front}/login`)
  await waitFor('the backend', () => evaluate(`fetch('/api/health').then((r) => r.ok)`))
  const status = await evaluate(`fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(${JSON.stringify({ email, password })}),
  }).then((r) => r.status)`)
  if (status !== 200) throw new Error(`login returned ${status}`)
  const id = await evaluate(
    `fetch('/api/portfolios').then((r) => r.json()).then((ps) => ps.find((p) => p.name === 'Demo')?.id)`,
  )
  if (!id) throw new Error('no Demo portfolio: was the database seeded with -demo?')

  for (const [name, path, ready] of pages) {
    await navigate(`${front}/portfolios/${id}${path}`)
    await waitFor(`the ${name} page`, () =>
      evaluate(`!!(${ready}) && !document.querySelector('.animate-pulse, [aria-busy="true"]')
        && document.fonts.status === 'loaded'`),
    )
    await sleep(500)
    const { data } = await page('Page.captureScreenshot', {
      format: 'png',
      captureBeyondViewport: false,
    })
    const file = join(outDir, `${name}.png`)
    writeFileSync(file, Buffer.from(data, 'base64'))
    console.log(file)
  }
  ws.close()
} catch (err) {
  console.error(err.message)
  failed = true
} finally {
  chrome.kill()
  await new Promise((resolve) =>
    chrome.exitCode === null ? chrome.on('exit', resolve) : resolve(),
  )
  rmSync(profile, { recursive: true, force: true })
}
process.exit(failed ? 1 : 0)
