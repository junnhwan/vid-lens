'use client'

import { useEffect, useMemo, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { AIProfile, AIProfileRequest, ProfilePurpose } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'

// BYOK 配置编辑模态(新建/编辑)。一个 profile 覆盖 llm / asr / embedding / vision 四组配置;
// rerank 无 profile 配置项(服务端为确定性 rerank),编辑器不提供。
// 拉模型列表 / 维度探测走服务端代理:编辑态 api_key 留空时用已存密钥(profile_id)。

interface GroupDraft {
  provider: string
  base_url: string
  api_key: string
  model: string
}

const EMPTY_GROUP: GroupDraft = { provider: '', base_url: '', api_key: '', model: '' }

export function ProfileFormModal({ profile, onClose, onSaved }: {
  profile?: AIProfile
  onClose: () => void
  onSaved: () => void
}) {
  const toast = useToast()
  const editing = !!profile
  const [name, setName] = useState(profile?.name || '')
  const [llm, setLlm] = useState<GroupDraft>({ ...EMPTY_GROUP, provider: profile?.llm_provider || '', base_url: profile?.llm_base_url || '', model: profile?.llm_model || '' })
  const [asr, setAsr] = useState<GroupDraft>({ ...EMPTY_GROUP, provider: profile?.asr_provider || '', base_url: profile?.asr_base_url || '', model: profile?.asr_model || '' })
  const [embedding, setEmbedding] = useState<GroupDraft>({ ...EMPTY_GROUP, provider: profile?.embedding_provider || '', base_url: profile?.embedding_endpoint || '', model: profile?.embedding_model || '' })
  const [embeddingDim, setEmbeddingDim] = useState<string>(profile?.embedding_dim ? String(profile.embedding_dim) : '')
  const [vision, setVision] = useState<GroupDraft>({ ...EMPTY_GROUP, provider: profile?.vision_provider || '', base_url: profile?.vision_base_url || '', model: profile?.vision_model || '' })
  const [visionEnabled, setVisionEnabled] = useState(!!profile?.vision_model)
  const [isDefault, setIsDefault] = useState(profile?.is_default || false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [models, setModels] = useState<Partial<Record<ProfilePurpose, string[]>>>({})
  const [probing, setProbing] = useState(false)

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

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
    return {
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

  const datalistId = (purpose: string) => `models-${purpose}-${profileId || 'new'}`

  return (
    <div className="overlay" onClick={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="modal" style={{ width: 620, maxWidth: '94vw' }}>
        <div className="modal-head">
          <h3>{editing ? `编辑配置 · ${profile?.name}` : '新建 AI 配置'}</h3>
          <button className="btn btn-ic btn-ghost" onClick={onClose} aria-label="关闭"><Icon name="x" /></button>
        </div>
        <div className="modal-body" style={{ maxHeight: '72vh', overflowY: 'auto' }}>
          <div className="field-label">配置名称</div>
          <input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="例如:硅基流动 · 全家桶" />

          <GroupBlock
            title="对话模型(LLM)"
            help="OpenAI 兼容 chat/completions"
            group={llm} setGroup={setGroup(setLlm)}
            purpose="llm" models={models.llm} datalistId={datalistId('llm')} onPull={pullModels}
            keyPlaceholder={editing ? `留空保留现有密钥(${profile?.llm_api_key_masked})` : 'sk-…'}
            required
          />
          <GroupBlock
            title="语音识别(ASR)"
            help="OpenAI 兼容 audio/transcriptions"
            group={asr} setGroup={setGroup(setAsr)}
            purpose="asr" models={models.asr} datalistId={datalistId('asr')} onPull={pullModels}
            keyPlaceholder={editing ? `留空保留现有密钥(${profile?.asr_api_key_masked})` : 'sk-…'}
            required
          />

          <div className="field-label" style={{ marginTop: 18 }}>向量模型(Embedding)</div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
            <input className="input" placeholder="provider(如 siliconflow)" value={embedding.provider} onChange={e => setEmbedding(g => ({ ...g, provider: e.target.value }))} />
            <input className="input" placeholder="endpoint(/embeddings 完整地址)" value={embedding.base_url} onChange={e => setEmbedding(g => ({ ...g, base_url: e.target.value }))} />
          </div>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
            <input className="input" type="password" placeholder={editing ? `留空保留现有密钥(${profile?.embedding_api_key_masked})` : 'sk-…'} value={embedding.api_key} onChange={e => setEmbedding(g => ({ ...g, api_key: e.target.value }))} />
            <div style={{ display: 'flex', gap: 8 }}>
              <input className="input" list={datalistId('embedding')} placeholder="模型(如 bge-m3)" value={embedding.model} onChange={e => setEmbedding(g => ({ ...g, model: e.target.value }))} />
              <datalist id={datalistId('embedding')}>{(models.embedding || []).map(m => <option key={m} value={m} />)}</datalist>
              <button className="btn btn-sm" style={{ flex: 'none' }} onClick={() => void pullModels('embedding', embedding)}>拉模型</button>
            </div>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginTop: 10 }}>
            <input className="input mono" style={{ width: 120 }} placeholder="维度" value={embeddingDim} onChange={e => setEmbeddingDim(e.target.value)} />
            <button className="btn btn-sm" disabled={probing} onClick={() => void probeDim()}>
              <Icon name="scan" size="sm" />{probing ? '探测中…' : '探测维度'}
            </button>
            <span className="field-help" style={{ marginTop: 0 }}>向量化一条短文本测出向量长度;更换向量模型会使索引 needs_rebuild</span>
          </div>

          <div className="pref-row" style={{ marginTop: 18 }}>
            <div className="pr-body">
              <b>视觉模型(Vision,可选)</b>
              <span>用于关键帧 OCR 与画面描述;多模态检索与 investigate_visual 依赖它</span>
            </div>
            <button
              className={`switch${visionEnabled ? ' on' : ''}`}
              onClick={() => setVisionEnabled(v => !v)}
              aria-label="启用视觉模型"
            />
          </div>
          {visionEnabled && (
            <GroupBlock
              title=""
              help="OpenAI 兼容视觉模型"
              group={vision} setGroup={setGroup(setVision)}
              purpose="vision" models={models.vision} datalistId={datalistId('vision')} onPull={pullModels}
              keyPlaceholder={editing ? `留空保留现有密钥(${profile?.vision_api_key_masked})` : 'sk-…'}
            />
          )}

          <div className="pref-row" style={{ marginTop: 18 }}>
            <div className="pr-body">
              <b>设为默认配置</b>
              <span>问答、转写与索引默认使用该配置;设为默认会取消其它默认</span>
            </div>
            <button
              className={`switch${isDefault ? ' on' : ''}`}
              onClick={() => setIsDefault(v => !v)}
              aria-label="设为默认配置"
            />
          </div>

          {err && <div className="login-err" style={{ marginTop: 12 }}>{err}</div>}
        </div>
        <div className="modal-foot">
          <button className="btn" onClick={onClose} disabled={busy}>取消</button>
          <button className="btn btn-primary" onClick={() => void save()} disabled={busy}>
            {busy ? '保存中…' : editing ? '保存修改' : '创建配置'}
          </button>
        </div>
      </div>
    </div>
  )
}

function GroupBlock({ title, help, group, setGroup, purpose, models, datalistId, onPull, keyPlaceholder, required }: {
  title: string
  help: string
  group: GroupDraft
  setGroup: (patch: Partial<GroupDraft>) => void
  purpose: ProfilePurpose
  models?: string[]
  datalistId: string
  onPull: (purpose: ProfilePurpose, group: GroupDraft) => void
  keyPlaceholder: string
  required?: boolean
}) {
  const canPull = !!group.base_url.trim() // 编辑态 api_key 留空时走已存密钥
  return (
    <div style={{ marginTop: 18 }}>
      {title && <div className="field-label">{title}{required && <span style={{ color: 'var(--acc-strong)' }}> · 必填</span>}</div>}
      {title && <div className="field-help" style={{ marginTop: 0, marginBottom: 6 }}>{help}</div>}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10 }}>
        <input className="input" placeholder="provider" value={group.provider} onChange={e => setGroup({ provider: e.target.value })} />
        <input className="input" placeholder="Base URL" value={group.base_url} onChange={e => setGroup({ base_url: e.target.value })} />
      </div>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 10, marginTop: 10 }}>
        <input className="input" type="password" placeholder={keyPlaceholder} value={group.api_key} onChange={e => setGroup({ api_key: e.target.value })} />
        <div style={{ display: 'flex', gap: 8 }}>
          <input className="input" list={datalistId} placeholder="模型" value={group.model} onChange={e => setGroup({ model: e.target.value })} />
          <datalist id={datalistId}>{(models || []).map(m => <option key={m} value={m} />)}</datalist>
          <button className="btn btn-sm" style={{ flex: 'none' }} disabled={!canPull} onClick={() => onPull(purpose, group)}>拉模型</button>
        </div>
      </div>
    </div>
  )
}
