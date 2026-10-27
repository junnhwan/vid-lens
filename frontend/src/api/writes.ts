import type { GenerationInput } from "../contract/types"

export function restoreGenerationInput(saved: GenerationInput): GenerationInput {
  return {
    ...(saved.purpose !== undefined ? { purpose: saved.purpose } : {}),
    ...(saved.model_id !== undefined ? { model_id: saved.model_id } : {}),
    ...(saved.model_revision_id !== undefined ? { model_revision_id: saved.model_revision_id } : {}),
    ...(saved.detail_level !== undefined ? { detail_level: saved.detail_level } : {}),
    ...(saved.regenerate !== undefined ? { regenerate: saved.regenerate } : {}),
  }
}

export interface PendingWrite {
  slot: string
  key: string
  path: string
  body: string
  expiresAt: number | null
  startedAt: number
}

export interface KeyValue {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
  removeItem(key: string): void
}

const storageKey = "vidlens.pending-writes"

export function memoryStore(initial?: Record<string, string>): KeyValue {
  const data = { ...initial }
  return {
    getItem: (key) => (key in data ? data[key] : null),
    setItem: (key, value) => {
      data[key] = value
    },
    removeItem: (key) => {
      delete data[key]
    },
  }
}

function readAll(store: KeyValue): PendingWrite[] {
  const raw = store.getItem(storageKey)
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw) as PendingWrite[]
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function writeAll(store: KeyValue, items: PendingWrite[]) {
  if (!items.length) store.removeItem(storageKey)
  else store.setItem(storageKey, JSON.stringify(items))
}

export function newIdempotencyKey(): string {
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  return [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("")
}

export function rememberWrite(store: KeyValue, write: PendingWrite) {
  const items = readAll(store).filter((item) => item.slot !== write.slot)
  items.push(write)
  writeAll(store, items)
}

export function forgetWrite(store: KeyValue, slot: string) {
  writeAll(store, readAll(store).filter((item) => item.slot !== slot))
}

export function clearWrites(store: KeyValue) {
  store.removeItem(storageKey)
}

export function pendingWrite(store: KeyValue, slot: string): PendingWrite | null {
  return readAll(store).find((item) => item.slot === slot) ?? null
}

export function canReplay(write: PendingWrite, now: number): boolean {
  if (write.expiresAt == null) return true
  return now <= write.expiresAt
}
