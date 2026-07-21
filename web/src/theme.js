/**
 * Client-side theme: CSS variables via documentElement[data-theme].
 * Keep THEME_IDS in sync with the FOUC snippet in index.html.
 */

export const STORAGE_KEY = 'vidlens-theme'
export const THEME_CHANGE_EVENT = 'vidlens-theme-change'
export const DEFAULT_THEME = 'dark'

/** @type {readonly string[]} */
export const THEME_IDS = Object.freeze(['dark', 'light', 'warm', 'soft'])

/** @type {readonly { id: string, label: string, hint: string, swatch: string }[]} */
export const THEME_OPTIONS = Object.freeze([
  { id: 'dark', label: '深色', hint: '深底青绿，默认', swatch: '#07090f' },
  { id: 'light', label: '浅色', hint: '浅灰底、深色字', swatch: '#f4f6fa' },
  { id: 'warm', label: '暖色', hint: '暖米底、琥珀强调', swatch: '#f7f1e8' },
  { id: 'soft', label: '护眼', hint: '低蓝光柔和绿灰', swatch: '#eef2ec' },
])

/**
 * @param {unknown} value
 * @returns {string}
 */
export function normalizeTheme(value) {
  if (typeof value !== 'string') return DEFAULT_THEME
  const id = value.trim().toLowerCase()
  return THEME_IDS.includes(id) ? id : DEFAULT_THEME
}

/**
 * @param {string} themeId
 * @returns {'dark' | 'light'}
 */
export function colorSchemeForTheme(themeId) {
  return themeId === 'dark' ? 'dark' : 'light'
}

/**
 * @param {{ getItem?: (k: string) => string | null } | null | undefined} storage
 * @returns {string}
 */
export function getStoredTheme(storage = typeof localStorage !== 'undefined' ? localStorage : null) {
  try {
    return normalizeTheme(storage?.getItem?.(STORAGE_KEY))
  } catch {
    return DEFAULT_THEME
  }
}

/**
 * Apply theme to the document (no persistence).
 * @param {string} themeId
 * @param {Document | null | undefined} doc
 */
export function applyTheme(themeId, doc = typeof document !== 'undefined' ? document : null) {
  const id = normalizeTheme(themeId)
  if (!doc?.documentElement) return id
  doc.documentElement.setAttribute('data-theme', id)
  doc.documentElement.style.colorScheme = colorSchemeForTheme(id)
  return id
}

/**
 * Persist + apply + notify.
 * @param {string} themeId
 * @param {{
 *   storage?: { getItem?: Function, setItem?: Function } | null,
 *   doc?: Document | null,
 *   dispatch?: boolean,
 * }} [opts]
 * @returns {string} normalized theme id
 */
export function setTheme(themeId, opts = {}) {
  const storage = opts.storage !== undefined
    ? opts.storage
    : (typeof localStorage !== 'undefined' ? localStorage : null)
  const doc = opts.doc !== undefined
    ? opts.doc
    : (typeof document !== 'undefined' ? document : null)
  const dispatch = opts.dispatch !== false

  const id = applyTheme(themeId, doc)
  try {
    storage?.setItem?.(STORAGE_KEY, id)
  } catch {
    // private mode / quota — still apply in-memory
  }

  if (dispatch && typeof window !== 'undefined') {
    try {
      window.dispatchEvent(new CustomEvent(THEME_CHANGE_EVENT, { detail: { theme: id } }))
    } catch {
      // ignore
    }
  }
  return id
}

/**
 * Read storage and apply (boot path).
 * @param {{ storage?: object | null, doc?: Document | null }} [opts]
 * @returns {string}
 */
export function initTheme(opts = {}) {
  const id = getStoredTheme(opts.storage)
  return applyTheme(id, opts.doc)
}
