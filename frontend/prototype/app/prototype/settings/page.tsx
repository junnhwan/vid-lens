'use client'

import { useCallback, useEffect, useState } from 'react'
import SettingsView from '@/components/prototype/c/SettingsView'
import { api, ApiError } from '@/lib/api'
import type { AIProfile } from '@/lib/types'
import { MOCK_PROFILES } from '@/components/prototype/c/mocks'

export default function PrototypeSettingsPage() {
  const [profiles, setProfiles] = useState<AIProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await api.listProfiles()
      if (list.length > 0) {
        setProfiles(list)
        setError('')
      } else {
        setProfiles(MOCK_PROFILES)
        setError('暂无真实数据 — 展示 mock')
      }
    } catch (e) {
      setProfiles(MOCK_PROFILES)
      setError(e instanceof ApiError ? e.message : '加载失败 — 展示 mock')
    } finally { setLoading(false) }
  }, [])

  useEffect(() => { load() }, [load])

  return <SettingsView profiles={profiles} loading={loading} error={error} />
}
