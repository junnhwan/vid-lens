'use client'

import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { User } from '@/lib/types'
import { useCrumb } from '@/components/shell/AppShell'
import { AIProfilesSection } from '@/components/settings/AIProfilesSection'
import { MemorySection } from '@/components/settings/MemorySection'
import { useTheme } from '@/components/theme/ThemeProvider'
import { Icon } from '@/components/ui/Icon'

export default function SettingsPage() {
  useCrumb([{ label: '设置' }])
  const [tab, setTab] = useState<'ai' | 'mem'>('ai')
  const [user, setUser] = useState<User | null>(null)
  const { theme, setTheme } = useTheme()

  useEffect(() => {
    let active = true
    api.profile().then(u => { if (active) setUser(u) }).catch(() => { /* 未取到用户时不阻塞两个 tab 的只读展示 */ })
    return () => { active = false }
  }, [])

  return (
    <div className="page">
      <div className="section-head" style={{ marginTop: 0 }}><h2>设置</h2></div>
      <div className="pref-row" style={{ marginBottom: 22 }}>
        <div className="pr-body">
          <b>外观</b>
        </div>
        <div className="seg">
          <button className={theme === 'dark' ? 'on' : ''} onClick={() => setTheme('dark')}>
            <Icon name="moon" size="sm" />深色
          </button>
          <button className={theme === 'light' ? 'on' : ''} onClick={() => setTheme('light')}>
            <Icon name="sun" size="sm" />浅色
          </button>
        </div>
      </div>
      <div className="settings-grid">
        <div className="settings-nav">
          <button className={tab === 'ai' ? 'on' : ''} onClick={() => setTab('ai')}>AI 服务</button>
          <button className={tab === 'mem' ? 'on' : ''} onClick={() => setTab('mem')}>记忆治理</button>
        </div>
        <div>
          {tab === 'ai' ? <AIProfilesSection readOnly={user?.role === 'DEMO'} /> : <MemorySection user={user} />}
        </div>
      </div>
    </div>
  )
}
