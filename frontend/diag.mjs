import { chromium } from '@playwright/test'

const BASE = 'https://whatsapp.beta-test.in'
const browser = await chromium.launch()
const page = await browser.newPage()

const logs = []
page.on('console', msg => logs.push(`[${msg.type()}] ${msg.text()}`))
page.on('pageerror', err => logs.push(`[pageerror] ${err.message}\n${err.stack}`))
page.on('requestfailed', req => logs.push(`[requestfailed] ${req.url()} ${req.failure()?.errorText}`))
page.on('response', res => { if (res.status() >= 400) logs.push(`[http${res.status()}] ${res.url()}`) })

await page.addInitScript(() => {
  window.addEventListener('unhandledrejection', e => {
    window.__unhandled = window.__unhandled || []
    window.__unhandled.push(String(e.reason?.stack || e.reason))
  })
})

await page.goto(`${BASE}/login`, { waitUntil: 'domcontentloaded' })
await page.fill('input[type="email"]', 'claude-diag@temp.local')
await page.fill('input[type="password"]', 'TempDiag123!')
await page.click('button[type="submit"]')
await page.waitForTimeout(2500)
console.log('after login URL:', page.url())

const client = await page.context().newCDPSession(page)
await client.send('Runtime.enable')
client.on('Runtime.exceptionThrown', e => logs.push(`[CDP-exception] ${JSON.stringify(e.exceptionDetails.exception?.description || e.exceptionDetails.text)}`))

page.on('response', async res => {
  if (res.url().includes('OrganizationDetailView') || res.url().includes('radio-group')) {
    logs.push(`[chunk] ${res.url()} status=${res.status()} type=${res.headers()['content-type']}`)
  }
})

await page.goto(`${BASE}/admin/organizations/new`, { waitUntil: 'networkidle' })
await page.waitForTimeout(1500)

console.log('URL:', page.url())
console.log('--- console/page logs ---')
console.log(logs.join('\n') || '(none)')

const bodyText = await page.evaluate(() => document.body.innerText.slice(0, 1000))
console.log('--- body text ---')
console.log(bodyText)

const cardHTML = await page.evaluate(() => {
  const cards = document.querySelectorAll('.p-6')
  return Array.from(cards).map(c => c.outerHTML.slice(0, 800))
})
console.log('--- .p-6 elements ---')
console.log(JSON.stringify(cardHTML, null, 2))

const inputCount = await page.evaluate(() => document.querySelectorAll('input').length)
console.log('input count on page:', inputCount)

const fullBodyHTML = await page.evaluate(() => document.querySelector('main, #app')?.outerHTML?.length || document.body.outerHTML.length)
console.log('body outerHTML length:', fullBodyHTML)

const mainCardHTML = await page.evaluate(() => {
  const el = document.querySelector('.p-6.pt-0, [class*="space-y-4"]')
  return el ? el.outerHTML : 'NOT FOUND'
})
console.log('--- looking for CardContent by alt selectors ---')
console.log(mainCardHTML)

// Dump the whole main content area HTML to find what's actually there
const mainArea = await page.evaluate(() => {
  const headerDiv = Array.from(document.querySelectorAll('div')).find(d => d.textContent?.trim() === 'New Organization' && d.querySelector('h3'))
  return headerDiv ? headerDiv.parentElement.outerHTML : 'not found'
})
console.log('--- parent of card header ---')
console.log(mainArea)

const unhandled = await page.evaluate(() => window.__unhandled || [])
console.log('--- unhandled rejections ---')
console.log(JSON.stringify(unhandled, null, 2))

const vueState = await page.evaluate(() => {
  const headerEl = Array.from(document.querySelectorAll('h3')).find(h => h.textContent.includes('New Organization'))
  if (!headerEl) return 'header h3 not found'
  let node = headerEl
  let instance = null
  for (let i = 0; i < 20 && node; i++) {
    if (node.__vueParentComponent) { instance = node.__vueParentComponent; break }
    node = node.parentElement
  }
  if (!instance) return 'no vue instance found walking up from h3'
  // walk up component tree to find OrganizationDetailView's setup state
  let comp = instance
  const results = []
  for (let i = 0; i < 15 && comp; i++) {
    const setupState = comp.setupState || {}
    const keys = Object.keys(setupState)
    results.push({ type: comp.type?.__name || comp.type?.name || 'anon', hasIsNew: 'isNew' in setupState, isNewVal: setupState.isNew?.value ?? setupState.isNew, keysSample: keys.slice(0, 15) })
    comp = comp.parent
  }
  return results
})
console.log('--- vue component tree walk ---')
console.log(JSON.stringify(vueState, null, 2))

await page.screenshot({ path: 'diag.png', fullPage: true })
await browser.close()
