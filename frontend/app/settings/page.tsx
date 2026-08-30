'use client'

import { useState, useEffect, useCallback } from 'react'
import { Plus, Info, CheckCircle, XCircle, PlugZap, Ruler, List, Eye, Star, Save, Trash2, Loader2 } from 'lucide-react'
import AppShell, { PageHero } from '@/components/layout/AppShell'
import { api, ApiError } from '@/lib/api'
import {
  buildProfileRequest,
  emptyProfileGroups,
  profileToGroups,
  profileToRequest,
  validateProfileGroups,
  type ProfileGroupsDraft,
  type ProfileGroupDraft,
} from '@/lib/ai-profile'
import { useRole } from '@/lib/useRole'
import type { AIProfile, ProfilePurpose } from '@/lib/types'

// 设置页：ASR/LLM/Embedding 三 tab 作"筛选视角"。
// 后端一个 profile 同时含四组配置、is_default 单 bool、无 type 字段。
// tab 决定：① 列表高亮哪一组已配置；② 编辑表单编辑哪一组字段；③ 探测可用模型的 purpose。
// 设为默认仍是 profile 整体默认（后端会把其它 profile 的 is_default 置 false）。

type Tab = 'asr' | 'llm' | 'embedding'

const TAB_META: Record<Tab, { label: string; purpose: ProfilePurpose }> = {
  asr: { label: 'ASR · 转写', purpose: 'asr' },
  llm: { label: 'LLM · 摘要问答', purpose: 'llm' },
  embedding: { label: 'Embedding · 索引', purpose: 'embedding' },
}

export default function SettingsPage() {
  // 演示账号：整个页面替换为只读模型清单（只显示三个模型名），不显示地址/密钥/编辑入口。
  const { isDemo } = useRole()
  if (isDemo) return <DemoProfilesView />
  return <SettingsEditor />
}

