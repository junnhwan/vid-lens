/**
 * Proves shipped icon helpers return Lucide keys (not emoji) and resolve legacy symbols.
 * Drives real modules: icons.js + format.js getDetailedStatus.
 */
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { ICON, STATUS_ICON, resolveIconKey } from '../src/icons.js'
import { getDetailedStatus } from '../src/utils/format.js'

const __dirname = dirname(fileURLToPath(import.meta.url))
const srcRoot = join(__dirname, '..', 'src')

const EMOJI_RE = /[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}\u{FE0F}]|⚠️|🗑️|✅|❌|📡|⚙|🤖|📝|🔄|📤|🔍|❓|⛔|⬇️|⏸️|⏳|✓|⧉/u

function assertLucideKey(value, label) {
  assert.equal(typeof value, 'string', `${label} should be string`)
  assert.ok(value.length > 0, `${label} should be non-empty`)
  assert.ok(/^[a-z][a-z0-9-]*$/i.test(value), `${label} should look like a Lucide key, got ${value}`)
  assert.ok(!EMOJI_RE.test(value), `${label} must not contain emoji: ${value}`)
}

// ICON constants are Lucide keys
for (const [k, v] of Object.entries(ICON)) {
  assertLucideKey(v, `ICON.${k}`)
}
for (const [k, v] of Object.entries(STATUS_ICON)) {
  assertLucideKey(v, `STATUS_ICON.${k}`)
}

// resolveIconKey normalizes legacy emoji → Lucide keys
assert.equal(resolveIconKey('⚠️'), ICON.alert)
assert.equal(resolveIconKey('🗑️'), ICON.trash)
assert.equal(resolveIconKey('⚙'), ICON.settings)
assert.equal(resolveIconKey('check-circle-2'), 'check-circle-2')
assert.equal(resolveIconKey(null), ICON.alert)

// getDetailedStatus (shipped) returns Lucide icon keys for all branches
const cases = [
  [null, STATUS_ICON.unknown],
  [{ status: 5 }, STATUS_ICON.dead],
  [{ status: 4, next_retry_at: '2099-01-01T00:00:00Z' }, STATUS_ICON.retrying],
  [{ status: 4 }, STATUS_ICON.failed],
  [{ status: 3 }, STATUS_ICON.completed],
  [{ status: 1, stage: 'downloading' }, STATUS_ICON.downloading],
  [{ status: 1, stage: 'uploaded' }, STATUS_ICON.uploaded],
  [{ status: 2, stage: 'transcribing' }, STATUS_ICON.transcribing],
  [{ status: 2, stage: 'summarizing' }, STATUS_ICON.summarizing],
  [{ status: 2, stage: 'indexing' }, STATUS_ICON.indexing],
  [{ status: 0 }, STATUS_ICON.pending],
  [{ status: 1 }, STATUS_ICON.queued],
  [{ status: 2 }, STATUS_ICON.running],
]

for (const [task, expectedIcon] of cases) {
  const result = getDetailedStatus(task)
  assertLucideKey(result.icon, `getDetailedStatus(${JSON.stringify(task)}).icon`)
  assert.equal(result.icon, expectedIcon)
}

// Structural: Lucide dependency, VlIcon, ImmersiveCanvas, package.json
const pkg = JSON.parse(readFileSync(join(__dirname, '..', 'package.json'), 'utf8'))
assert.ok(
  pkg.dependencies?.['lucide-vue-next'] || pkg.devDependencies?.['lucide-vue-next'],
  'package.json must declare lucide-vue-next',
)

const vlIcon = readFileSync(join(srcRoot, 'components', 'VlIcon.vue'), 'utf8')
assert.ok(vlIcon.includes('lucide-vue-next'), 'VlIcon must import lucide-vue-next')
assert.ok(vlIcon.includes('from \'lucide-vue-next\'') || vlIcon.includes('from "lucide-vue-next"'))

const canvas = readFileSync(join(srcRoot, 'components', 'ImmersiveCanvas.vue'), 'utf8')
assert.ok(canvas.includes('immersive-canvas'), 'ImmersiveCanvas shell class required')
assert.ok(canvas.includes('orb'), 'ImmersiveCanvas must provide atmosphere orbs')

const appVue = readFileSync(join(srcRoot, 'App.vue'), 'utf8')
assert.ok(appVue.includes('ImmersiveCanvas'), 'App shell must mount ImmersiveCanvas')
assert.ok(!appVue.includes('📡'), 'App offline toast must not use emoji icon')
assert.ok(!appVue.includes("'⚠️'") && !appVue.includes('"⚠️"'), 'App confirm defaults must not use emoji icons')

// Sample user-visible Vue paths: no emoji icon props / common UI emoji
const uiFiles = [
  'App.vue',
  'components/Navbar.vue',
  'components/ConfirmDialog.vue',
  'components/TaskCard.vue',
  'components/TaskList.vue',
  'components/Sidebar.vue',
  'components/VideoRAGChat.vue',
  'components/AIProfileEditor.vue',
  'components/TaskDetailPanel.vue',
  'views/SettingsView.vue',
  'views/ChatView.vue',
  'utils/format.js',
]

const emojiAsIcon = /icon:\s*['"][^'"]*[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}⚠️🗑️✅❌📡⚙🤖📝🔄📤🔍❓⛔⬇️⏸️⏳]/u
for (const rel of uiFiles) {
  const text = readFileSync(join(srcRoot, rel), 'utf8')
  assert.ok(!emojiAsIcon.test(text), `${rel} must not set emoji as icon: prop`)
  // bare emoji common UI decorations (allow markdown content pipelines only in comments is fine)
  const stripped = text
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '')
  // Fail if classic status/action emoji remain in templates/scripts
  const banned = ['⚠️', '🗑️', '📡', '🤖', '📝', '🔄', '📤', '🔍', '❓', '⛔', '✅', '❌']
  for (const e of banned) {
    assert.ok(!stripped.includes(e), `${rel} still contains UI emoji ${e}`)
  }
}

console.log('icons.test.mjs: ok')
