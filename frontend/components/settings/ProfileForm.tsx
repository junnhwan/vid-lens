import { useEffect, useMemo, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { AIProfile, AIProfileRequest, ProfilePurpose, AgentBudgetOverride, AgentBudgetOptions } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'
import { ModelCombobox } from '@/components/settings/ModelCombobox'
import { PROVIDER_PRESETS, matchPreset } from '@/lib/providerPresets'
import { useShell } from '@/components/shell/AppShell'
import { CapabilityProbe, type ProbeTarget } from '@/components/settings/CapabilityProbe'
import './ProfileForm.css'

interface GroupDraft {
  provider: string
  base_url: string
  api_key: string
  model: string
  preset: string
}

type ListFeedback = { kind: 'loading' | 'success' | 'error'; message: string }
const connectionKey = (group: GroupDraft) => [group.provider, group.base_url, group.api_key].join('\u0000')

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
  const [clearGroups, setClearGroups] = useState<ProfilePurpose[]>([])
  const [isDefault, setIsDefault] = useState(imported?.is_default || profile?.is_default || false)
  const [reuseASR, setReuseASR] = useState(false)
  const [reuseEmbedding, setReuseEmbedding] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [models, setModels] = useState<Partial<Record<ProfilePurpose, string[]>>>({})
  const [listStatus, setListStatus] = useState<Partial<Record<ProfilePurpose, ListFeedback>>>({})
  const currentGroups = useRef({ llm, asr, embedding, vision })
  currentGroups.current = { llm, asr, embedding, vision }
  const listRequests = useRef<Partial<Record<ProfilePurpose, number>>>({})
  const [probing, setProbing] = useState(false)
  const dimRequest = useRef(0)
  const [observedDim, setObservedDim] = useState<number | null>(null)
  const [dimError, setDimError] = useState('')
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

  const currentSnapshot = useMemo(() => JSON.stringify({ clearGroups, name, llm, llmContextTokens, asr, embedding, embeddingDim, vision, visionEnabled, isDefault, customBudget, budgetDraft }), [clearGroups, name, llm, llmContextTokens, asr, embedding, embeddingDim, vision, visionEnabled, isDefault, customBudget, budgetDraft])
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
  const setGroup = (purpose: ProfilePurpose, setter: (fn: (g: GroupDraft) => GroupDraft) => void) =>
    (patch: Partial<GroupDraft>) => {
      setter(group => ({ ...group, ...patch }))
      if ('provider' in patch || 'base_url' in patch || 'api_key' in patch || 'preset' in patch) {
        listRequests.current[purpose] = (listRequests.current[purpose] || 0) + 1
        setModels(previous => ({ ...previous, [purpose]: [] }))
        setListStatus(previous => ({ ...previous, [purpose]: undefined }))
      }
      if (purpose === 'embedding' && ('provider' in patch || 'base_url' in patch || 'api_key' in patch || 'model' in patch || 'preset' in patch)) {
        dimRequest.current += 1
        setObservedDim(null)
        setDimError('')
      }
    }

  const reusableConnection = !!llm.api_key.trim() && !validateModelURL(llm.base_url, false, llm.preset) && !!llm.base_url.trim()
  useEffect(() => {
    if (reuseASR) setGroup('asr', setAsr)({ provider:llm.provider, base_url:llm.base_url.replace(/\/+$/, ''), api_key:llm.api_key, preset:llm.preset })
    if (reuseEmbedding) setGroup('embedding', setEmbedding)({ provider:llm.provider, base_url:llm.base_url.replace(/\/+$/, '') + '/embeddings', api_key:llm.api_key, preset:llm.preset })
  // Reuse follows connection edits; the capability model stays independently selected.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reuseASR, reuseEmbedding, llm.provider, llm.base_url, llm.api_key, llm.preset])

  const buildRequest = (): AIProfileRequest | null => {
    if ((reuseASR || reuseEmbedding) && !reusableConnection) { setErr('复用连接需要有效的对话地址与实际 API Key；也可切换为单独配置'); return null }
    if (!name.trim()) { setErr('请填写配置名称'); return null }
    const contextTokens = llmContextTokens.trim() === '' ? 0 : Number(llmContextTokens)
    if (!Number.isSafeInteger(contextTokens) || (contextTokens !== 0 && (contextTokens < 8192 || contextTokens > 1048576))) { setErr('模型上下文窗口需为 8192–1048576 token，或留空'); return null }
    const present = (g: GroupDraft) => !!(g.base_url.trim() || g.model.trim() || g.api_key.trim())
    const enabled = { llm: present(llm) || contextTokens !== 0, asr: present(asr), embedding: present(embedding) || embeddingDim.trim() !== '', vision: visionEnabled }
    const dim = enabled.embedding ? Number(embeddingDim) : 0
    if (!Object.values(enabled).some(Boolean)) { setErr('请至少配置一个完整模型组'); return null }
    for (const [purpose, label, group] of [['llm', '对话', llm], ['asr', '语音识别', asr], ['embedding', '向量', embedding], ['vision', '视觉', vision]] as const) {
      if (!enabled[purpose]) continue
      if (!group.provider.trim() || !group.base_url.trim() || !group.model.trim()) { setErr(`${label}配置不完整，请填写整组或全部留空`); return null }
      if (!group.api_key.trim() && !profile?.[`${purpose}_api_key_masked`]) { setErr(`${label} API Key 不能为空`); return null }
      const problem = validateModelURL(group.base_url, purpose === 'embedding', group.preset)
      if (problem) { setErr(`${label}地址：${problem}`); return null }
    }
    if (enabled.embedding && (!Number.isSafeInteger(dim) || dim <= 0)) { setErr('embedding 维度需为正整数:先探测,或手动填写'); return null }
    if (enabled.embedding && observedDim !== null && dim !== observedDim) { setErr('向量维度与最新探测结果不符，请采用检测值或重新检查模型'); return null }
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
      clear_groups: editing ? clearGroups.filter(group => !enabled[group]) : [],
      name: name.trim(),
      llm_provider: enabled.llm ? llm.provider.trim() : '', llm_base_url: llm.base_url.trim(), llm_model: llm.model.trim(), llm_context_tokens: contextTokens,
      ...(llm.api_key.trim() ? { llm_api_key: llm.api_key.trim() } : {}),
      asr_provider: enabled.asr ? asr.provider.trim() : '', asr_base_url: asr.base_url.trim(), asr_model: asr.model.trim(),
      ...(asr.api_key.trim() ? { asr_api_key: asr.api_key.trim() } : {}),
      embedding_provider: enabled.embedding ? embedding.provider.trim() : '', embedding_endpoint: embedding.base_url.trim(), embedding_model: embedding.model.trim(),
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
      setListStatus(previous => ({ ...previous, [purpose]: { kind: 'error', message: '先填写地址和 API Key；编辑已保存配置可留空密钥。' } }))
      return
    }
    const request = (listRequests.current[purpose] || 0) + 1
    listRequests.current[purpose] = request
    const key = connectionKey(group)
    setListStatus(previous => ({ ...previous, [purpose]: { kind: 'loading', message: '正在读取此服务的模型列表…' } }))
    try {
      const res = await api.listModels(group.base_url.trim(), group.api_key.trim(), profileId, purpose)
      if (request !== listRequests.current[purpose] || key !== connectionKey(currentGroups.current[purpose])) return
      setModels(prev => ({ ...prev, [purpose]: res.models || [] }))
      setListStatus(previous => ({ ...previous, [purpose]: { kind: 'success', message: `找到 ${res.models?.length ?? 0} 个模型。列表可读取不代表模型一定可调用，保存前可运行能力检查。` } }))
    } catch (e) {
      if (request !== listRequests.current[purpose] || key !== connectionKey(currentGroups.current[purpose])) return
      setModels(previous => ({ ...previous, [purpose]: [] }))
      setListStatus(previous => ({ ...previous, [purpose]: { kind: 'error', message: `${e instanceof ApiError ? e.message : '拉取模型列表失败'}。检查地址、密钥和服务商权限后重试；也可手动填写模型 ID。` } }))
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
    if (reuseEmbedding && !reusableConnection) { setDimError('请先补齐复用的对话地址与实际密钥'); return }
    if (!embedding.base_url.trim() || !embedding.model.trim() || (!embedding.api_key.trim() && !profileId)) {
      toast.info('先填 endpoint、模型与 API Key(编辑时留空 Key 用已存密钥)')
      return
    }
    setProbing(true)
    setDimError('')
    const request = ++dimRequest.current
    const key = connectionKey(embedding) + '\u0000' + embedding.model
    try {
      const res = await api.probeEmbeddingDim(embedding.base_url.trim(), embedding.api_key.trim(), embedding.model.trim(), profileId)
      if (request === dimRequest.current && key === connectionKey(currentGroups.current.embedding) + '\u0000' + currentGroups.current.embedding.model) setObservedDim(res.dimension)
    } catch (e) {
      if (request === dimRequest.current && key === connectionKey(currentGroups.current.embedding) + '\u0000' + currentGroups.current.embedding.model) setDimError(e instanceof ApiError ? e.message : '维度探测失败，请检查模型与密钥后重试')
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

      <p className="muted">至少填写一个完整模型组。无需使用的组可全部留空；清除已保存的组请点击“清除整组”。</p><label className="field-label">配置名称</label>
      <input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="例如:硅基流动" />
      <details className="disclosure" style={{ marginTop: 12 }}>
        <summary><Icon name="info" size="sm" />服务地址填写示例与拼接规则</summary>
        <div className="disclosure-body">
          <p>本项目使用 OpenAI 兼容协议。对话、语音识别、视觉填写 API 基础地址，系统按用途追加路径；向量模型填写完整接口（以 <code>/embeddings</code> 结尾），系统不再追加。</p>
          <ul className="disclosure-list">
            <li><b>硅基流动</b><code>https://api.siliconflow.cn/v1</code></li>
            <li><b>OpenAI</b><code>https://api.openai.com/v1</code></li>
            <li><b>DeepSeek 对话</b><code>https://api.deepseek.com/v1</code><span>主域名或 /v1 均可</span></li>
            <li><b>向量示例</b><code>https://api.siliconflow.cn/v1/embeddings</code></li>
          </ul>
          <p>硅基流动和 OpenAI 只填主域名会漏掉 <code>/v1</code>；将完整接口填进基础地址会重复路径。各服务商支持的能力与模型不同，先核对其文档，保存前可运行能力检查。</p>
        </div>
      </details>

      <GroupBlock
        onClear={() => { setLlm(fromProfile('', '', '')); setLlmContextTokens(''); setClearGroups(g => [...new Set([...g, 'llm' as const])]) }}
        title="对话模型"
        group={llm} setGroup={setGroup('llm', setLlm)}
        purpose="llm" models={models.llm || []} onPull={pullModels} listStatus={listStatus.llm}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.llm_api_key_masked})` : 'sk-…'}
      />
      <label className="field-label" htmlFor="llm-context-tokens">模型上下文窗口（token，可选）</label>
      <input id="llm-context-tokens" className="input mono" type="number" min={8192} max={1048576} step={1} value={llmContextTokens} onChange={e => setLlmContextTokens(e.target.value)} placeholder="留空按 8192 计算" />
      <small style={{ color: 'var(--tx-3)' }}>按模型服务实际允许的上下文填写。摘要能放入时使用一次请求；超出时自动分段。请预留输出空间，填大于实际上限可能导致请求失败。</small>
      <GroupBlock
        onClear={() => { setAsr(fromProfile('', '', '')); setReuseASR(false); setClearGroups(g => [...new Set([...g, 'asr' as const])]) }}
        title="语音识别"
        group={asr} setGroup={setGroup('asr', setAsr)}
        purpose="asr" models={models.asr || []} onPull={pullModels} listStatus={listStatus.asr}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.asr_api_key_masked})` : 'sk-…'}
        reuse={reuseASR} onReuse={setReuseASR} reuseAvailable={reusableConnection}
      />
      <GroupBlock
        onClear={() => { setEmbedding(fromProfile('', '', '')); setEmbeddingDim(''); setReuseEmbedding(false); setObservedDim(null); setClearGroups(g => [...new Set([...g, 'embedding' as const])]) }}
        title="向量模型"
        group={embedding} setGroup={setGroup('embedding', setEmbedding)}
        purpose="embedding" models={models.embedding || []} onPull={pullModels} listStatus={listStatus.embedding}
        keyPlaceholder={editing ? `留空保留现有密钥(${profile?.embedding_api_key_masked})` : 'sk-…'}
        urlPlaceholder="https://…/v1/embeddings"
        reuse={reuseEmbedding} onReuse={setReuseEmbedding} reuseAvailable={reusableConnection}
      />
      <div className="profile-dimension">
        <label className="field-label" htmlFor="embedding-dim">向量维度</label>
        <div className="profile-dimension-controls"><input id="embedding-dim" className="input mono" inputMode="numeric" placeholder="维度" value={embeddingDim} onChange={e => setEmbeddingDim(e.target.value)} />
          <button type="button" className="btn btn-sm" disabled={probing} onClick={() => void probeDim()}><Icon name="scan" size="sm" />{probing ? '检测中…' : '检测维度'}</button></div>
        <p className={'profile-field-feedback ' + (dimError ? 'error' : observedDim !== null && Number(embeddingDim) !== observedDim ? 'error' : observedDim !== null ? 'success' : '')} role="status">
          {probing ? '正在用当前向量模型发送小样本请求…' : dimError || (observedDim !== null ? Number(embeddingDim) === observedDim ? `检测得到 ${observedDim} 维，与配置一致。` : `检测得到 ${observedDim} 维，当前填写 ${embeddingDim || '为空'}。` : '检测结果会显示在这里，方便与填写值核对。')}
          {observedDim !== null && Number(embeddingDim) !== observedDim && <button type="button" className="btn btn-sm" onClick={() => setEmbeddingDim(String(observedDim))}>采用 {observedDim} 维</button>}
        </p>
      </div>

      <div className="pref-row" style={{ marginTop: 22 }}>
        <div className="pr-body">
          <b>视觉模型</b>
          <span>可选。画面描述使用视觉模型，OCR 使用本地工具</span>
        </div>
        <button
          type="button"
          className={`switch${visionEnabled ? ' on' : ''}`}
          onClick={() => { if (visionEnabled) { setVision(fromProfile('', '', '')); setClearGroups(g => [...new Set([...g, 'vision' as const])]) }; setVisionEnabled(v => !v) }}
          aria-label="启用视觉模型"
        />
      </div>
      {visionEnabled && (
        <GroupBlock
          title=""
          group={vision} setGroup={setGroup('vision', setVision)}
          purpose="vision" models={models.vision || []} onPull={pullModels} listStatus={listStatus.vision}
          keyPlaceholder={editing ? `留空保留现有密钥(${profile?.vision_api_key_masked})` : 'sk-…'}
          />
      )}

      <CapabilityProbe targets={probeTargets.filter(target => !!target.model.trim())} disabled={busy || ((reuseASR || reuseEmbedding) && !reusableConnection)} />

      {!profile?.read_only && profile?.source !== 'hosted' && (
        <details className="disclosure" style={{ marginTop: 22 }}>
          <summary><Icon name="bolt" size="sm" />Agent 执行预算</summary>
          <div className="disclosure-body plain">
            <p>用于此配置下新开始的 Agent 问答和学习笔记任务。输入、输出 Token 是整次任务中多次调用的累计额度；模型上下文窗口是单次请求容量，单独设置。Token 用量可能为估算。</p>
            {profile?.agent_budget_error && <p role="alert">{profile.agent_budget_error}；可恢复默认或重新填写预算修复。</p>}
            {budgetError && <p role="alert">{budgetError}</p>}
            <div style={{ display: 'flex', gap: 18, margin: '2px 0' }}>
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
            <div><button type="button" className="btn btn-sm" style={{ marginTop: 12 }} onClick={() => { setCustomBudget(false); setBudgetDraft({}) }}>恢复默认</button></div>
            <p>保存后新运行生效，进行中的运行保持原预算。修改预算无需重新输入 API Key。</p>
          </div>
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

function GroupBlock({ title, group, setGroup, purpose, models, onPull, listStatus, keyPlaceholder, urlPlaceholder, onClear, required, reuse, onReuse, reuseAvailable }: {
  title: string
  group: GroupDraft
  setGroup: (patch: Partial<GroupDraft>) => void
  purpose: ProfilePurpose
  models: string[]
  onPull: (purpose: ProfilePurpose, group: GroupDraft) => void
  listStatus?: ListFeedback
  keyPlaceholder: string
  urlPlaceholder?: string
  onClear?: () => void
  required?: boolean
  reuse?: boolean
  onReuse?: (reuse:boolean) => void
  reuseAvailable?: boolean
}) {
  return (
    <section className="profile-model-card" aria-label={title || '视觉模型'}>
      <div className="profile-model-card-head"><h3>{title || '视觉模型'}{required && <span> · 必填</span>}</h3><small>{purpose.toUpperCase()}</small>{onClear && <button type="button" className="meta-link" onClick={onClear}>清除整组</button>}</div>
      {onReuse && <div className="profile-connection-choice"><div className="seg"><button type="button" aria-pressed={!!reuse} className={reuse ? 'on' : ''} disabled={!reuseAvailable && !reuse} onClick={() => onReuse(true)}>复用对话连接</button><button type="button" aria-pressed={!reuse} className={!reuse ? 'on' : ''} onClick={() => onReuse(false)}>单独配置</button></div><p>{reuse ? '连接跟随对话设置；请单独选择此能力的模型，并检查服务是否支持。' : !reuseAvailable ? '填写有效对话地址与实际密钥后可复用。已保存的脱敏密钥无法复制。' : '可复用对话地址与密钥；模型仍单独选择。'}</p></div>}
      {!reuse && <div className="profile-group-grid">
        <label className="profile-input-label">服务商<select
          className="input"
          value={group.preset}
          onChange={e => setGroup(applyPreset(e.target.value, group))}
        >
          {PROVIDER_PRESETS.map(p => (
            <option key={p.id} value={p.id}>{p.label}</option>
          ))}
        </select></label>
        <label className="profile-input-label">{purpose === 'embedding' ? '完整请求地址' : '服务 Base URL'}<input
          className="input"
          placeholder={urlPlaceholder || 'Base URL'}
          value={group.base_url}
          onChange={e => setGroup({ base_url: e.target.value })}
        /></label>
      </div>}
      {!reuse && <p className="profile-url-hint">
        {purpose === 'embedding' ? '填写完整 Embedding 接口地址，例如 https://api.siliconflow.cn/v1/embeddings；请求直接发送到此地址。' : `填写服务商要求的 API 基础地址，例如硅基流动 https://api.siliconflow.cn/v1；系统会追加 ${purpose === 'asr' ? '/audio/transcriptions' : '/chat/completions'}。不要填写完整接口路径。`}
      </p>}
      {validateModelURL(group.base_url, purpose === 'embedding', group.preset) && <p role="alert" style={{ fontSize: 12, color: 'var(--bad)' }}>{validateModelURL(group.base_url, purpose === 'embedding', group.preset)}</p>}
      <div className="profile-group-grid profile-model-row">
        {!reuse && <label className="profile-input-label">API Key<input className="input" type="password" autoComplete="off" placeholder={keyPlaceholder} value={group.api_key} onChange={e => setGroup({ api_key: e.target.value })} /></label>}
        <div className="profile-input-label"><label htmlFor={purpose + '-model'}>模型 ID</label><div className="profile-model-pick">
          <ModelCombobox id={purpose + '-model'} label={(title || '视觉模型') + '模型 ID'} value={group.model} onChange={model => setGroup({ model })} models={models} />
          <button type="button" className="btn btn-sm" style={{ flex: 'none' }} disabled={listStatus?.kind === 'loading' || (!!reuse && !reuseAvailable)} onClick={() => onPull(purpose, group)}>{listStatus?.kind === 'loading' ? '读取中…' : '读取模型'}</button>
        </div></div>
      </div>
      <p className={'profile-field-feedback ' + (listStatus?.kind || '')} role="status">{listStatus?.message || '模型列表尚未读取；也可以手动填写模型 ID。'}</p>
    </section>
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
