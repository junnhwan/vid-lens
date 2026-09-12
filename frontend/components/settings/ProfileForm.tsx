'use client'

import { useEffect, useMemo, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { AIProfile, AIProfileRequest, ProfilePurpose, AgentBudgetOverride, AgentBudgetOptions } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'
import { ModelCombobox } from '@/components/settings/ModelCombobox'
import { PROVIDER_PRESETS, matchPreset } from '@/lib/providerPresets'

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

export function ProfileForm({ profile, onClose, onSaved }: {
  profile?: AIProfile
  onClose: () => void
  onSaved: () => void
}) {
  const toast = useToast()
  const editing = !!profile
  const [name, setName] = useState(profile?.name || '')
  const [llm, setLlm] = useState<GroupDraft>(fromProfile(profile?.llm_provider || '', profile?.llm_base_url || '', profile?.llm_model || ''))
  const [asr, setAsr] = useState<GroupDraft>(fromProfile(profile?.asr_provider || '', profile?.asr_base_url || '', profile?.asr_model || ''))
  const [embedding, setEmbedding] = useState<GroupDraft>(fromProfile(profile?.embedding_provider || '', profile?.embedding_endpoint || '', profile?.embedding_model || ''))
  const [embeddingDim, setEmbeddingDim] = useState<string>(profile?.embedding_dim ? String(profile.embedding_dim) : '')
  const [vision, setVision] = useState<GroupDraft>(fromProfile(profile?.vision_provider || '', profile?.vision_base_url || '', profile?.vision_model || ''))
  const [visionEnabled, setVisionEnabled] = useState(!!profile?.vision_model)
  const [isDefault, setIsDefault] = useState(profile?.is_default || false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [models, setModels] = useState<Partial<Record<ProfilePurpose, string[]>>>({})
  const [probing, setProbing] = useState(false)
  const [budgetOptions, setBudgetOptions] = useState<AgentBudgetOptions | null>(null)
  const [budgetError, setBudgetError] = useState('')
  const [customBudget, setCustomBudget] = useState(!!profile?.agent_budget)
  const [budgetDraft, setBudgetDraft] = useState<Partial<Record<keyof AgentBudgetOverride, string>>>(() => Object.fromEntries(Object.entries(profile?.agent_budget || {}).map(([k, v]) => [k, String(v)])))
  useEffect(() => {
    let live = true
    api.budgetOptions().then(options => { if (live) setBudgetOptions(options) }).catch(() => { if (live) setBudgetError('预算选项加载失败，请重新打开表单重试') })
    return () => { live = false }
  }, [])
  const budgetFields = [
    ['max_tool_calls', '最多工具调用次数'], ['max_duration_seconds', '最长运行时间（秒）'],
    ['max_input_tokens', '累计输入 Token'], ['max_output_tokens', '累计输出 Token'], ['max_visual_frames', '最多检查帧数'],
  ] as const

  const dirty = useMemo(() => {
    if (!editing) return !!(name || llm.base_url || llm.api_key || llm.model || asr.base_url || embedding.base_url)
    return true
  }, [editing, name, llm, asr, embedding])

  const back = () => {
    if (dirty && !window.confirm('离开后已填写的内容会丢失,确定返回?')) return
    onClose()
  }

  const profileId = profile?.id ?? 0
  const setGroup = (setter: (fn: (g: GroupDraft) => GroupDraft) => void) =>
    (patch: Partial<GroupDraft>) => setter(g => ({ ...g, ...patch }))

  const buildRequest = (): AIProfileRequest | null => {
    if (!name.trim()) { setErr('请填写配置名称'); return null }
    if (!llm.provider.trim() || !llm.base_url.trim() || !llm.model.trim()) { setErr('LLM 配置不完整'); return null }
    if (!asr.provider.trim() || !asr.base_url.trim() || !asr.model.trim()) { setErr('ASR 配置不完整'); return null }
    if (!embedding.provider.trim() || !embedding.base_url.trim() || !embedding.model.trim()) { setErr('embedding 配置不完整'); return null }
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
      llm_provider: llm.provider.trim(), llm_base_url: llm.base_url.trim(), llm_model: llm.model.trim(),
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
      toast.info('先填 Base URL 与 API Key(编辑时留空 Key 用已存密钥)')
      return
    }
    try {
      const res = await api.listModels(group.base_url.trim(), group.api_key.trim(), profileId, purpose)
      setModels(prev => ({ ...prev, [purpose]: res.models || [] }))
      toast.success(`拉取到 ${res.models?.length ?? 0} 个模型`)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '拉取模型列表失败')
    }
  }

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

      <GroupBlock
        title="对话模型"
        group={llm} setGroup={setGroup(setLlm)}
        purpose="llm" models={models.llm || []} onPull={pullModels}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.llm_api_key_masked})` : 'sk-…'}
        required
      />
      <GroupBlock
        title="语音识别"
        group={asr} setGroup={setGroup(setAsr)}
        purpose="asr" models={models.asr || []} onPull={pullModels}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.asr_api_key_masked})` : 'sk-…'}
        required
      />
      <GroupBlock
        title="向量模型"
        group={embedding} setGroup={setGroup(setEmbedding)}
        purpose="embedding" models={models.embedding || []} onPull={pullModels}
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
          purpose="vision" models={models.vision || []} onPull={pullModels}
          keyPlaceholder={editing ? `留空保留现有密钥(${profile?.vision_api_key_masked})` : 'sk-…'}
        />
      )}

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

      {err && <div className="login-err" style={{ marginTop: 14 }}>{err}</div>}

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

function GroupBlock({ title, group, setGroup, purpose, models, onPull, keyPlaceholder, urlPlaceholder, required }: {
  title: string
  group: GroupDraft
  setGroup: (patch: Partial<GroupDraft>) => void
  purpose: ProfilePurpose
  models: string[]
  onPull: (purpose: ProfilePurpose, group: GroupDraft) => void
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
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
        <input className="input" type="password" placeholder={keyPlaceholder} value={group.api_key} onChange={e => setGroup({ api_key: e.target.value })} />
        <div style={{ display: 'flex', gap: 8 }}>
          <ModelCombobox value={group.model} onChange={model => setGroup({ model })} models={models} />
          <button type="button" className="btn btn-sm" style={{ flex: 'none' }} onClick={() => onPull(purpose, group)}>拉模型</button>
        </div>
      </div>
    </div>
  )
}
