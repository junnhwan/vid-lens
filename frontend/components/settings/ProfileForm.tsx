'use client'

import { useEffect, useMemo, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { AIProfile, AIProfileRequest, ProfilePurpose, AgentBudgetOverride, AgentBudgetOptions } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'
import { ModelCombobox } from '@/components/settings/ModelCombobox'
import { PROVIDER_PRESETS, matchPreset } from '@/lib/providerPresets'
import { useShell } from '@/components/shell/AppShell'
import { CapabilityProbe, type ProbeTarget } from '@/components/settings/CapabilityProbe'

interface GroupDraft {
  provider: string
  base_url: string
  api_key: string
  model: string
  preset: string
}

const EMPTY_GROUP: GroupDraft = { provider: 'openai', base_url: '', api_key: '', model: '', preset: 'openai-compat' }

function fromProfile(provider: string, baseUrl: string, model: string): GroupDraft {
  return {
    provider: provider || 'openai',
    base_url: baseUrl || '',
    api_key: '',
    model: model || '',
    preset: matchPreset(provider, baseUrl),
  }
}

export function ProfileForm({ profile, imported, onClose, onSaved }: {
  profile?: AIProfile
  imported?: AIProfileRequest
  onClose: () => void
  onSaved: () => void
}) {
  const toast = useToast()
  const { registerLeaveGuard } = useShell()
  const editing = !!profile
  const [name, setName] = useState(imported?.name || profile?.name || '')
  const [llm, setLlm] = useState<GroupDraft>(fromProfile(imported?.llm_provider || profile?.llm_provider || '', imported?.llm_base_url || profile?.llm_base_url || '', imported?.llm_model || profile?.llm_model || ''))
  const [llmContextTokens, setLlmContextTokens] = useState(imported?.llm_context_tokens ? String(imported.llm_context_tokens) : profile?.llm_context_tokens ? String(profile.llm_context_tokens) : '')
  const [asr, setAsr] = useState<GroupDraft>(fromProfile(imported?.asr_provider || profile?.asr_provider || '', imported?.asr_base_url || profile?.asr_base_url || '', imported?.asr_model || profile?.asr_model || ''))
  const [embedding, setEmbedding] = useState<GroupDraft>(fromProfile(imported?.embedding_provider || profile?.embedding_provider || '', imported?.embedding_endpoint || profile?.embedding_endpoint || '', imported?.embedding_model || profile?.embedding_model || ''))
  const [embeddingDim, setEmbeddingDim] = useState<string>(imported?.embedding_dim ? String(imported.embedding_dim) : profile?.embedding_dim ? String(profile.embedding_dim) : '')
  const [vision, setVision] = useState<GroupDraft>(fromProfile(imported?.vision_provider || profile?.vision_provider || '', imported?.vision_base_url || profile?.vision_base_url || '', imported?.vision_model || profile?.vision_model || ''))
  const [visionEnabled, setVisionEnabled] = useState(!!(imported?.vision_model || profile?.vision_model))
  const [isDefault, setIsDefault] = useState(imported?.is_default || profile?.is_default || false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [models, setModels] = useState<Partial<Record<ProfilePurpose, string[]>>>({})
  const [listStatus, setListStatus] = useState<Partial<Record<ProfilePurpose, string>>>({})
  useEffect(() => { setListStatus({}) }, [llm, asr, embedding, vision])
  const [probing, setProbing] = useState(false)
  const [budgetOptions, setBudgetOptions] = useState<AgentBudgetOptions | null>(null)
  const [budgetError, setBudgetError] = useState('')
  const [customBudget, setCustomBudget] = useState(!!(imported?.agent_budget || profile?.agent_budget))
  const [budgetDraft, setBudgetDraft] = useState<Partial<Record<keyof AgentBudgetOverride, string>>>(() => Object.fromEntries(Object.entries(imported?.agent_budget || profile?.agent_budget || {}).map(([k, v]) => [k, String(v)])))
  useEffect(() => {
    let live = true
    api.budgetOptions().then(options => { if (live) setBudgetOptions(options) }).catch(() => { if (live) setBudgetError('预算选项加载失败，请重新打开表单重试') })
    return () => { live = false }
  }, [])
  const budgetFields = [
    ['max_tool_calls', '最多工具调用次数'], ['max_duration_seconds', '最长运行时间（秒）'],
    ['max_input_tokens', '累计输入 Token'], ['max_output_tokens', '累计输出 Token'], ['max_visual_frames', '最多检查帧数'],
  ] as const

  const currentSnapshot = useMemo(() => JSON.stringify({ name, llm, llmContextTokens, asr, embedding, embeddingDim, vision, visionEnabled, isDefault, customBudget, budgetDraft }), [name, llm, llmContextTokens, asr, embedding, embeddingDim, vision, visionEnabled, isDefault, customBudget, budgetDraft])
  const initialSnapshot = useRef(currentSnapshot)
  const saved = useRef(false)
  const dirty = !!imported || currentSnapshot !== initialSnapshot.current

  useEffect(() => {
    if (!dirty || saved.current) { registerLeaveGuard(null); return }
    registerLeaveGuard(() => window.confirm('当前 AI 配置有未保存修改。放弃修改并离开吗？'))
    return () => registerLeaveGuard(null)
  }, [dirty, registerLeaveGuard])

  const back = () => {
    if (dirty && !window.confirm('当前 AI 配置有未保存修改。放弃修改并返回吗？')) return
    registerLeaveGuard(null)
    onClose()
  }

  const profileId = profile?.id ?? 0
  const setGroup = (setter: (fn: (g: GroupDraft) => GroupDraft) => void) =>
    (patch: Partial<GroupDraft>) => setter(g => ({ ...g, ...patch }))

  const buildRequest = (): AIProfileRequest | null => {
    if (!name.trim()) { setErr('请填写配置名称'); return null }
    if (!llm.provider.trim() || !llm.base_url.trim() || !llm.model.trim()) { setErr('LLM 配置不完整'); return null }
    const contextTokens = llmContextTokens.trim() === '' ? 0 : Number(llmContextTokens)
    if (!Number.isSafeInteger(contextTokens) || (contextTokens !== 0 && (contextTokens < 8192 || contextTokens > 1048576))) { setErr('模型上下文窗口需为 8192–1048576 token，或留空'); return null }
    if (!asr.provider.trim() || !asr.base_url.trim() || !asr.model.trim()) { setErr('ASR 配置不完整'); return null }
    if (!embedding.provider.trim() || !embedding.base_url.trim() || !embedding.model.trim()) { setErr('embedding 配置不完整'); return null }
    for (const [label, url, endpoint, preset] of [['对话', llm.base_url, false, llm.preset], ['语音识别', asr.base_url, false, asr.preset], ['向量', embedding.base_url, true, embedding.preset], ...(visionEnabled ? [['视觉', vision.base_url, false, vision.preset]] : [])] as [string, string, boolean, string][]) {
      const problem = validateModelURL(url, endpoint, preset)
      if (problem) { setErr(`${label}地址：${problem}`); return null }
    }
    const dim = Number(embeddingDim)
    if (!Number.isFinite(dim) || dim <= 0) { setErr('embedding 维度需为正数:先探测,或手动填写'); return null }
    let agentBudget: AgentBudgetOverride | null = null
    if (customBudget) {
      if (!budgetOptions) { setErr(budgetError || '正在加载预算选项'); return null }
      agentBudget = { ...budgetOptions.defaults }
      for (const [key, label] of budgetFields) {
        const value = Number(budgetDraft[key] ?? budgetOptions.defaults[key])
        const range = budgetOptions.limits[key]
        if (!Number.isSafeInteger(value) || value < range.min || value > range.max) { setErr(`${label}须为 ${range.min}–${range.max} 之间的整数`); return null }
        agentBudget[key] = value
      }
    }
    return {
      agent_budget: agentBudget,
      name: name.trim(),
      llm_provider: llm.provider.trim(), llm_base_url: llm.base_url.trim(), llm_model: llm.model.trim(), llm_context_tokens: contextTokens,
      ...(llm.api_key.trim() ? { llm_api_key: llm.api_key.trim() } : {}),
      asr_provider: asr.provider.trim(), asr_base_url: asr.base_url.trim(), asr_model: asr.model.trim(),
      ...(asr.api_key.trim() ? { asr_api_key: asr.api_key.trim() } : {}),
      embedding_provider: embedding.provider.trim(), embedding_endpoint: embedding.base_url.trim(), embedding_model: embedding.model.trim(),
      ...(embedding.api_key.trim() ? { embedding_api_key: embedding.api_key.trim() } : {}),
      embedding_dim: Math.round(dim),
      ...(visionEnabled ? {
        vision_provider: vision.provider.trim(), vision_base_url: vision.base_url.trim(), vision_model: vision.model.trim(),
        ...(vision.api_key.trim() ? { vision_api_key: vision.api_key.trim() } : {}),
      } : {}),
      is_default: isDefault,
    }
  }

  const save = async () => {
    if (busy) return
    const req = buildRequest()
    if (!req) return
    setBusy(true)
    setErr('')
    try {
      if (editing && profile) await api.updateProfile(profile.id, req)
      else await api.createProfile(req)
      saved.current = true
      registerLeaveGuard(null)
      toast.success(editing ? '配置已更新' : '配置已创建')
      onSaved()
      onClose()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const pullModels = async (purpose: ProfilePurpose, group: GroupDraft) => {
    if (!group.base_url.trim() || (!group.api_key.trim() && !profileId)) {
      setListStatus(previous => ({ ...previous, [purpose]: '请先填写地址和 API Key；编辑已保存配置可留空密钥' }))
      return
    }
    setListStatus(previous => ({ ...previous, [purpose]: '正在读取模型列表…' }))
    try {
      const res = await api.listModels(group.base_url.trim(), group.api_key.trim(), profileId, purpose)
      setModels(prev => ({ ...prev, [purpose]: res.models || [] }))
      setListStatus(previous => ({ ...previous, [purpose]: `列表接口返回 ${res.models?.length ?? 0} 个模型；尚未验证所选模型能否调用` }))
    } catch (e) {
      setListStatus(previous => ({ ...previous, [purpose]: e instanceof ApiError ? e.message : '拉取模型列表失败' }))
    }
  }

  const probeTargets: ProbeTarget[] = [
    { purpose: 'llm', label: '对话', model: llm.model, base_url: llm.base_url, api_key: llm.api_key, provider: llm.provider, profile_id: profileId },
    { purpose: 'asr', label: '语音识别', model: asr.model, base_url: asr.base_url, api_key: asr.api_key, provider: asr.provider, profile_id: profileId },
    { purpose: 'embedding', label: '向量', model: embedding.model, base_url: embedding.base_url, api_key: embedding.api_key, provider: embedding.provider, profile_id: profileId, embedding_dim: Number(embeddingDim) || undefined },
    ...(visionEnabled ? [{ purpose: 'vision' as const, label: '视觉', model: vision.model, base_url: vision.base_url, api_key: vision.api_key, provider: vision.provider, profile_id: profileId }] : []),
  ]

  const probeDim = async () => {
    if (probing) return
    if (!embedding.base_url.trim() || !embedding.model.trim() || (!embedding.api_key.trim() && !profileId)) {
      toast.info('先填 endpoint、模型与 API Key(编辑时留空 Key 用已存密钥)')
      return
    }
    setProbing(true)
    try {
      const res = await api.probeEmbeddingDim(embedding.base_url.trim(), embedding.api_key.trim(), embedding.model.trim(), profileId)
      setEmbeddingDim(String(res.dimension))
      toast.success(`向量维度:${res.dimension}`)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '维度探测失败')
    } finally {
      setProbing(false)
    }
  }

  return (
    <div className="profile-form">
      <div className="section-head" style={{ marginTop: 0 }}>
        <button className="btn btn-sm btn-ghost" onClick={back}><Icon name="chev-l" size="sm" />返回</button>
        <h2>{editing ? `编辑 · ${profile?.name}` : '新建 AI 配置'}</h2>
      </div>

      <label className="field-label">配置名称</label>
      <input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="例如:硅基流动" />
      <details style={{ marginTop: 12, fontSize: 13 }}>
        <summary>服务地址填写示例与拼接规则</summary>
        <p>本项目使用 OpenAI 兼容协议。对话、语音识别、视觉填 API 基础地址；例如硅基流动 <code>https://api.siliconflow.cn/v1</code>、OpenAI <code>https://api.openai.com/v1</code>、DeepSeek 对话 <code>https://api.deepseek.com/v1</code>。系统分别追加 <code>/chat/completions</code>、<code>/audio/transcriptions</code>、<code>/chat/completions</code>。</p>
        <p>向量模型填完整接口，例如 <code>https://api.siliconflow.cn/v1/embeddings</code>，系统不会再追加路径。各服务商支持的能力与模型不同，先核对其文档。硅基流动和 OpenAI 只填主域名会漏掉 <code>/v1</code>；DeepSeek 对话可按其接口使用主域名或 <code>/v1</code>。将完整接口填进基础地址会重复路径。</p>
      </details>

      <GroupBlock
        title="对话模型"
        group={llm} setGroup={setGroup(setLlm)}
        purpose="llm" models={models.llm || []} onPull={pullModels} listStatus={listStatus.llm}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.llm_api_key_masked})` : 'sk-…'}
        required
      />
      <label className="field-label" htmlFor="llm-context-tokens">模型上下文窗口（token，可选）</label>
      <input id="llm-context-tokens" className="input mono" type="number" min={8192} max={1048576} step={1} value={llmContextTokens} onChange={e => setLlmContextTokens(e.target.value)} placeholder="留空按 8192 计算" />
      <small style={{ color: 'var(--tx-3)' }}>按模型服务实际允许的上下文填写。摘要能放入时使用一次请求；超出时自动分段。请预留输出空间，填大于实际上限可能导致请求失败。</small>
      <GroupBlock
        title="语音识别"
        group={asr} setGroup={setGroup(setAsr)}
        purpose="asr" models={models.asr || []} onPull={pullModels} listStatus={listStatus.asr}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.asr_api_key_masked})` : 'sk-…'}
        required
      />
      <GroupBlock
        title="向量模型"
        group={embedding} setGroup={setGroup(setEmbedding)}
        purpose="embedding" models={models.embedding || []} onPull={pullModels} listStatus={listStatus.embedding}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.embedding_api_key_masked})` : 'sk-…'}
        urlPlaceholder="https://…/v1/embeddings"
        required
      />
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 10 }}>
        <input className="input mono" style={{ width: 140 }} placeholder="维度" value={embeddingDim} onChange={e => setEmbeddingDim(e.target.value)} />
        <button type="button" className="btn btn-sm" disabled={probing} onClick={() => void probeDim()}>
          <Icon name="scan" size="sm" />{probing ? '探测中…' : '探测维度'}
        </button>
      </div>

      <div className="pref-row" style={{ marginTop: 22 }}>
        <div className="pr-body">
          <b>视觉模型</b>
          <span>可选。关键帧 OCR 与画面描述会用到它</span>
        </div>
        <button
          type="button"
          className={`switch${visionEnabled ? ' on' : ''}`}
          onClick={() => setVisionEnabled(v => !v)}
          aria-label="启用视觉模型"
        />
      </div>
      {visionEnabled && (
        <GroupBlock
          title=""
          group={vision} setGroup={setGroup(setVision)}
          purpose="vision" models={models.vision || []} onPull={pullModels} listStatus={listStatus.vision}
          keyPlaceholder={editing ? `留空保留现有密钥(${profile?.vision_api_key_masked})` : 'sk-…'}
        />
      )}

      <CapabilityProbe targets={probeTargets} disabled={busy} />

      {!profile?.read_only && profile?.source !== 'hosted' && (
        <details style={{ marginTop: 22 }}>
          <summary className="field-label" style={{ cursor: 'pointer' }}>Agent 执行预算</summary>
          <p style={{ color: 'var(--tx-3)', fontSize: 13 }}>仅用于此配置下新开始的 Agent 运行，不影响普通 Chat；Token 用量可能为估算，不代表模型思考强度。</p>
          {profile?.agent_budget_error && <p role="alert">{profile.agent_budget_error}；可恢复默认或重新填写预算修复。</p>}
          {budgetError && <p role="alert">{budgetError}</p>}
          <div style={{ display: 'flex', gap: 18, margin: '12px 0' }}>
            <label><input type="radio" name="budget-mode" checked={!customBudget} onChange={() => setCustomBudget(false)} /> 跟随服务端默认值</label>
            <label><input type="radio" name="budget-mode" checked={customBudget} onChange={() => setCustomBudget(true)} disabled={!budgetOptions} /> 自定义</label>
          </div>
          {budgetOptions && budgetFields.map(([key, label]) => {
            if (key === 'max_visual_frames' && !budgetOptions.visual_available) return null
            const range = budgetOptions.limits[key]
            return <label key={key} style={{ display: 'block', marginTop: 10 }}>
              <span className="field-label">{label} {key === 'max_visual_frames' && !visionEnabled ? '（未启用视觉模型，设置保留但当前不生效）' : ''}</span>
              <input className="input mono" type="number" step={1} min={range.min} max={range.max} disabled={!customBudget} value={customBudget ? budgetDraft[key] ?? budgetOptions.defaults[key] : budgetOptions.defaults[key]} onChange={e => setBudgetDraft(draft => ({ ...draft, [key]: e.target.value }))} />
              <small style={{ color: 'var(--tx-3)' }}>允许范围 {range.min}–{range.max}</small>
            </label>
          })}
          {profile?.effective_agent_budget?.adjustments?.map(note => <p key={note}>{note}</p>)}
          <button type="button" className="btn btn-sm" style={{ marginTop: 12 }} onClick={() => { setCustomBudget(false); setBudgetDraft({}) }}>恢复默认</button>
          <p style={{ fontSize: 13, color: 'var(--tx-3)' }}>保存后新运行生效，进行中的运行保持原预算。修改预算无需重新输入 API Key。</p>
        </details>
      )}

      <div className="pref-row" style={{ marginTop: 18 }}>
        <div className="pr-body">
          <b>设为默认配置</b>
        </div>
        <button
          type="button"
          className={`switch${isDefault ? ' on' : ''}`}
          onClick={() => setIsDefault(v => !v)}
          aria-label="设为默认配置"
        />
      </div>

      {err && <div className="form-err" style={{ marginTop: 14 }}>{err}</div>}

      <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 10, marginTop: 22 }}>
        <button type="button" className="btn" onClick={back} disabled={busy}>取消</button>
        <button type="button" className="btn btn-primary" onClick={() => void save()} disabled={busy}>
          {busy ? '保存中…' : editing ? '保存修改' : '创建配置'}
        </button>
      </div>
    </div>
  )
}

function applyPreset(presetId: string, group: GroupDraft): Partial<GroupDraft> {
  const preset = PROVIDER_PRESETS.find(p => p.id === presetId)
  if (!preset) return { preset: presetId }
  return {
    preset: presetId,
    provider: preset.provider,
    base_url: preset.baseUrl || group.base_url,
  }
}

function GroupBlock({ title, group, setGroup, purpose, models, onPull, listStatus, keyPlaceholder, urlPlaceholder, required }: {
  title: string
  group: GroupDraft
  setGroup: (patch: Partial<GroupDraft>) => void
  purpose: ProfilePurpose
  models: string[]
  onPull: (purpose: ProfilePurpose, group: GroupDraft) => void
  listStatus?: string
  keyPlaceholder: string
  urlPlaceholder?: string
  required?: boolean
}) {
  return (
    <div style={{ marginTop: 22 }}>
      {title && <div className="field-label">{title}{required && <span style={{ color: 'var(--acc-strong)' }}> · 必填</span>}</div>}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
        <select
          className="input"
          value={group.preset}
          onChange={e => setGroup(applyPreset(e.target.value, group))}
        >
          {PROVIDER_PRESETS.map(p => (
            <option key={p.id} value={p.id}>{p.label}</option>
          ))}
        </select>
        <input
          className="input"
          placeholder={urlPlaceholder || 'Base URL'}
          value={group.base_url}
          onChange={e => setGroup({ base_url: e.target.value })}
        />
      </div>
      <p style={{ fontSize: 12, color: 'var(--tx-3)', marginTop: 6 }}>
        {purpose === 'embedding' ? '填写完整 Embedding 接口地址，例如 https://api.siliconflow.cn/v1/embeddings；请求直接发送到此地址。' : `填写服务商要求的 API 基础地址，例如硅基流动 https://api.siliconflow.cn/v1；系统会追加 ${purpose === 'asr' ? '/audio/transcriptions' : '/chat/completions'}。不要填写完整接口路径。`}
      </p>
      {validateModelURL(group.base_url, purpose === 'embedding', group.preset) && <p role="alert" style={{ fontSize: 12, color: 'var(--bad)' }}>{validateModelURL(group.base_url, purpose === 'embedding', group.preset)}</p>}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
        <input className="input" type="password" placeholder={keyPlaceholder} value={group.api_key} onChange={e => setGroup({ api_key: e.target.value })} />
        <div style={{ display: 'flex', gap: 8 }}>
          <ModelCombobox value={group.model} onChange={model => setGroup({ model })} models={models} />
          <button type="button" className="btn btn-sm" style={{ flex: 'none' }} onClick={() => onPull(purpose, group)}>拉模型</button>
        </div>
      </div>
      {listStatus && <p aria-live="polite" style={{ fontSize: 12 }}>{listStatus}</p>}
    </div>
  )
}

function validateModelURL(value: string, embedding: boolean, preset: string): string | null {
  if (!value.trim()) return null
  let url: URL
  try { url = new URL(value) } catch { return '请输入完整的 http(s) URL' }
  if (!['http:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash) return '只允许不含账号、查询参数和片段的 http(s) 地址'
  const path = url.pathname.replace(/\/+$/, '')
  if (/(\/v1){2}(\/|$)/i.test(path)) return '路径中重复出现 /v1，请删掉多余的一段'
  if (embedding) {
    if (!path.endsWith('/embeddings')) return '向量模型需要完整接口地址，末尾应为 /embeddings'
  } else if (/\/(chat\/completions|audio\/transcriptions|embeddings|models)$/i.test(path)) {
    return '这里填写基础地址，不要包含完整接口路径'
  } else if ((!path || path === '/') && (preset === 'siliconflow' || preset === 'openai' || ['api.siliconflow.cn', 'api.openai.com'].includes(url.hostname.toLowerCase()))) {
    return '该服务商的示例地址需要包含 /v1'
  }
  return null
}