function SettingsEditor() {
  const [tab, setTab] = useState<Tab>('asr')
  const [profiles, setProfiles] = useState<AIProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const [err, setErr] = useState('')
  const [showNewForm, setShowNewForm] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try { setProfiles(await api.listProfiles()); setErr('') } catch (e) { setErr(e instanceof ApiError ? e.message : '加载失败') } finally { setLoading(false) }
  }, [])
  useEffect(() => { load() }, [load])

  const selected = profiles.find(p => p.id === selectedId) || null
  // 演示账号(DEMO)的 profile 是只读的：隐藏新建/设默认/编辑表单，只展示模型清单。
  const readOnly = profiles.length > 0 && profiles.every(p => p.read_only)

  return (
    <AppShell>
      <PageHero
        kicker="设置 · BYOK"
        title="AI 服务配置"
        desc="自带密钥。一个 Profile 同时包含 ASR / LLM / Embedding 三组配置，任务消费时按所属用户解析。"
      />

      <div className="px-8 pb-3">
        <div className="flex items-center gap-1 border-b border-ink-0/8">
          {(Object.keys(TAB_META) as Tab[]).map(t => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`px-4 py-2.5 text-[12px] border-b-2 -mb-px transition-colors duration-200 ${
                tab === t ? 'border-sienna-500 text-ink-0 font-medium' : 'border-transparent text-ink-4 hover:text-ink-2'
              }`}
            >
              {TAB_META[t].label}
            </button>
          ))}
          {!readOnly && (
            <button
              onClick={() => { setShowNewForm(true); setSelectedId(null) }}
              className="ml-auto h-8 px-3 rounded-lg border border-ink-0/10 text-[11px] text-ink-2 flex items-center gap-1 ui-btn-lift hover:text-ink-0"
            >
              <Plus className="w-3 h-3" />新建 Profile
            </button>
          )}
        </div>
      </div>

      <main className="flex-1 overflow-y-auto scroll-thin px-8 pb-24">
        {err && (
          <div className="mb-4 py-3 px-4 text-[13px] text-amber-800 bg-amber-50 border border-amber-200 rounded-lg">
            {err}<button onClick={load} className="ml-2 underline">重试</button>
          </div>
        )}

        <div className="grid lg:grid-cols-[1fr_1.4fr] gap-8">
          <section className="ui-fade-in">
            <div className="text-[12px] text-ink-4 mb-3">
              {TAB_META[tab].label} · {profiles.length} 个
            </div>
            {loading ? (
              <div className="space-y-2">{[0, 1, 2].map(i => <div key={i} className="h-16 bg-paper-2 rounded-lg sk" />)}</div>
            ) : profiles.length === 0 ? (
              <div className="py-10 text-center border border-dashed border-ink-0/15 rounded-xl">
                <div className="text-[12px] text-ink-4 mb-2">暂无 Profile</div>
                {!readOnly && (
                  <button onClick={() => { setShowNewForm(true); setSelectedId(null) }} className="h-8 px-3 rounded-lg border border-ink-0/10 text-[11px] ui-btn-lift inline-flex items-center gap-1">
                    <Plus className="w-3 h-3" />新建
                  </button>
                )}
              </div>
            ) : (
              <ul className="space-y-2">
                {profiles.map((p, i) => {
                  const sel = selectedId === p.id
                  const hasGroup = Boolean(groupField(p, tab).provider)
                  return (
                    <li key={p.id}>
                      <div
                        role="button"
                        tabIndex={0}
                        onClick={() => { setSelectedId(p.id); setShowNewForm(false) }}
                        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setSelectedId(p.id); setShowNewForm(false) } }}
                        className={`w-full text-left p-4 rounded-xl transition-colors duration-200 ui-card-hover cursor-pointer ${
                          sel ? 'bg-sienna-500/8' : 'hover:bg-ink-0/4'
                        }`}
                        style={{ animationDelay: `${i * 50}ms` }}
                      >
                        <div className="flex items-center gap-2">
                          <span className="text-[15px] font-medium text-ink-0">{p.name}</span>
                          {p.is_default && (
                            <span className="text-[9px] px-1.5 py-0.5 rounded bg-sienna-500/10 text-sienna-700 border border-sienna-500/25">默认</span>
                          )}
                          {!hasGroup && (
                            <span className="text-[9px] text-ink-4 border border-ink-0/10 px-1 rounded">未配{tab.toUpperCase()}</span>
                          )}
                        </div>
                        <div className="text-[11px] text-ink-3 mt-1 font-mono truncate">
                          {readOnly ? (groupField(p, tab).model || '—') : `${groupField(p, tab).provider || '—'} · ${groupField(p, tab).base || '—'}`}
                        </div>
                        {!readOnly && !sel && !p.is_default && (
                          <button
                            onClick={(e) => { e.stopPropagation(); setDefault(p.id) }}
                            className="mt-2 text-[10px] text-ink-4 hover:text-ink-2"
                          >
                            设默认
                          </button>
                        )}
                      </div>
                    </li>
                  )
                })}
              </ul>
            )}
            <div className="mt-5 text-[10px] text-ink-4 leading-relaxed flex items-start gap-1.5">
              <Info className="w-3 h-3 mt-0.5 shrink-0" />
              <span>Create 时若该类无默认，将自动设为默认。设新默认会取消旧默认。</span>
            </div>
          </section>

          <section className="ui-fade-in">
            {readOnly ? (
              selected ? <ReadOnlyProfile profile={selected} /> : (
                <div className="border border-dashed border-ink-0/15 rounded-xl py-16 text-center text-[13px] text-ink-4">从左侧选择一个 Profile</div>
              )
            ) : showNewForm ? (
              <ProfileForm key="new" tab={tab} onChanged={load} onSaved={(id) => { setSelectedId(id); setShowNewForm(false) }} />
            ) : selected ? (
              <ProfileForm key={selected.id} tab={tab} profile={selected} onChanged={load} />
            ) : (
              <div className="border border-dashed border-ink-0/15 rounded-xl py-16 text-center text-[13px] text-ink-4">从左侧选择一个 Profile，或新建</div>
            )}
          </section>
        </div>
      </main>
    </AppShell>
  )

  async function setDefault(id: number) {
    const p = profiles.find(x => x.id === id)
    if (!p) return
    try {
      await api.updateProfile(id, profileToRequest(p, { is_default: true }))
      load()
    } catch (e) { setErr(e instanceof ApiError ? e.message : '设默认失败') }
  }
}

