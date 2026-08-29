'use client'

import { useCallback, useEffect, useState } from 'react'
import KBListView from '@/components/prototype/c/KBListView'
import { api, ApiError } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { MOCK_KBS } from '@/components/prototype/c/mocks'

export default function PrototypeKBPage() {
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await api.listKBs()
      if (list.length > 0) {
        setKbs(list)
        setError('')
      } else {
        setKbs(MOCK_KBS)
        setError('暂无真实数据 — 展示 mock')
      }
    } catch (e) {
      setKbs(MOCK_KBS)
      setError(e instanceof ApiError ? e.message : '加载失败 — 展示 mock')
    } finally { setLoading(false) }
  }, [])

  useEffect(() => { load() }, [load])

  return <KBListView kbs={kbs} loading={loading} error={error} onRefresh={load} />
}
