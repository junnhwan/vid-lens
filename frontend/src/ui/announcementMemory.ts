const storageKey = "vidlens.announcement.read"

// Keep dismissal working in this page even when browser storage is unavailable.
export function announcementMemory(storage: () => Pick<Storage, "getItem" | "setItem">) {
  const read = new Set<string>()
  return {
    hasRead(version: string) {
      if (read.has(version)) return true
      try { return storage().getItem(storageKey) === version }
      catch { return false }
    },
    markRead(version: string) {
      read.add(version)
      try { storage().setItem(storageKey, version) }
      catch { /* The in-memory record still suppresses repeat displays. */ }
    },
  }
}
