import type { SummaryContextRef } from './summaryExperience'

// Ephemeral view state only. Runs, messages, accepted annotations and auth
// remain server-owned; nothing here is persisted to browser storage.
export interface SummaryMessageAnchor { messageID?: number; scrollTop: number; offset: number }
export interface SummaryReaderPosition { scrollTop: number; blockID?: string; offset?: number; contentDigest?: string }
export interface SummarySessionView { draft: string; contexts: SummaryContextRef[]; messageAnchor?: SummaryMessageAnchor }
export interface SummaryTaskView { sessionID: number | null; reader?: SummaryReaderPosition; summaryOpen: boolean }

const EMPTY_SESSION: SummarySessionView = { draft: '', contexts: [] }
Object.freeze(EMPTY_SESSION.contexts); Object.freeze(EMPTY_SESSION)
const EMPTY_TASK: SummaryTaskView = Object.freeze({ sessionID: null, summaryOpen: false })
const MAX_SESSIONS = 40, MAX_TASKS = 20, MAX_AGE_MS = 6 * 60 * 60 * 1000
type Entry<T> = { value: T; touched: number }
const sessions = new Map<string, Entry<SummarySessionView>>()
const tasks = new Map<string, Entry<SummaryTaskView>>()
const listeners = new Set<() => void>()
let owner: number | null = null
const taskKey = (userID: number, taskID: number) => `${userID}/${taskID}`
const sessionKey = (userID: number, taskID: number, sessionID: number | null) => `${taskKey(userID, taskID)}/${sessionID ?? 'new'}`
const eligible = (userID: number, taskID: number) => userID > 0 && taskID > 0 && owner === userID
function emit() { listeners.forEach(listener => listener()) }
function trim<T>(map: Map<string, Entry<T>>, max: number) {
  const cutoff = Date.now() - MAX_AGE_MS
  for (const [key, entry] of map) if (entry.touched < cutoff) map.delete(key)
  while (map.size > max) map.delete(map.keys().next().value!)
}
function write<T>(map: Map<string, Entry<T>>, key: string, value: T, max: number) {
  map.delete(key); map.set(key, { value, touched: Date.now() }); trim(map, max); emit()
}
export function clearSummaryViewState() { sessions.clear(); tasks.clear(); owner = null; emit() }
export function setSummaryViewOwner(userID: number | null) {
  if (owner === userID) return
  sessions.clear(); tasks.clear(); owner = userID; emit()
}
export function subscribeSummaryViewState(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } }
export function getSummaryTaskView(userID: number, taskID: number): SummaryTaskView {
  if (!eligible(userID, taskID)) return EMPTY_TASK
  const entry = tasks.get(taskKey(userID, taskID))
  return entry && entry.touched >= Date.now() - MAX_AGE_MS ? entry.value : EMPTY_TASK
}
export function getSummarySessionView(userID: number, taskID: number, sessionID: number | null): SummarySessionView {
  if (!eligible(userID, taskID)) return EMPTY_SESSION
  const entry = sessions.get(sessionKey(userID, taskID, sessionID))
  return entry && entry.touched >= Date.now() - MAX_AGE_MS ? entry.value : EMPTY_SESSION
}
export function patchSummaryTaskView(userID: number, taskID: number, patch: Partial<SummaryTaskView>) {
  if (!eligible(userID, taskID)) return
  const value = { ...getSummaryTaskView(userID, taskID), ...patch }
  if (patch.reader) value.reader = Object.freeze({ ...patch.reader })
  write(tasks, taskKey(userID, taskID), Object.freeze(value), MAX_TASKS)
}
export function patchSummarySessionView(userID: number, taskID: number, sessionID: number | null, patch: Partial<SummarySessionView>) {
  if (!eligible(userID, taskID)) return
  if (patch.draft !== undefined && Array.from(patch.draft).length > 4000) return
  if (patch.contexts && (patch.contexts.length > 3 || patch.contexts.some(ref => ref.task_id !== taskID) || patch.contexts.reduce((n, ref) => n + Array.from(ref.quote).length, 0) > 3000)) return
  const value = { ...getSummarySessionView(userID, taskID, sessionID), ...patch }
  if (patch.contexts) {
    value.contexts = patch.contexts.map(ref => Object.freeze({ ...ref, version_ref: Object.freeze({ ...ref.version_ref }) }))
    Object.freeze(value.contexts)
  }
  if (patch.messageAnchor) value.messageAnchor = Object.freeze({ ...patch.messageAnchor })
  write(sessions, sessionKey(userID, taskID, sessionID), Object.freeze(value), MAX_SESSIONS)
}
// The first send creates a server session. Carry only that unsent view state
// into its new ID; ordinary switching between existing sessions never copies.
export function promoteSummarySessionView(userID: number, taskID: number, sessionID: number) {
  if (!eligible(userID, taskID) || sessionID <= 0) return
  const pending = sessions.get(sessionKey(userID, taskID, null))
  if (pending) {
    sessions.delete(sessionKey(userID, taskID, null))
    write(sessions, sessionKey(userID, taskID, sessionID), pending.value, MAX_SESSIONS)
  }
  patchSummaryTaskView(userID, taskID, { sessionID })
}
