'use client'

import { useState, useEffect, useCallback } from 'react'
import { Plus, Info, CheckCircle, XCircle, PlugZap, Ruler, List, Eye, Star, Save, Trash2, Loader2 } from 'lucide-react'
import Header from '@/components/Header'
import { api, ApiError } from '@/lib/api'
import { useRole } from '@/lib/useRole'
import type { AIProfile, AIProfileRequest, ProfilePurpose } from '@/lib/types'

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
    <div className="flex flex-col h-screen">
      <Header active="settings" />
      <main className="flex-1 overflow-y-auto scroll-thin">
        <div className="max-w-[920px] mx-auto px-8 py-8">
          {/* 章首 */}
          <div className="pb-6 border-b border-ink-0/15">
            <div className="font-sans text-[10px] text-ink-4">设置 · BYOK</div>
            <h1 className="font-sans text-[36px] leading-[1.05] font-medium tight text-ink-0 mt-1.5">AI 服务 Profile<span className="text-sienna-500">.</span></h1>
            <p className="font-sans italic text-[14px] text-ink-3 mt-1.5">自带密钥。三类配置各设一个默认，任务消费时按所属用户解析。</p>
          </div>

          {/* 三 tab */}
          <div className="flex items-center border-b border-ink-0/15 mt-6 font-mono text-[11px]">
            {(Object.keys(TAB_META) as Tab[]).map(t => (
              <button key={t} onClick={() => setTab(t)} className={`tab py-2 mr-7 ${tab === t ? 'on' : ''}`}>{TAB_META[t].label}</button>
            ))}
            {!readOnly && <button onClick={() => { setShowNewForm(true); setSelectedId(null) }} className="btn-line h-7 px-2.5 ml-auto font-sans text-[10px] flex items-center gap-1"><Plus className="w-3 h-3" />新建 Profile</button>}
          </div>

          {err && <div className="mt-4 text-[12px] text-rust">{err}<button onClick={load} className="ml-2 underline">重试</button></div>}

          {/* 左列表 + 右编辑表单 */}
          <div className="grid grid-cols-[1fr_1.4fr] gap-10 mt-7">
            {/* 左：列表 */}
            <section>
              <div className="font-mono text-[10px] text-ink-4 mb-2.5">{TAB_META[tab].label} Profile <span className="text-ink-3">{profiles.length}</span></div>
              {loading ? (
                <div className="space-y-2">{[0,1,2].map(i => <div key={i} className="border border-ink-0/15 px-4 py-3"><div className="sk h-4 w-2/3 mb-2" /><div className="sk h-3 w-1/2" /></div>)}</div>
              ) : profiles.length === 0 ? (
                <div className="py-10 text-center border border-dashed border-ink-0/20">
                  <div className="font-mono text-[10px] text-ink-4 wide uppercase mb-2">— 暂无 Profile —</div>
                  {!readOnly && (
                    <button onClick={() => { setShowNewForm(true); setSelectedId(null) }} className="btn-line h-7 px-3 text-[10px] font-medium inline-flex items-center gap-1"><Plus className="w-3 h-3" />新建</button>
                  )}
                </div>
              ) : (
                <ul className="space-y-2">
                  {profiles.map(p => {
                    const sel = selectedId === p.id
                    const hasGroup = Boolean(groupField(p, tab).provider)
                    return (
                      <li key={p.id} onClick={() => { setSelectedId(p.id); setShowNewForm(false) }}
                        className={`border px-4 py-3 cursor-pointer ${sel ? 'border-sienna-500/40 bg-sienna-500/5' : 'border-ink-0/15 hover:bg-ink-0/[.03]'}`}>
                        <div className="flex items-start gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <span className={`font-sans text-[15px] font-medium truncate ${sel ? 'text-ink-0' : 'text-ink-1'}`}>{p.name}</span>
                              {p.is_default && <span className="default-badge font-sans text-[9px] px-1.5 py-0.5">默认</span>}
                              {!hasGroup && <span className="font-mono text-[9px] text-ink-4 border border-ink-0/20 px-1">未配{tab.toUpperCase()}</span>}
                            </div>
                            <div className="font-mono text-[11px] text-ink-3 mt-0.5 truncate">{readOnly ? (groupField(p, tab).model || '—') : `${groupField(p, tab).provider || '—'} · ${groupField(p, tab).base || '—'}`}</div>
                          </div>
                          {!readOnly && !sel && !p.is_default && <button onClick={(e) => { e.stopPropagation(); setDefault(p.id) }} className="font-sans text-[10px] text-ink-4 hover:text-ink-0">设默认</button>}
                        </div>
                      </li>
                    )
                  })}
                </ul>
              )}
              <div className="mt-5 font-mono text-[10px] text-ink-4 leading-relaxed flex items-start gap-1.5">
                <Info className="w-3 h-3 mt-0.5 shrink-0" />
                <span>Create 时若该类无默认，将自动设为默认。设新默认会取消旧默认。</span>
              </div>
            </section>

            {/* 右：编辑表单 */}
            <section>
              {readOnly ? (
                selected ? (
                  <ReadOnlyProfile profile={selected} />
                ) : (
                  <div className="font-sans text-[10px] text-ink-4 mb-2.5">查看 · 未选中</div>
                )
              ) : showNewForm ? (
                <ProfileForm key="new" tab={tab} onChanged={load} onSaved={(id) => { setSelectedId(id); setShowNewForm(false) }} />
              ) : selected ? (
                <ProfileForm key={selected.id} tab={tab} profile={selected} onChanged={load} />
              ) : (
                <div className="font-sans text-[10px] text-ink-4 mb-2.5">编辑 · 未选中</div>
              )}
              {!selected && !showNewForm && !readOnly && (
                <div className="border border-dashed border-ink-0/20 py-12 text-center font-sans text-[12px] text-ink-4">从左侧选择一个 Profile，或新建</div>
              )}
            </section>
          </div>
        </div>
      </main>
    </div>
  )

  async function setDefault(id: number) {
    // 设为默认：后端通过 PUT 带 is_default:true，自动把其它置 false
    const p = profiles.find(x => x.id === id)
    if (!p) return
    try {
      await api.updateProfile(id, { is_default: true })
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
    <div className="flex flex-col h-screen">
      <Header active="settings" />
      <main className="flex-1 overflow-y-auto scroll-thin">
        <div className="max-w-[920px] mx-auto px-8 py-8">
          <div className="pb-6 border-b border-ink-0/15">
            <div className="font-sans text-[10px] text-ink-4">设置 · 演示账号</div>
            <h1 className="font-sans text-[36px] leading-[1.05] font-medium tight text-ink-0 mt-1.5">AI 模型<span className="text-sienna-500">.</span></h1>
            <p className="font-sans italic text-[14px] text-ink-3 mt-1.5">演示账号仅展示当前使用的模型，配置只读，服务地址与密钥不对外显示。</p>
          </div>

          {err && <div className="mt-4 text-[12px] text-rust">{err}<button onClick={load} className="ml-2 underline">重试</button></div>}

          {loading ? (
            <div className="mt-7 space-y-3">
              {[0, 1, 2].map(i => <div key={i} className="border border-ink-0/15 px-5 py-4"><div className="sk h-4 w-1/3 mb-2" /><div className="sk h-3 w-1/2" /></div>)}
            </div>
          ) : profiles.length === 0 ? (
            <div className="py-16 text-center">
              <div className="font-mono text-[10px] text-ink-4 wide uppercase mb-2">— 暂无模型配置 —</div>
            </div>
          ) : (
            <div className="mt-7 space-y-4">
              {profiles.map(p => (
                <div key={p.id} className="border border-ink-0/15 bg-paper-0 p-6">
                  <div className="flex items-center gap-2 mb-4">
                    <span className="font-sans text-[18px] font-medium text-ink-0">{p.name}</span>
                    {p.is_default && <span className="default-badge font-sans text-[9px] px-1.5 py-0.5">默认</span>}
                  </div>
                  <div className="space-y-3">
                    {rows(p).map(r => (
                      <div key={r.label} className="flex items-center justify-between gap-4 border-b border-ink-0/10 pb-2.5 last:border-0 last:pb-0">
                        <span className="font-mono text-[10px] text-ink-4 wide uppercase">{r.label}</span>
                        <span className="font-mono text-[12.5px] text-ink-1">{r.value || '—'}</span>
                      </div>
                    ))}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </main>
    </div>
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
    <div className="space-y-4">
      <div className="font-sans text-[10px] text-ink-4 mb-0.5">查看 · {profile.name}</div>
      <div className="border border-ink-0/15 bg-paper-0 p-5 space-y-3">
        <div className="flex items-center gap-2">
          <span className="font-sans text-[18px] font-medium text-ink-0">{profile.name}</span>
          {profile.is_default && <span className="default-badge font-sans text-[9px] px-1.5 py-0.5">默认</span>}
        </div>
        {rows.map(r => (
          <div key={r.label} className="flex items-center justify-between gap-4 border-b border-ink-0/10 pb-2.5 last:border-0 last:pb-0">
            <span className="font-mono text-[10px] text-ink-4 wide uppercase">{r.label}</span>
            <span className="font-mono text-[12.5px] text-ink-1">{r.value || '—'}</span>
          </div>
        ))}
        <p className="font-sans text-[11px] text-ink-4 pt-1">演示账号配置为只读，服务地址与密钥已隐藏。</p>
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
  const [provider, setProvider] = useState(g.provider)
  const [baseUrl, setBaseUrl] = useState(g.base)
  const [apiKey, setApiKey] = useState('') // 编辑时不回显明文，留空保留旧值
  const [model, setModel] = useState(g.model)
  const [showKey, setShowKey] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [testResult, setTestResult] = useState<{ ok: boolean; msg: string } | null>(null)
  const [models, setModels] = useState<string[]>([])
  const [probeDim, setProbeDim] = useState<number | null>(null)

  // tab 切换时同步字段
  useEffect(() => {
    if (profile) {
      const ng = groupField(profile, tab)
      setProvider(ng.provider); setBaseUrl(ng.base); setModel(ng.model); setApiKey('')
      setTestResult(null); setModels([]); setProbeDim(null)
    }
  }, [tab, profile])

  const fieldLabel = { provider: 'Provider', base: tab === 'embedding' ? 'Endpoint' : 'Base URL', key: 'API Key', model: 'Model' } as const

  // 收集当前 tab 这一组的请求体片段（创建时需要完整四组，编辑时只 PATCH 当前组）
  const currentGroupBody = () => {
    if (tab === 'asr') return { asr_provider: provider, asr_base_url: baseUrl, asr_model: model, ...(apiKey ? { asr_api_key: apiKey } : {}) }
    if (tab === 'llm') return { llm_provider: provider, llm_base_url: baseUrl, llm_model: model, ...(apiKey ? { llm_api_key: apiKey } : {}) }
    return { embedding_provider: provider, embedding_endpoint: baseUrl, embedding_model: model, ...(apiKey ? { embedding_api_key: apiKey } : {}) }
  }

  const save = async () => {
    if (!name.trim()) { setErr('请输入 Profile 名称'); return }
    if (!provider.trim()) { setErr('请输入 Provider'); return }
    if (!baseUrl.trim()) { setErr('请输入 Base URL / Endpoint'); return }
    if (!model.trim()) { setErr('请输入 Model'); return }
    setBusy(true); setErr('')
    try {
      if (isEdit && profile) {
        await api.updateProfile(profile.id, currentGroupBody())
        onChanged()
        setApiKey('')
        setTestResult({ ok: true, msg: '已保存' })
      } else {
        // 新建：当前组 + 名称 + is_default（后端：该类无默认时自动设默认）
        const body = { name: name.trim(), ...currentGroupBody(), is_default: false }
        const created = await api.createProfile(body as AIProfileRequest)
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
    if (!baseUrl || !apiKey) { setErr('探测可用模型需先填 Base URL + API Key'); return }
    setBusy(true); setErr('')
    try {
      const r = await api.listModels(baseUrl, apiKey, profile?.id || 0, TAB_META[tab].purpose)
      setModels(r.models || [])
    } catch (e) { setErr(e instanceof ApiError ? e.message : '探测失败') } finally { setBusy(false) }
  }

  const probeDimFn = async () => {
    if (tab !== 'embedding') { setErr('仅 Embedding tab 可探测维度'); return }
    if (!baseUrl || !apiKey || !model) { setErr('探测维度需先填 Endpoint + API Key + Model'); return }
    setBusy(true); setErr('')
    try {
      const r = await api.probeEmbeddingDim(baseUrl, apiKey, model, profile?.id || 0)
      setProbeDim(r.dimension)
    } catch (e) { setErr(e instanceof ApiError ? e.message : '探测失败') } finally { setBusy(false) }
  }

  const setDefault = async () => {
    if (!profile) return
    try { await api.updateProfile(profile.id, { is_default: true }); onChanged() } catch (e) { setErr(e instanceof ApiError ? e.message : '设默认失败') }
  }
  const del = async () => {
    if (!profile) return
    if (!confirm(`删除 Profile「${profile.name}」？`)) return
    try { await api.deleteProfile(profile.id); onChanged() } catch (e) { setErr(e instanceof ApiError ? e.message : '删除失败') }
  }

  return (
    <div className="space-y-4">
      <div className="font-sans text-[10px] text-ink-4 mb-0.5">{isEdit ? `编辑 · ${profile?.name}` : '新建 · ' + TAB_META[tab].label}</div>
      <div className="border border-ink-0/15 bg-paper-0 p-5 space-y-4">
        {/* 名称 */}
        <div>
          <label className="block font-mono text-[10px] text-ink-3 mb-1.5">Profile 名称</label>
          <div className="field px-3 h-9 flex items-center">
            <input value={name} onChange={(e) => setName(e.target.value)} disabled={isEdit} className="w-full font-mono text-[12.5px] text-ink-1 disabled:text-ink-4" />
          </div>
          {isEdit && <p className="font-sans text-[10px] text-ink-4 mt-1">名称创建后不可改；当前编辑 {TAB_META[tab].label} 这一组的配置。</p>}
        </div>
        {/* Provider */}
        <div>
          <label className="block font-mono text-[10px] text-ink-3 mb-1.5">{fieldLabel.provider}</label>
          <div className="field px-3 h-9 flex items-center">
            <input value={provider} onChange={(e) => setProvider(e.target.value)} placeholder="mimo / openai / siliconflow" className="w-full font-mono text-[12.5px] text-ink-1" />
          </div>
        </div>
        {/* Base URL */}
        <div>
          <label className="block font-mono text-[10px] text-ink-3 mb-1.5">{fieldLabel.base}</label>
          <div className="field px-3 h-9 flex items-center">
            <input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.example.com/v1" className="w-full font-mono text-[12.5px] text-ink-1" />
          </div>
        </div>
        {/* API Key */}
        <div>
          <label className="block font-mono text-[10px] text-ink-3 mb-1.5">{fieldLabel.key}</label>
          <div className="field px-3 h-9 flex items-center gap-2">
            <input type={showKey ? 'text' : 'password'} value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={isEdit ? '•••• 留空保留旧值' : 'sk-...'} className="w-full font-mono text-[12.5px] text-ink-1" />
            <button onClick={() => setShowKey(s => !s)} className="text-ink-4 hover:text-ink-0"><Eye className="w-3.5 h-3.5" /></button>
          </div>
          <p className="font-sans text-[10px] text-ink-4 mt-1">加密存储，使用 VIDLENS_API_KEY_SECRET。{isEdit && g.keyMasked ? `当前：${g.keyMasked}` : ''}</p>
        </div>
        {/* Model */}
        <div>
          <label className="block font-mono text-[10px] text-ink-3 mb-1.5">{fieldLabel.model}</label>
          <div className="field px-3 h-9 flex items-center gap-2">
            <input value={model} onChange={(e) => setModel(e.target.value)} placeholder="模型名" className="w-full font-mono text-[12.5px] text-ink-1" />
            <button onClick={probeModels} disabled={busy} className="font-sans text-[10px] text-ink-4 hover:text-ink-0 whitespace-nowrap flex items-center gap-1 disabled:opacity-50"><List className="w-3 h-3" />探测</button>
          </div>
          {/* 探测结果 */}
          {models.length > 0 && (
            <div className="mt-2 border border-ink-0/10 p-2.5 font-mono text-[11px] text-ink-2 space-y-0.5">
              <div className="text-ink-4 text-[10px] mb-1">可用模型</div>
              {models.map(m => (
                <div key={m} className="cursor-pointer hover:text-sienna-700" onClick={() => setModel(m)}>
                  {m}{m === model && <span className="text-sienna-700"> ← 当前</span>}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* 测试连通 + 探测维度 */}
        <div className="flex items-center gap-2 pt-1">
          <button onClick={testConn} disabled={busy || !isEdit} className="btn-ink h-8 px-3.5 font-sans text-[11px] flex items-center gap-1.5 disabled:opacity-50"><PlugZap className="w-3.5 h-3.5" />测试连通</button>
          {tab === 'embedding' && (
            <button onClick={probeDimFn} disabled={busy} className="btn-line h-8 px-3 font-sans text-[11px] flex items-center gap-1.5 disabled:opacity-50"><Ruler className="w-3.5 h-3.5" />探测维度</button>
          )}
          {busy && <Loader2 className="w-3.5 h-3.5 text-ink-3 animate-spin" />}
        </div>

        {/* 测试结果 */}
        {testResult && (
          <div className={`${testResult.ok ? 'result-ok border-moss/30' : 'result-err border-rust/30'} border px-3 py-2.5 flex items-start gap-2`}>
            {testResult.ok ? <CheckCircle className="w-4 h-4 mt-0.5" /> : <XCircle className="w-4 h-4 mt-0.5" />}
            <div className="flex-1"><div className="font-sans text-[11px]">{testResult.msg}</div></div>
          </div>
        )}
        {/* 探测维度结果 */}
        {probeDim != null && (
          <div className="border border-ink-0/15 bg-paper-1 px-3 py-2.5 flex items-start gap-2">
            <Ruler className="w-4 h-4 mt-0.5 text-sienna-700" />
            <div className="flex-1">
              <div className="font-sans text-[11px] text-ink-3">Embedding 维度 = <span className="text-sienna-700">{probeDim}</span></div>
              <p className="font-sans text-[12.5px] text-ink-2 mt-0.5">已与 pgvector projection 对齐，索引可直接写入。</p>
            </div>
          </div>
        )}

        {/* 保存 */}
        <div className="flex items-center gap-2 pt-3 border-t border-ink-0/10">
          <button onClick={save} disabled={busy} className="btn-ink h-8 px-4 font-sans text-[11px] flex items-center gap-1.5 disabled:opacity-50">
            {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Save className="w-3.5 h-3.5" />}{isEdit ? '保存' : '创建'}
          </button>
          {isEdit && (
            <>
              <button onClick={setDefault} disabled={busy} className="btn-line h-8 px-3 font-sans text-[11px] flex items-center gap-1.5 disabled:opacity-50"><Star className="w-3.5 h-3.5" />设为默认</button>
              <button onClick={del} className="ml-auto font-sans text-[11px] text-rust hover:underline flex items-center gap-1"><Trash2 className="w-3.5 h-3.5" />删除</button>
            </>
          )}
        </div>
        {err && <div className="text-[12px] text-rust">{err}</div>}
      </div>
    </div>
  )
}
