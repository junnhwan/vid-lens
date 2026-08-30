'use client'

import { useCallback, useMemo, useState } from 'react'
import {
  SEED_MEMORIES,
  countByStatus,
  conflictKey,
  type MemoryItem,
} from '@/components/prototype/memory/types'

export function useMemoryDemo() {
  const [items, setItems] = useState<MemoryItem[]>(SEED_MEMORIES)

  const withdraw = useCallback((id: string) => {
    setItems(prev => prev.map(item => (
      item.id === id ? { ...item, status: 'withdrawn', version: item.version + 1 } : item
    )))
  }, [])

  const remove = useCallback((id: string) => {
    setItems(prev => prev.filter(item => item.id !== id))
  }, [])

  const keep = useCallback((id: string) => {
    setItems(prev => {
      const target = prev.find(item => item.id === id)
      if (!target) return prev
      const key = conflictKey(target)
      return prev.map(item => {
        if (item.id === id) return { ...item, status: 'active' as const, version: item.version + 1 }
        if (conflictKey(item) === key && item.status === 'conflicted') {
          return { ...item, status: 'withdrawn' as const, version: item.version + 1 }
        }
        return item
      })
    })
  }, [])

  const reset = useCallback(() => setItems(SEED_MEMORIES), [])

  const counts = useMemo(() => countByStatus(items), [items])

  return { items, counts, withdraw, remove, keep, reset }
}

export type MemoryDemo = ReturnType<typeof useMemoryDemo>
