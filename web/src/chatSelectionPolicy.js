/** localStorage key for last selected chat video task */
export const LAST_CHAT_TASK_KEY = 'vidlens:lastChatTaskId'

/**
 * @param {Storage} [storage]
 * @returns {number|null}
 */
export function readLastChatTaskId(storage = globalThis.localStorage) {
  try {
    if (!storage) return null
    const raw = storage.getItem(LAST_CHAT_TASK_KEY)
    if (raw == null || raw === '') return null
    const id = Number(raw)
    return Number.isFinite(id) ? id : null
  } catch {
    return null
  }
}

/**
 * @param {number|string|null|undefined} id
 * @param {Storage} [storage]
 */
export function writeLastChatTaskId(id, storage = globalThis.localStorage) {
  try {
    if (!storage) return
    if (id == null || id === '') {
      storage.removeItem(LAST_CHAT_TASK_KEY)
      return
    }
    const n = Number(id)
    if (!Number.isFinite(n)) return
    storage.setItem(LAST_CHAT_TASK_KEY, String(n))
  } catch {
    // ignore quota / private mode
  }
}

/**
 * When user opens bare /chat (no :taskId), pick which video to open.
 * Prefer last remembered id if still chattable; only auto-pick the sole video;
 * never force the first of many (that felt like a bug in the nav).
 *
 * @param {{ lastTaskId: number|null, chattableTaskIds: Array<number|string> }} args
 * @returns {number|null}
 */
export function resolveBareChatSelection({ lastTaskId, chattableTaskIds }) {
  const ids = (Array.isArray(chattableTaskIds) ? chattableTaskIds : [])
    .map((id) => Number(id))
    .filter((id) => Number.isFinite(id))

  if (lastTaskId != null) {
    const last = Number(lastTaskId)
    if (Number.isFinite(last) && ids.includes(last)) return last
  }
  if (ids.length === 1) return ids[0]
  return null
}

/**
 * Parse route :taskId param into a finite number, or null if missing/invalid.
 * @param {string|string[]|null|undefined} param
 * @returns {{ ok: true, id: number } | { ok: false, missing: true } | { ok: false, invalid: true }}
 */
export function parseChatTaskIdParam(param) {
  if (param == null || param === '') {
    return { ok: false, missing: true }
  }
  const raw = Array.isArray(param) ? param[0] : param
  const id = Number(raw)
  if (!Number.isFinite(id)) {
    return { ok: false, invalid: true }
  }
  return { ok: true, id }
}
