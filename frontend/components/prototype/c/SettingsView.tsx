'use client'

import { useState } from 'react'
import Link from 'next/link'
import {
  ArrowLeft, BookOpen, Plus, MessageCircle, CheckCircle, XCircle,
  PlugZap, Star, Save, Eye, List,
} from 'lucide-react'
import type { AIProfile } from '@/lib/types'
import { ProtoShell, PageHero } from '@/components/prototype/c/Shell'

type Tab = 'asr' | 'llm' | 'embedding'
const TABS: { key: Tab; label: string }[] = [
  { key: 'asr', label: 'ASR · 转写' },
  { key: 'llm', label: 'LLM · 摘要问答' },
  { key: 'embedding', label: 'Embedding · 索引' },
]

interface Props {
  profiles: AIProfile[]
  loading: boolean
  error: string
}

export default function SettingsView({ profiles, loading, error }: Props) {
  const [tab, setTab] = useState<Tab>('asr')
  const [selectedId, setSelectedId] = useState<number | null>(profiles[0]?.id ?? null)
  const selected = profiles.find(p => p.id === selectedId) ?? null

  return (
    <ProtoShell active="settings">
      <PageHero
        kicker="设置 · BYOK"
        title="AI 服务配置"
        desc="自带密钥。一个 Profile 同时包含 ASR / LLM / Embedding 三组配置，任务消费时按所属用户解析。"
      />

      <div className="px-8 pb-3">
        <div className="flex items-center gap-1 border-b border-stone-200">
          {TABS.map(t => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className={`px-4 py-2.5 text-[12px] border-b-2 -mb-px transition-colors duration-200 ${
                tab === t.key ? 'border-amber-600 text-stone-900 font-medium' : 'border-transparent text-stone-400 hover:text-stone-600'
              }`}
            >
              {t.label}
            </button>
          ))}
          <button className="ml-auto h-8 px-3 rounded-lg border border-stone-300 text-[11px] flex items-center gap-1 proto-btn-lift">
            <Plus className="w-3 h-3" />新建 Profile
          </button>
        </div>
      </div>

      <main className="flex-1 overflow-y-auto px-8 pb-24">
        {error && <div className="mb-4 py-3 px-4 text-[13px] text-amber-800 bg-amber-50 border border-amber-200 rounded-lg">{error}</div>}

        <div className="grid lg:grid-cols-[1fr_1.4fr] gap-8">
          {/* 左：列表 */}
          <section className="proto-fade-in">
            <div className="text-[10px] uppercase tracking-wider text-stone-400 mb-3">{TABS.find(t => t.key === tab)?.label} · {profiles.length} 个</div>
            {loading ? (
              <div className="space-y-2">{[0, 1].map(i => <div key={i} className="h-16 bg-stone-100 rounded-lg animate-pulse" />)}</div>
            ) : (
              <ul className="space-y-2">
                {profiles.map((p, i) => {
                  const g = groupField(p, tab)
                  const sel = selectedId === p.id
                  return (
                    <li key={p.id}>
                      <button
                        onClick={() => setSelectedId(p.id)}
                        className={`w-full text-left p-4 rounded-xl border transition-all duration-200 proto-card-hover ${
                          sel ? 'border-amber-400/60 bg-amber-50/60 shadow-sm' : 'border-stone-200 bg-white hover:border-stone-300'
                        }`}
                        style={{ animationDelay: `${i * 50}ms` }}
                      >
                        <div className="flex items-center gap-2">
                          <span className="text-[15px] font-medium text-stone-900">{p.name}</span>
                          {p.is_default && (
                            <span className="text-[9px] px-1.5 py-0.5 rounded bg-amber-100 text-amber-800 border border-amber-200">默认</span>
                          )}
                        </div>
                        <div className="text-[11px] text-stone-500 mt-1 font-mono truncate">{g.provider} · {g.model || '未配置'}</div>
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
          </section>

          {/* 右：表单 */}
          <section className="proto-slide-in-right">
            {selected ? (
              <ProfileFormMock profile={selected} tab={tab} />
            ) : (
              <div className="border border-dashed border-stone-300 rounded-xl py-16 text-center text-[13px] text-stone-400">
                从左侧选择一个 Profile
              </div>
            )}
          </section>
        </div>
      </main>
    </ProtoShell>
  )
}

function groupField(p: AIProfile, tab: Tab) {
  if (tab === 'asr') return { provider: p.asr_provider, base: p.asr_base_url, model: p.asr_model, key: p.asr_api_key_masked }
  if (tab === 'llm') return { provider: p.llm_provider, base: p.llm_base_url, model: p.llm_model, key: p.llm_api_key_masked }
  return { provider: p.embedding_provider, base: p.embedding_endpoint, model: p.embedding_model, key: p.embedding_api_key_masked }
}

function ProfileFormMock({ profile, tab }: { profile: AIProfile; tab: Tab }) {
  const g = groupField(profile, tab)
  const [showKey, setShowKey] = useState(false)
  const [testOk, setTestOk] = useState<boolean | null>(null)

  return (
    <div className="bg-white border border-stone-200 rounded-xl p-6 space-y-4 proto-fade-in">
      <div className="flex items-center justify-between">
        <div className="text-[10px] uppercase tracking-wider text-stone-400">编辑 · {profile.name}</div>
        <Link href="/settings" className="text-[10px] text-stone-400 hover:text-stone-600">→ 正式设置页</Link>
      </div>

      <Field label="Profile 名称" value={profile.name} disabled />
      <Field label="Provider" value={g.provider} placeholder="openai / mimo / siliconflow" />
      <Field label={tab === 'embedding' ? 'Endpoint' : 'Base URL'} value={g.base} placeholder="https://api.example.com/v1" />
      <div>
        <label className="block text-[10px] uppercase tracking-wider text-stone-400 mb-1.5">API Key</label>
        <div className="flex items-center gap-2 h-10 px-3 rounded-lg border border-stone-200 bg-stone-50">
          <input type={showKey ? 'text' : 'password'} defaultValue={g.key} className="flex-1 bg-transparent text-[13px] font-mono" readOnly />
          <button onClick={() => setShowKey(s => !s)} className="text-stone-400 hover:text-stone-600"><Eye className="w-3.5 h-3.5" /></button>
        </div>
        <p className="text-[10px] text-stone-400 mt-1">加密存储，使用 VIDLENS_API_KEY_SECRET</p>
      </div>
      <Field label="Model" value={g.model} placeholder="模型名" suffix={<button className="text-[10px] text-stone-400 hover:text-stone-600 flex items-center gap-1"><List className="w-3 h-3" />探测</button>} />

      <div className="flex gap-2 pt-2">
        <button
          onClick={() => setTestOk(true)}
          className="h-9 px-4 rounded-lg bg-stone-900 text-white text-[12px] flex items-center gap-1.5 proto-btn-lift"
        >
          <PlugZap className="w-3.5 h-3.5" />测试连通
        </button>
        {tab === 'embedding' && (
          <button className="h-9 px-3 rounded-lg border border-stone-300 text-[12px] text-stone-600 proto-btn-lift">探测维度</button>
        )}
      </div>

      {testOk === true && (
        <div className="flex items-center gap-2 px-3 py-2.5 rounded-lg bg-emerald-50 border border-emerald-200 text-emerald-700 text-[12px] proto-fade-in">
          <CheckCircle className="w-4 h-4" />连通成功（原型演示）
        </div>
      )}
      {testOk === false && (
        <div className="flex items-center gap-2 px-3 py-2.5 rounded-lg bg-red-50 border border-red-200 text-red-700 text-[12px]">
          <XCircle className="w-4 h-4" />连通失败
        </div>
      )}

      <div className="flex gap-2 pt-3 border-t border-stone-100">
        <button className="h-9 px-4 rounded-lg bg-stone-900 text-white text-[12px] flex items-center gap-1.5 proto-btn-lift">
          <Save className="w-3.5 h-3.5" />保存
        </button>
        <button className="h-9 px-3 rounded-lg border border-stone-300 text-[12px] flex items-center gap-1.5 proto-btn-lift">
          <Star className="w-3.5 h-3.5" />设为默认
        </button>
      </div>
    </div>
  )
}

function Field({ label, value, placeholder, disabled, suffix }: {
  label: string; value?: string; placeholder?: string; disabled?: boolean; suffix?: React.ReactNode
}) {
  return (
    <div>
      <label className="block text-[10px] uppercase tracking-wider text-stone-400 mb-1.5">{label}</label>
      <div className="flex items-center gap-2 h-10 px-3 rounded-lg border border-stone-200 bg-white focus-within:ring-2 focus-within:ring-amber-600/20 focus-within:border-amber-400 transition-shadow">
        <input
          defaultValue={value}
          placeholder={placeholder}
          disabled={disabled}
          className="flex-1 bg-transparent text-[13px] disabled:text-stone-400"
        />
        {suffix}
      </div>
    </div>
  )
}
