'use client'

import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { User } from '@/lib/types'
import { useCrumb } from '@/components/shell/AppShell'
import { AIProfilesSection } from '@/components/settings/AIProfilesSection'
import { MemorySection } from '@/components/settings/MemorySection'

// 设置页:BYOK AI 服务配置 + 记忆治理,左侧导航两个 tab。
// 演示账号(role=DEMO)在后端对所有写操作直接拒绝,这里以只读态呈现,不做假象。

export default function SettingsPage() {
  useCrumb(['设置'])
  const [tab, setTab] = useState<'ai' | 'mem'>('ai')
  const [user, setUser] = useState<User | null>(null)

  useEffect(() => {
    let active = true
    api.profile().then(u => { if (active) setUser(u) }).catch(() => { /* 未取到用户时不阻塞两个 tab 的只读展示 */ })
    return () => { active = false }
  }, [])

  return (
    <div className="page">
      <div className="section-head" style={{ marginTop: 0 }}><h2>设置</h2></div>
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
