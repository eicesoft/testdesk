// Browser sidecar for TestDesk.
//
// Reads one JSON request per line from stdin and writes one JSON response per
// line to stdout: {"id": n, "method": "...", "params": {...}} -> {"id": n, "result": {...}} | {"id": n, "error": "..."}.
// Written into the runtime directory by the app; resolves `playwright` from there.

import readline from 'node:readline'
import { chromium } from 'playwright'
import { expect } from '@playwright/test'

const REF_ATTR = 'data-testdesk-ref'
const ACTION_TIMEOUT = 10000
const MAX_ELEMENTS = 300

let browser = null
let page = null

// Manual recording state. Interactions are reported as they happen; navigations that
// follow a click or key press are left out because replaying the click already causes them.
let recording = false
let bindingReady = false
let lastReportedURL = ''
let lastInteractionAt = 0
const INTERACTION_WINDOW = 2000

const emitEvent = (event, params = {}) => send({ event, params })

function onUserAction(payload) {
  lastInteractionAt = Date.now()
  emitEvent('action', payload)
}

// Runs inside the page. One function with an `op` switch so the helpers are shared.
//   collect:  tag interactive elements with REF_ATTR and return them
//   describe: return the role, accessible name and CSS path of one tagged element
//   locate:   same as describe, for a live element passed in as `el` (manual recording)
function inPage({ op, attr, ref, max, el }) {
  const INTERACTIVE =
    'a[href], button, input, select, textarea, summary, h1, h2, h3, h4, h5, h6, img[alt], [role], [contenteditable="true"], [tabindex]:not([tabindex="-1"])'
  const IMPLICIT = {
    A: 'link',
    BUTTON: 'button',
    SELECT: 'combobox',
    TEXTAREA: 'textbox',
    SUMMARY: 'button',
    H1: 'heading',
    H2: 'heading',
    H3: 'heading',
    H4: 'heading',
    H5: 'heading',
    H6: 'heading',
    IMG: 'img',
  }
  const INPUT_ROLE = {
    button: 'button',
    submit: 'button',
    reset: 'button',
    image: 'button',
    checkbox: 'checkbox',
    radio: 'radio',
    range: 'slider',
    number: 'spinbutton',
  }

  const squash = (s) => s.replace(/\s+/g, ' ').trim()

  function roleOf(el) {
    const explicit = (el.getAttribute('role') || '').trim().split(/\s+/)[0]
    if (explicit) return explicit
    if (el.tagName === 'INPUT') return INPUT_ROLE[(el.type || 'text').toLowerCase()] || 'textbox'
    return IMPLICIT[el.tagName] || ''
  }

  function nameOf(el) {
    const labelledby = el.getAttribute('aria-labelledby')
    if (labelledby) {
      const t = squash(labelledby.split(/\s+/).map((id) => document.getElementById(id)?.textContent ?? '').join(' '))
      if (t) return t
    }
    const aria = squash(el.getAttribute('aria-label') || '')
    if (aria) return aria
    if (el.tagName === 'INPUT' && ['button', 'submit', 'reset'].includes(el.type)) return el.value || ''
    if (el.tagName === 'IMG') return el.getAttribute('alt') || ''
    if (el.labels && el.labels.length) {
      const t = squash([...el.labels].map((l) => l.textContent).join(' '))
      if (t) return t
    }
    const text = squash(el.textContent || '')
    return (text || el.getAttribute('placeholder') || el.getAttribute('title') || '').slice(0, 100)
  }

  function visible(el) {
    const style = getComputedStyle(el)
    return style.visibility !== 'hidden' && style.display !== 'none' && el.getClientRects().length > 0
  }

  function cssPath(el) {
    if (el.id && document.querySelectorAll('#' + CSS.escape(el.id)).length === 1) return '#' + CSS.escape(el.id)
    const parts = []
    let node = el
    while (node && node.nodeType === 1 && node !== document.body) {
      let part = node.tagName.toLowerCase()
      const parent = node.parentElement
      if (parent) {
        const same = [...parent.children].filter((c) => c.tagName === node.tagName)
        if (same.length > 1) part += `:nth-of-type(${same.indexOf(node) + 1})`
      }
      parts.unshift(part)
      node = parent
    }
    return ['body', ...parts].join(' > ')
  }

  if (op === 'collect') {
    document.querySelectorAll(`[${attr}]`).forEach((el) => el.removeAttribute(attr))
    const elements = []
    let counter = 0
    for (const el of document.querySelectorAll(INTERACTIVE)) {
      if (elements.length >= max) break
      if (!visible(el)) continue
      const role = roleOf(el)
      if (!role) continue
      const id = 'e' + ++counter
      el.setAttribute(attr, id)
      const entry = { ref: id, role, name: nameOf(el) }
      if (el.disabled) entry.disabled = true
      elements.push(entry)
    }
    const text = (document.body?.innerText || '').replace(/\n{3,}/g, '\n\n').slice(0, 4000)
    return { elements, text }
  }

  if (op === 'describe') {
    const el = document.querySelector(`[${attr}="${ref}"]`)
    if (!el) return null
    return { role: roleOf(el), name: nameOf(el), css: cssPath(el) }
  }

  if (op === 'locate') {
    // Use the nearest interactive ancestor, so a click on a <span> inside a button records the button.
    const target = el.closest(INTERACTIVE) || el
    const role = roleOf(target)
    const name = nameOf(target)
    if (role && name) {
      const same = [...document.querySelectorAll(INTERACTIVE)].filter(
        (e) => visible(e) && roleOf(e) === role && nameOf(e) === name,
      )
      if (same.length === 1) return { by: 'role', role, name }
    }
    return { by: 'css', selector: cssPath(target) }
  }

  throw new Error('unknown op ' + op)
}

