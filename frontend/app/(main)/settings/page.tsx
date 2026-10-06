import { useCallback, useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { User } from '@/lib/types'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { PageHeading } from '@/components/product/PageHeading'
import { AIProfilesSection } from '@/components/settings/AIProfilesSection'
import { OptionalCapabilitiesSection } from '@/components/settings/OptionalCapabilitiesSection'
import { MemorySection } from '@/components/settings/MemorySection'
import { PromptPreferencesSection } from '@/components/settings/PromptPreferencesSection'
import { useTheme } from '@/components/theme/ThemeProvider'
import type { ThemeMode } from '@/lib/theme'

export default function SettingsPage() {
  useCrumb([{ label: '设置' }])
  const [tab, setTab] = useState<'ai' | 'mem' | 'prompts'>('ai')
  const [user, setUser] = useState<User | null>(null)
  const [capabilityRevision, setCapabilityRevision] = useState(0)
  const refreshCapabilities = useCallback(() => setCapabilityRevision(value => value + 1), [])
  const { theme, setTheme } = useTheme()
  const { confirmLeave } = useShell()

  useEffect(() => {
    let active = true
    api.profile().then(u => { if (active) setUser(u) }).catch(() => { /* 未取到用户时不阻塞两个 tab 的只读展示 */ })
    return () => { active = false }
  }, [])

  return (
    <div className="page">
      <PageHeading title="设置"
        description="外观、AI 服务、记忆与提示词偏好，都在这里调整。改动只影响之后的新请求。" />
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
          <button className={tab === 'ai' ? 'on' : ''} onClick={() => { if (confirmLeave()) setTab('ai') }}>AI 服务</button>
          <button className={tab === 'mem' ? 'on' : ''} onClick={() => { if (confirmLeave()) setTab('mem') }}>记忆治理</button>
          <button className={tab === 'prompts' ? 'on' : ''} onClick={() => { if (confirmLeave()) setTab('prompts') }}>提示词</button>
        </div>
        <div>
          {tab === 'ai' ? <><OptionalCapabilitiesSection readOnly={!user || user.role === 'DEMO'} refreshKey={capabilityRevision} /><AIProfilesSection readOnly={!user || user.role === 'DEMO'} onChanged={refreshCapabilities} /></> : tab === 'prompts' ? <PromptPreferencesSection readOnly={!user || user.role === 'DEMO'} /> : <MemorySection user={user} />}
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