// 演示账号只读视图：整页只显示每个 profile 的三个模型名（LLM/ASR/Embedding）。
// 不显示服务地址、API Key、提供商，也没有任何输入框或新建/编辑/删除入口。
function DemoProfilesView() {
  const [profiles, setProfiles] = useState<AIProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')

  const load = useCallback(async () => {
    setLoading(true)
    try { setProfiles(await api.listProfiles()); setErr('') } catch (e) { setErr(e instanceof ApiError ? e.message : '加载失败') } finally { setLoading(false) }
  }, [])
  useEffect(() => { load() }, [load])

  const rows = (p: AIProfile) => [
    { label: 'LLM · 摘要问答', value: p.llm_model },
    { label: 'ASR · 转写', value: p.asr_model },
    { label: 'Embedding · 索引', value: p.embedding_model },
  ]

  return (
    <AppShell>
      <PageHero
        kicker="设置 · 演示账号"
        title="AI 模型"
        desc="演示账号仅展示当前使用的模型，配置只读，服务地址与密钥不对外显示。"
      />

      <main className="flex-1 overflow-y-auto scroll-thin px-8 pb-24">
        {err && (
          <div className="mb-4 py-3 px-4 text-[13px] text-amber-800 bg-amber-50 border border-amber-200 rounded-lg">
            {err}<button onClick={load} className="ml-2 underline">重试</button>
          </div>
        )}

        {loading ? (
          <div className="space-y-3">
            {[0, 1, 2].map(i => <div key={i} className="h-24 bg-paper-0 border border-ink-0/8 rounded-xl sk" />)}
          </div>
        ) : profiles.length === 0 ? (
          <div className="py-16 text-center text-[12px] text-ink-4">暂无模型配置</div>
        ) : (
          <div className="space-y-4">
            {profiles.map(p => (
              <div key={p.id} className="bg-paper-0 border border-ink-0/8 rounded-xl p-6 ui-card-hover">
                <div className="flex items-center gap-2 mb-4">
                  <span className="text-[18px] font-medium text-ink-0 ui-serif">{p.name}</span>
                  {p.is_default && (
                    <span className="text-[9px] px-1.5 py-0.5 rounded bg-sienna-500/10 text-sienna-700 border border-sienna-500/25">默认</span>
                  )}
                </div>
                <div className="space-y-3">
                  {rows(p).map(r => (
                    <div key={r.label} className="flex items-center justify-between gap-4 border-b border-ink-0/6 pb-2.5 last:border-0 last:pb-0">
                      <span className="text-[10px] text-ink-4 uppercase tracking-wider">{r.label}</span>
                      <span className="text-[12.5px] text-ink-2 font-mono">{r.value || '—'}</span>
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        )}
      </main>
    </AppShell>
  )
}

// 取 profile 在某 tab 下的字段组（asr/llm/embedding）
function groupField(p: AIProfile, tab: Tab): { provider: string; base: string; keyMasked: string; model: string } {
  if (tab === 'asr') return { provider: p.asr_provider, base: p.asr_base_url, keyMasked: p.asr_api_key_masked, model: p.asr_model }
  if (tab === 'llm') return { provider: p.llm_provider, base: p.llm_base_url, keyMasked: p.llm_api_key_masked, model: p.llm_model }
  return { provider: p.embedding_provider, base: p.embedding_endpoint, keyMasked: p.embedding_api_key_masked, model: p.embedding_model }
}

// 演示账号只读视图：只展示名字与模型名，不暴露服务地址/密钥，也不提供任何编辑入口。
function ReadOnlyProfile({ profile }: { profile: AIProfile }) {
  const rows: { label: string; value: string }[] = [
    { label: 'LLM · 摘要问答', value: profile.llm_model },
    { label: 'ASR · 转写', value: profile.asr_model },
    { label: 'Embedding · 索引', value: profile.embedding_model },
  ]
  return (
    <div className="space-y-4 ui-fade-in">
      <div className="text-[10px] uppercase tracking-wider text-stone-400">查看 · {profile.name}</div>
      <div className="bg-paper-0 border border-ink-0/8 rounded-xl p-5 space-y-3">
        <div className="flex items-center gap-2">
          <span className="text-[18px] font-medium text-ink-0 ui-serif">{profile.name}</span>
          {profile.is_default && (
            <span className="text-[9px] px-1.5 py-0.5 rounded bg-sienna-500/10 text-sienna-700 border border-sienna-500/25">默认</span>
          )}
        </div>
        {rows.map(r => (
          <div key={r.label} className="flex items-center justify-between gap-4 border-b border-ink-0/6 pb-2.5 last:border-0 last:pb-0">
            <span className="text-[10px] text-ink-4 uppercase tracking-wider">{r.label}</span>
            <span className="text-[12.5px] text-ink-2 font-mono">{r.value || '—'}</span>
          </div>
        ))}
        <p className="text-[11px] text-ink-4 pt-1">演示账号配置为只读，服务地址与密钥已隐藏。</p>
      </div>
    </div>
  )
}

// ============ 编辑/新建表单 ============
function ProfileForm({ tab, profile, onChanged, onSaved }: {
  tab: Tab
  profile?: AIProfile
  onChanged: () => void
  onSaved?: (id: number) => void
}) {
  const isEdit = !!profile
  const g = profile ? groupField(profile, tab) : { provider: '', base: '', keyMasked: '', model: '' }

  const [name, setName] = useState(profile?.name || '')
  const [groups, setGroups] = useState<ProfileGroupsDraft>(() => profile ? profileToGroups(profile) : emptyProfileGroups())
  const [showKey, setShowKey] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null)
  const [models, setModels] = useState<string[]>([])
  const [probeDim, setProbeDim] = useState<number | null>(null)

  const current = tab === 'embedding' ? groups.embedding : groups[tab]

  useEffect(() => {
    if (profile) {
      setGroups(profileToGroups(profile))
      setName(profile.name)
      setTestResult(null)
      setModels([])
      setProbeDim(null)
    }
  }, [profile])

  const updateCurrent = (patch: Partial<ProfileGroupDraft> & { dim?: number }) => {
    setGroups(prev => {
      if (tab === 'embedding') {
        return { ...prev, embedding: { ...prev.embedding, ...patch } }
      }
      return { ...prev, [tab]: { ...prev[tab], ...patch } }
    })
  }

  const fieldLabel = { provider: 'Provider', base: tab === 'embedding' ? 'Endpoint' : 'Base URL', key: 'API Key', model: 'Model' } as const

  const save = async () => {
    if (!name.trim()) { setErr('请输入 Profile 名称'); return }
    const validationErr = validateProfileGroups(groups, !isEdit)
    if (validationErr) { setErr(validationErr); return }
    setBusy(true); setErr('')
    try {
      const body = buildProfileRequest(name, groups, { is_default: profile?.is_default })
      if (isEdit && profile) {
        await api.updateProfile(profile.id, body)
        onChanged()
        setTestResult({ ok: true, msg: '已保存' })
      } else {
        const created = await api.createProfile(body)
        onSaved?.(created.id)
      }
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally { setBusy(false) }
  }

  const testConn = async () => {
    if (!profile) { setErr('请先保存 Profile 再测试'); return }
    setBusy(true); setErr(''); setTestResult(null)
    try {
      const r = await api.testProfile({ id: profile.id })
      setTestResult({ ok: r.ok, msg: r.ok ? '连通成功' : '连通失败' })
    } catch (e) {
      setTestResult({ ok: false, msg: e instanceof ApiError ? e.message : '测试失败' })
    } finally { setBusy(false) }
  }

  const probeModels = async () => {
    if (!current.base || !current.apiKey) { setErr('探测可用模型需先填 Base URL + API Key'); return }
    setBusy(true); setErr('')
    try {
      const r = await api.listModels(current.base, current.apiKey, profile?.id || 0, TAB_META[tab].purpose)
      setModels(r.models || [])
    } catch (e) { setErr(e instanceof ApiError ? e.message : '探测失败') } finally { setBusy(false) }
  }

  const probeDimFn = async () => {
    if (tab !== 'embedding') { setErr('仅 Embedding tab 可探测维度'); return }
    if (!current.base || !current.apiKey || !current.model) { setErr('探测维度需先填 Endpoint + API Key + Model'); return }
    setBusy(true); setErr('')
    try {
      const r = await api.probeEmbeddingDim(current.base, current.apiKey, current.model, profile?.id || 0)
      setProbeDim(r.dimension)
      updateCurrent({ dim: r.dimension })
    } catch (e) { setErr(e instanceof ApiError ? e.message : '探测失败') } finally { setBusy(false) }
  }

  const setDefault = async () => {
    if (!profile) return
    try {
      await api.updateProfile(profile.id, profileToRequest(profile, { is_default: true, groups }))
      onChanged()
    } catch (e) { setErr(e instanceof ApiError ? e.message : '设默认失败') }
  }
  const del = async () => {
    if (!profile) return
    if (!confirm(`删除 Profile「${profile.name}」？`)) return
    try { await api.deleteProfile(profile.id); onChanged() } catch (e) { setErr(e instanceof ApiError ? e.message : '删除失败') }
  }

  return (
    <div className="space-y-4 ui-fade-in">
      <div className="text-[12px] text-ink-4">
        {isEdit ? `编辑 · ${profile?.name}` : `新建 · 完整 Profile`}
        {!isEdit && <span className="block mt-1 text-[11px]">请在 LLM / ASR / Embedding 三个 Tab 下分别填写配置后创建。</span>}
      </div>
      <div className="bg-paper-0 border border-ink-0/8 rounded-xl p-6 space-y-4">
        <FormField label="Profile 名称">
          <input value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} className="ui-input disabled:text-ink-4" />
          {isEdit && <p className="text-[10px] text-ink-4 mt-1">名称创建后不可改；当前编辑 {TAB_META[tab].label} 这一组的配置，保存时提交完整 Profile。</p>}
        </FormField>
        <FormField label={fieldLabel.provider}>
          <input value={current.provider} onChange={(e) => updateCurrent({ provider: e.target.value })} placeholder="mimo / openai / siliconflow" className="ui-input font-mono" />
        </FormField>
        <FormField label={fieldLabel.base}>
          <input value={current.base} onChange={(e) => updateCurrent({ base: e.target.value })} placeholder="https://api.example.com/v1" className="ui-input font-mono" />
        </FormField>
        <FormField label={fieldLabel.key}>
          <div className="flex items-center gap-2 h-10 px-3 rounded-lg border border-ink-0/10 bg-paper-1">
            <input type={showKey ? 'text' : 'password'} value={current.apiKey} onChange={(e) => updateCurrent({ apiKey: e.target.value })} placeholder={isEdit ? '•••• 留空保留旧值' : 'sk-...'} className="flex-1 bg-transparent text-[13px] font-mono" />
            <button onClick={() => setShowKey(s => !s)} className="text-ink-4 hover:text-ink-2"><Eye className="w-3.5 h-3.5" /></button>
          </div>
          <p className="text-[10px] text-ink-4 mt-1">加密存储，使用 VIDLENS_API_KEY_SECRET。{isEdit && g.keyMasked ? `当前：${g.keyMasked}` : ''}</p>
        </FormField>
        <FormField label={fieldLabel.model}>
          <div className="flex items-center gap-2 h-10 px-3 rounded-lg border border-ink-0/10 bg-paper-0 focus-within:ring-2 focus-within:ring-sienna-500/20 focus-within:border-sienna-500">
            <input value={current.model} onChange={(e) => updateCurrent({ model: e.target.value })} placeholder="模型名" className="flex-1 bg-transparent text-[13px] font-mono" />
            <button onClick={probeModels} disabled={busy} className="text-[10px] text-ink-4 hover:text-ink-2 whitespace-nowrap flex items-center gap-1 disabled:opacity-50">
              <List className="w-3 h-3" />探测
            </button>
          </div>
          {models.length > 0 && (
            <div className="mt-2 border border-ink-0/10 rounded-lg p-2.5 text-[11px] text-ink-2 space-y-0.5">
              <div className="text-ink-4 text-[10px] mb-1">可用模型</div>
              {models.map(m => (
                <div key={m} className="cursor-pointer hover:text-sienna-700 font-mono" onClick={() => updateCurrent({ model: m })}>
                  {m}{m === current.model && <span className="text-sienna-700"> ← 当前</span>}
                </div>
              ))}
            </div>
          )}
        </FormField>
        {tab === 'embedding' && (
          <FormField label="Embedding 维度">
            <input
              type="number"
              min={1}
              value={groups.embedding.dim || ''}
              onChange={(e) => updateCurrent({ dim: Number(e.target.value) || 0 })}
              placeholder="如 1024"
              className="ui-input font-mono"
            />
            <p className="text-[10px] text-ink-4 mt-1">须与 rag.embedding_dim 及模型实际输出维度一致。</p>
          </FormField>
        )}

        <div className="flex items-center gap-2 pt-1">
          <button onClick={testConn} disabled={busy || !isEdit} className="h-8 px-3.5 rounded-lg bg-ink-0 text-paper-0 text-[11px] flex items-center gap-1.5 ui-btn-lift disabled:opacity-50">
            <PlugZap className="w-3.5 h-3.5" />测试连通
          </button>
          {tab === 'embedding' && (
            <button onClick={probeDimFn} disabled={busy} className="h-8 px-3 rounded-lg border border-ink-0/10 text-[11px] ui-btn-lift disabled:opacity-50">
              <Ruler className="w-3.5 h-3.5" />探测维度
            </button>
          )}
          {busy && <Loader2 className="w-3.5 h-3.5 text-ink-4 animate-spin" />}
        </div>

        {testResult && (
          <div className={`px-3 py-2.5 rounded-lg border flex items-start gap-2 text-[11px] ${
            testResult.ok ? 'bg-emerald-50 border-emerald-200 text-emerald-700' : 'bg-red-50 border-red-200 text-red-700'
          }`}>
            {testResult.ok ? <CheckCircle className="w-4 h-4 mt-0.5" /> : <XCircle className="w-4 h-4 mt-0.5" />}
            <div>{testResult.msg}</div>
          </div>
        )}
        {probeDim != null && (
          <div className="border border-ink-0/10 bg-paper-1 rounded-lg px-3 py-2.5 flex items-start gap-2">
            <Ruler className="w-4 h-4 mt-0.5 text-sienna-700" />
            <div>
              <div className="text-[11px] text-ink-2">Embedding 维度 = <span className="text-sienna-700 font-medium">{probeDim}</span>（已写入表单）</div>
              <p className="text-[12px] text-ink-3 mt-0.5">已与 pgvector projection 对齐，索引可直接写入。</p>
            </div>
          </div>
        )}

        <div className="flex items-center gap-2 pt-3 border-t border-ink-0/8">
          <button onClick={save} disabled={busy} className="h-8 px-4 rounded-lg bg-ink-0 text-paper-0 text-[11px] flex items-center gap-1.5 ui-btn-lift disabled:opacity-50">
            {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}{isEdit ? '保存' : '创建'}
          </button>
          {isEdit && (
            <>
              <button onClick={setDefault} disabled={busy} className="h-8 px-3 rounded-lg border border-ink-0/10 text-[11px] flex items-center gap-1.5 ui-btn-lift disabled:opacity-50">
                <Star className="w-3.5 h-3.5" />设为默认
              </button>
              <button onClick={del} className="ml-auto text-[11px] text-red-600 hover:underline flex items-center gap-1">
                <Trash2 className="w-3.5 h-3.5" />删除
              </button>
            </>
          )}
        </div>
        {err && <div className="text-[12px] text-red-600">{err}</div>}
      </div>
    </div>
  )
}

function FormField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="block text-[10px] uppercase tracking-wider text-ink-4 mb-1.5">{label}</label>
      {children}
    </div>
  )
}