// Runs inside the page during manual recording. Reports clicks, field commits, select changes
// and Enter presses through window.__testdeskRecord. Fields are recorded as one fill when they
// lose focus or Enter is pressed, not per keystroke.
function captureScript() {
  if (window.__testdeskInstalled) return
  window.__testdeskInstalled = true

  const NON_TEXT_INPUT = ['button', 'submit', 'reset', 'image', 'checkbox', 'radio', 'file', 'range', 'color']
  const isField = (el) =>
    el.tagName === 'TEXTAREA' || (el.tagName === 'INPUT' && !NON_TEXT_INPUT.includes((el.type || '').toLowerCase()))
  const baseline = new WeakMap()
  const report = (payload) => window.__testdeskRecord(JSON.stringify(payload))
  const locate = (el) => window.__tdLocate({ op: 'locate', el })

  function commitField(el) {
    if (!baseline.has(el)) return
    const value = el.value
    if (value === baseline.get(el)) return
    baseline.set(el, value)
    report({ tool: 'fill', locator: locate(el), value })
  }

  document.addEventListener('focusin', (e) => {
    if (isField(e.target)) baseline.set(e.target, e.target.value)
  }, true)

  document.addEventListener('click', (e) => {
    const el = e.target
    if (!(el instanceof Element)) return
    // Typing and choosing are recorded by the change handler below; a click on them would only add noise.
    if (isField(el) || el.tagName === 'SELECT' || el.tagName === 'OPTION') return
    report({ tool: 'click', locator: locate(el) })
  }, true)

  document.addEventListener('change', (e) => {
    const el = e.target
    if (el.tagName === 'SELECT') {
      report({ tool: 'select_option', locator: locate(el), value: el.value })
    } else if (isField(el)) {
      commitField(el)
    }
  }, true)

  document.addEventListener('keydown', (e) => {
    // Enter on a field submits it, so flush the typed value first to keep the replay in order.
    if (e.key !== 'Enter' || !(e.target instanceof Element) || !isField(e.target)) return
    commitField(e.target)
    report({ tool: 'press', locator: locate(e.target), key: 'Enter' })
  }, true)
}

function requirePage() {
  if (!page) throw new Error('浏览器未启动，请先调用 launch')
  return page
}

// Turns a ref from the last snapshot into a locator descriptor plus a live Playwright locator.
// Prefers getByRole (stable and readable); falls back to a CSS path.
async function describeRef(ref) {
  const p = requirePage()
  const info = await p.evaluate(inPage, { op: 'describe', attr: REF_ATTR, ref })
  if (!info) throw new Error(`元素 ${ref} 不存在，请重新 snapshot`)

  if (info.role && info.name) {
    const byRole = p.getByRole(info.role, { name: info.name, exact: true })
    const count = await byRole.count()
    for (let i = 0; i < Math.min(count, 50); i++) {
      if ((await byRole.nth(i).getAttribute(REF_ATTR)) === ref) {
        return {
          desc: { by: 'role', role: info.role, name: info.name, ...(count > 1 ? { nth: i } : {}) },
          locator: byRole.nth(i),
        }
      }
    }
  }
  return {
    desc: { by: 'css', selector: info.css },
    locator: p.locator(`[${REF_ATTR}="${ref}"]`),
  }
}

// Resolves the target of an action: a ref, a text match, or nothing.
async function target(params) {
  if (params.ref) return describeRef(params.ref)
  if (params.text) {
    return { desc: { by: 'text', text: params.text }, locator: requirePage().getByText(params.text).first() }
  }
  throw new Error('需要 ref 或 text 参数')
}

