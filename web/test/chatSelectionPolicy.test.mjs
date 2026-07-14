import assert from 'node:assert/strict'

import {
  LAST_CHAT_TASK_KEY,
  parseChatTaskIdParam,
  readLastChatTaskId,
  resolveBareChatSelection,
  writeLastChatTaskId,
} from '../src/chatSelectionPolicy.js'

// --- memory store ---
function memStorage(seed = {}) {
  const map = new Map(Object.entries(seed))
  return {
    getItem: (k) => (map.has(k) ? map.get(k) : null),
    setItem: (k, v) => map.set(k, String(v)),
    removeItem: (k) => map.delete(k),
  }
}

assert.equal(readLastChatTaskId(memStorage()), null, 'empty storage → null')
assert.equal(readLastChatTaskId(memStorage({ [LAST_CHAT_TASK_KEY]: '42' })), 42)
assert.equal(readLastChatTaskId(memStorage({ [LAST_CHAT_TASK_KEY]: 'nope' })), null)

const store = memStorage()
writeLastChatTaskId(7, store)
assert.equal(store.getItem(LAST_CHAT_TASK_KEY), '7')
writeLastChatTaskId(null, store)
assert.equal(store.getItem(LAST_CHAT_TASK_KEY), null)

// --- bare /chat selection ---
assert.equal(
  resolveBareChatSelection({ lastTaskId: 3, chattableTaskIds: [1, 2, 3] }),
  3,
  'remembered id still chattable → use it',
)

assert.equal(
  resolveBareChatSelection({ lastTaskId: 9, chattableTaskIds: [1, 2, 3] }),
  null,
  'remembered id gone → do NOT fall back to first of many',
)

assert.equal(
  resolveBareChatSelection({ lastTaskId: null, chattableTaskIds: [1, 2, 3] }),
  null,
  'no memory + multiple videos → null (user picks)',
)

assert.equal(
  resolveBareChatSelection({ lastTaskId: null, chattableTaskIds: [5] }),
  5,
  'single chattable video → auto-select',
)

assert.equal(
  resolveBareChatSelection({ lastTaskId: 5, chattableTaskIds: [5] }),
  5,
  'single video matches memory',
)

assert.equal(
  resolveBareChatSelection({ lastTaskId: null, chattableTaskIds: [] }),
  null,
  'empty list → null',
)

// --- route param ---
assert.deepEqual(parseChatTaskIdParam(undefined), { ok: false, missing: true })
assert.deepEqual(parseChatTaskIdParam(''), { ok: false, missing: true })
assert.deepEqual(parseChatTaskIdParam('abc'), { ok: false, invalid: true })
assert.deepEqual(parseChatTaskIdParam('12'), { ok: true, id: 12 })
assert.deepEqual(parseChatTaskIdParam(['8']), { ok: true, id: 8 })

console.log('chatSelectionPolicy tests passed')
