import assert from 'node:assert/strict'

import {
  DEFAULT_THEME,
  STORAGE_KEY,
  THEME_IDS,
  THEME_OPTIONS,
  applyTheme,
  colorSchemeForTheme,
  getStoredTheme,
  initTheme,
  normalizeTheme,
  setTheme,
} from '../src/theme.js'

function createStorage(initial = {}) {
  const state = new Map(Object.entries(initial))
  return {
    getItem: (key) => (state.has(key) ? state.get(key) : null),
    setItem: (key, value) => {
      state.set(key, String(value))
    },
    _state: state,
  }
}

function createDoc() {
  const attrs = new Map()
  const style = {}
  return {
    documentElement: {
      setAttribute: (k, v) => attrs.set(k, v),
      getAttribute: (k) => attrs.get(k) ?? null,
      style,
    },
    _attrs: attrs,
  }
}

assert.equal(normalizeTheme(null), DEFAULT_THEME)
assert.equal(normalizeTheme(''), DEFAULT_THEME)
assert.equal(normalizeTheme('nope'), DEFAULT_THEME)
assert.equal(normalizeTheme('DARK'), 'dark')
assert.equal(normalizeTheme(' light '), 'light')
assert.equal(normalizeTheme('warm'), 'warm')
assert.equal(normalizeTheme('soft'), 'soft')

for (const id of THEME_IDS) {
  assert.ok(THEME_OPTIONS.some((o) => o.id === id), `THEME_OPTIONS should include ${id}`)
}

assert.equal(colorSchemeForTheme('dark'), 'dark')
assert.equal(colorSchemeForTheme('light'), 'light')
assert.equal(colorSchemeForTheme('warm'), 'light')
assert.equal(colorSchemeForTheme('soft'), 'light')

{
  const storage = createStorage({ [STORAGE_KEY]: 'warm' })
  assert.equal(getStoredTheme(storage), 'warm')
}

{
  const storage = createStorage({ [STORAGE_KEY]: 'bogus' })
  assert.equal(getStoredTheme(storage), DEFAULT_THEME)
}

{
  const storage = createStorage()
  assert.equal(getStoredTheme(storage), DEFAULT_THEME)
}

{
  const doc = createDoc()
  const id = applyTheme('light', doc)
  assert.equal(id, 'light')
  assert.equal(doc.documentElement.getAttribute('data-theme'), 'light')
  assert.equal(doc.documentElement.style.colorScheme, 'light')
}

{
  const storage = createStorage()
  const doc = createDoc()
  const id = setTheme('soft', { storage, doc, dispatch: false })
  assert.equal(id, 'soft')
  assert.equal(storage.getItem(STORAGE_KEY), 'soft')
  assert.equal(doc.documentElement.getAttribute('data-theme'), 'soft')
  assert.equal(doc.documentElement.style.colorScheme, 'light')
}

{
  const storage = createStorage({ [STORAGE_KEY]: 'warm' })
  const doc = createDoc()
  const id = initTheme({ storage, doc })
  assert.equal(id, 'warm')
  assert.equal(doc.documentElement.getAttribute('data-theme'), 'warm')
}

{
  const storage = createStorage()
  const doc = createDoc()
  setTheme('not-a-theme', { storage, doc, dispatch: false })
  assert.equal(storage.getItem(STORAGE_KEY), DEFAULT_THEME)
  assert.equal(doc.documentElement.getAttribute('data-theme'), DEFAULT_THEME)
}

console.log('theme.test.mjs: ok')