const timeoutOf = (params) => params.timeout ?? ACTION_TIMEOUT

// Installed into the page (new documents via addInitScript, the current one via evaluate).
// window.__tdLocate is set alongside it so captureScript can describe elements with inPage.
const captureSource = `(() => { window.__tdLocate = (${inPage.toString()}); (${captureScript.toString()})() })()`

let navigatingByCall = false

const handlers = {
  async launch({ headless = false } = {}) {
    if (browser) return {}
    browser = await chromium.launch({ headless })
    // A new page has no binding or listeners yet, so record_start must set them up again.
    bindingReady = false
    browser.on('disconnected', () => {
      if (!recording) return
      recording = false
      emitEvent('closed')
    })
    page = await browser.newPage()
    return {}
  },

  async navigate({ url }) {
    const p = requirePage()
    navigatingByCall = true
    try {
      await p.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 })
    } finally {
      navigatingByCall = false
      lastReportedURL = p.url()
    }
    return { url: p.url(), title: await p.title() }
  },

  // Starts reporting user actions on the current page. Safe to call once per launch.
  async record_start() {
    const p = requirePage()
    if (recording) return {}
    recording = true
    lastReportedURL = p.url()
    if (!bindingReady) {
      bindingReady = true
      await p.exposeBinding('__testdeskRecord', (_source, json) => onUserAction(JSON.parse(json)))
      await p.addInitScript(captureSource)
      p.on('framenavigated', (frame) => {
        if (!recording || navigatingByCall || frame !== p.mainFrame()) return
        const url = frame.url()
        if (url === lastReportedURL) return
        lastReportedURL = url
        if (Date.now() - lastInteractionAt < INTERACTION_WINDOW) return
        emitEvent('action', { tool: 'navigate', url })
      })
    }
    await p.evaluate(captureSource)
    return {}
  },

  async snapshot() {
    const p = requirePage()
    const data = await p.evaluate(inPage, { op: 'collect', attr: REF_ATTR, max: MAX_ELEMENTS })
    return { url: p.url(), title: await p.title(), ...data }
  },

  async click(params) {
    const t = await target(params)
    await t.locator.click({ timeout: timeoutOf(params) })
    return { locator: t.desc }
  },

  async fill(params) {
    const t = await target(params)
    await t.locator.fill(String(params.value ?? ''), { timeout: timeoutOf(params) })
    return { locator: t.desc }
  },

  async press(params) {
    const t = await target(params)
    await t.locator.press(String(params.key), { timeout: timeoutOf(params) })
    return { locator: t.desc }
  },

  async select_option(params) {
    const t = await target(params)
    await t.locator.selectOption(String(params.value ?? ''), { timeout: timeoutOf(params) })
    return { locator: t.desc }
  },

  async wait_for(params) {
    const t = await target(params)
    await expect(t.locator).toBeVisible({ timeout: timeoutOf(params) })
    return { locator: t.desc }
  },

  async assert_visible(params) {
    const t = await target(params)
    await expect(t.locator).toBeVisible({ timeout: timeoutOf(params) })
    return { locator: t.desc }
  },

  async assert_text(params) {
    if (!params.text) throw new Error('assert_text 需要 text 参数')
    // Without a ref, check the whole page body.
    const t = params.ref
      ? await describeRef(params.ref)
      : { desc: { by: 'css', selector: 'body' }, locator: requirePage().locator('body') }
    await expect(t.locator).toContainText(String(params.text), { timeout: timeoutOf(params) })
    return { locator: t.desc }
  },

  async close() {
    recording = false
    if (browser) await browser.close().catch(() => {})
    browser = null
    page = null
    return {}
  },
}

const send = (msg) => process.stdout.write(JSON.stringify(msg) + '\n')

async function handle(line) {
  let req
  try {
    req = JSON.parse(line)
  } catch {
    return
  }
  try {
    const fn = handlers[req.method]
    if (!fn) throw new Error(`unknown method ${req.method}`)
    const result = await fn(req.params ?? {})
    send({ id: req.id, result: result ?? {} })
  } catch (err) {
    send({ id: req.id, error: String(err?.message ?? err).slice(0, 500) })
  }
  if (req.method === 'close') process.exit(0)
}

// Requests are handled one at a time, in arrival order.
let queue = Promise.resolve()
const rl = readline.createInterface({ input: process.stdin })
rl.on('line', (line) => {
  queue = queue.then(() => handle(line))
})
rl.on('close', () => {
  queue = queue.then(async () => {
    if (browser) await browser.close().catch(() => {})
    process.exit(0)
  })
})
