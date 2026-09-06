'use client'

import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { User } from '@/lib/types'
import { useCrumb } from '@/components/shell/AppShell'
import { AIProfilesSection } from '@/components/settings/AIProfilesSection'
import { MemorySection } from '@/components/settings/MemorySection'
import { useTheme } from '@/components/theme/ThemeProvider'
import type { ThemeMode } from '@/lib/theme'

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
      <div className="theme-pick-block">
        <div className="section-head" style={{ marginTop: 0 }}>
          <h2>外观</h2>
        </div>
        <div className="theme-pick">
          <ThemeCard mode="dark" label="深色" hint="放映厅" active={theme === 'dark'} onSelect={() => setTheme('dark')} />
          <ThemeCard mode="light" label="浅色" hint="阅读" active={theme === 'light'} onSelect={() => setTheme('light')} />
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

function ThemeCard({ mode, label, hint, active, onSelect }: {
  mode: ThemeMode
  label: string
  hint: string
  active: boolean
  onSelect: () => void
}) {
  return (
    <button type="button" className={`theme-card${active ? ' on' : ''}`} onClick={onSelect}>
      <span className={`theme-swatch theme-swatch-${mode}`} aria-hidden="true">
        <span className="theme-swatch-bar" />
        <span className="theme-swatch-line" />
        <span className="theme-swatch-line short" />
      </span>
      <span className="theme-card-copy">
        <b>{label}</b>
        <span>{hint}</span>
      </span>
    </button>
  )
}
