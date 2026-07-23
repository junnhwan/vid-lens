/**
 * Central Lucide icon keys for UI chrome.
 * Components render via VlIcon; never put emoji in user-visible icon slots.
 */

/** @typedef {string} IconKey */

export const ICON = Object.freeze({
  alert: 'alert-triangle',
  ban: 'ban',
  bot: 'bot',
  check: 'check',
  checkCircle: 'check-circle-2',
  chevronDown: 'chevron-down',
  chevronRight: 'chevron-right',
  chevronUp: 'chevron-up',
  clock: 'clock',
  copy: 'copy',
  download: 'download',
  film: 'clapperboard',
  help: 'help-circle',
  library: 'library',
  loader: 'loader-2',
  menu: 'menu',
  message: 'message-square',
  palette: 'palette',
  pause: 'pause',
  plus: 'plus',
  refresh: 'refresh-cw',
  rotate: 'rotate-cw',
  search: 'search',
  settings: 'settings',
  sparkles: 'sparkles',
  trash: 'trash-2',
  upload: 'upload',
  wifiOff: 'wifi-off',
  x: 'x',
  xCircle: 'x-circle',
  fileText: 'file-text',
  arrowLeft: 'arrow-left',
  arrowUp: 'arrow-up',
  empty: 'inbox',
  warn: 'circle-alert',
})

/**
 * Normalize legacy emoji / symbol icon props to Lucide keys.
 * @param {unknown} value
 * @param {string} [fallback]
 * @returns {string}
 */
export function resolveIconKey(value, fallback = ICON.alert) {
  if (value == null || value === '') return fallback
  const s = String(value).trim()
  if (!s) return fallback
  // Already a Lucide-style key
  if (/^[a-z][a-z0-9-]*$/i.test(s) && !/[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]/u.test(s)) {
    return s
  }
  const legacy = {
    '⚠️': ICON.alert,
    '⚠': ICON.alert,
    '🗑️': ICON.trash,
    '🗑': ICON.trash,
    '❌': ICON.xCircle,
    '✅': ICON.checkCircle,
    '❓': ICON.help,
    '⛔': ICON.ban,
    '🔄': ICON.refresh,
    '📝': ICON.fileText,
    '🤖': ICON.bot,
    '🔍': ICON.search,
    '📤': ICON.upload,
    '⬇️': ICON.download,
    '⬇': ICON.download,
    '⚙': ICON.settings,
    '⚙️': ICON.settings,
    '⏸️': ICON.pause,
    '⏸': ICON.pause,
    '⏳': ICON.clock,
    '↻': ICON.rotate,
    '📡': ICON.wifiOff,
    '✓': ICON.check,
    '⧉': ICON.copy,
  }
  return legacy[s] || fallback
}

/**
 * Status / stage → Lucide icon key (used by getDetailedStatus).
 * @type {Readonly<Record<string, string>>}
 */
export const STATUS_ICON = Object.freeze({
  unknown: ICON.help,
  dead: ICON.ban,
  retrying: ICON.refresh,
  failed: ICON.xCircle,
  completed: ICON.checkCircle,
  pending: ICON.pause,
  queued: ICON.clock,
  running: ICON.loader,
  downloading: ICON.download,
  uploaded: ICON.upload,
  transcribing: ICON.fileText,
  summarizing: ICON.sparkles,
  indexing: ICON.search,
})
