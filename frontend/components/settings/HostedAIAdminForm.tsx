import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { api, ApiError } from '@/lib/api'
import type { HostedAIAdmin, HostedAIRequest } from '@/lib/types'
import { useShell } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { LoadingBlock, ErrorState } from '@/components/ui/AsyncState'
import './ProfileForm.css'

const GROUPS = [
  { key: 'llm', label: '对话', url: 'llm_base_url', hint: 'API 基础地址，包含 /v1；系统追加 /chat/completions。' },
  { key: 'vision', label: '视觉', url: 'vision_base_url', hint: 'API 基础地址，包含 /v1；系统追加 /chat/completions。' },
  { key: 'asr', label: '语音识别', url: 'asr_base_url', hint: 'API 基础地址，包含 /v1；系统追加 /audio/transcriptions。' },
  { key: 'embedding', label: '向量检索', url: 'embedding_endpoint', hint: '完整接口地址，以 /embeddings 结尾。' },
  { key: 'rerank', label: '重排序', url: 'rerank_endpoint', hint: '完整接口地址，以 /rerank 结尾。' },
] as const

function toDraft(profile: HostedAIAdmin): HostedAIRequest {
  return {
    enabled: profile.enabled, name: profile.name || '作者免费 AI',
    llm_provider: profile.llm_provider || 'openai', llm_base_url: profile.llm_base_url || '', llm_model: profile.llm_model || '',
    llm_context_tokens: profile.llm_context_tokens || 0,
    asr_provider: profile.asr_provider || 'openai', asr_base_url: profile.asr_base_url || '', asr_model: profile.asr_model || '',
    embedding_provider: profile.embedding_provider || 'siliconflow', embedding_endpoint: profile.embedding_endpoint || '', embedding_model: profile.embedding_model || '', embedding_dim: profile.embedding_dim || 1024,
    vision_provider: profile.vision_provider || 'openai', vision_base_url: profile.vision_base_url || '', vision_model: profile.vision_model || '',
    rerank_provider: profile.rerank_provider || 'siliconflow', rerank_endpoint: profile.rerank_endpoint || '', rerank_model: profile.rerank_model || '',
    agent_budget: profile.agent_budget,
  }
}

export function HostedAIAdminForm({ onClose, onSaved }: { onClose: () => void; onSaved: () => Promise<void> }) {
  const { registerLeaveGuard } = useShell()
  const toast = useToast()
  const [profile, setProfile] = useState<HostedAIAdmin | null>(null)
  const [draft, setDraft] = useState<HostedAIRequest | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const initial = useRef('')
  const dirty = !!draft && JSON.stringify(draft) !== initial.current
  const load = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      const next = await api.hostedAIAdmin()
      const nextDraft = toDraft(next)
      initial.current = JSON.stringify(nextDraft)
      setProfile(next)
      setDraft(nextDraft)
    } catch { setLoadError('免费服务管理配置加载失败，请确认当前账号具有管理权限后重试。') }
    finally { setLoading(false) }
  }, [])
  useEffect(() => { void load() }, [load])
  useEffect(() => {
    registerLeaveGuard(dirty ? () => window.confirm('免费服务配置有未保存修改，放弃修改并离开吗？') : null)
    return () => registerLeaveGuard(null)
  }, [dirty, registerLeaveGuard])

  const close = () => {
    if (dirty && !window.confirm('免费服务配置有未保存修改，放弃修改并关闭吗？')) return
    registerLeaveGuard(null)
    onClose()
  }
  const change = (key: keyof HostedAIRequest, value: string | number | boolean) => setDraft(previous => previous ? { ...previous, [key]: value } : previous)
  const save = async (event: FormEvent) => {
    event.preventDefault()
    if (!draft || busy) return
    setError('')
    setBusy(true)
    try {
      const request = { ...draft }
      // Never echo masked credentials. Blank password fields preserve server-side keys.
      for (const { key } of GROUPS) {
        const field = `${key}_api_key` as const
        const value = request[field]?.trim()
        if (value) request[field] = value
        else delete request[field]
      }
      const next = await api.updateHostedAI(request)
      const nextDraft = toDraft(next)
      initial.current = JSON.stringify(nextDraft)
      setProfile(next)
      setDraft(nextDraft)
      registerLeaveGuard(null)
      await onSaved()
      toast.success('免费服务已更新，后续请求将使用最新配置')
    } catch (e) { setError(e instanceof ApiError ? e.message : '保存失败，请重试。') }
    finally { setBusy(false) }
  }

  return (
    <div className="hosted-admin">
      <div className="hosted-ai-heading"><h4>免费服务管理</h4><button className="btn btn-sm btn-ghost" disabled={busy} onClick={close}>关闭管理</button></div>
      <p className="hosted-ai-sync">仅作者账号可管理。保存后自动应用到所有已启用免费配置的账号；进行中的任务可能继续使用原配置。</p>
      {loading ? <LoadingBlock label="正在加载管理配置…" variant="card" /> : loadError ? <ErrorState message={loadError} onRetry={() => void load()} /> : draft && profile && (
        <form onSubmit={event => void save(event)}>
          <fieldset disabled={busy} style={{ border: 0, padding: 0, margin: 0, minWidth: 0 }}>
            <label className="hosted-admin-state"><input type="checkbox" checked={draft.enabled} onChange={e => change('enabled', e.target.checked)} />开放免费服务<span className="hosted-ai-sync">取消勾选并保存即可暂停。</span></label>
            <div className="hosted-admin-settings">
              <label className="profile-input-label">配置名称<input className="input" value={draft.name} required onChange={e => change('name', e.target.value)} /></label>
              <label className="profile-input-label">向量维度<input className="input" type="number" min={1} step={1} value={draft.embedding_dim} required onChange={e => change('embedding_dim', Number(e.target.value))} /></label>
            </div>
            {GROUPS.map(({ key, label, url }) => <fieldset key={key} className="hosted-admin-group">
              <legend>{label}</legend>
              <div className="profile-group-grid">
                <label className="profile-input-label">{label} Provider<input className="input" required value={draft[`${key}_provider`] || ''} onChange={e => change(`${key}_provider`, e.target.value)} autoComplete="off" /></label>
                <label className="profile-input-label">{label}模型<input className="input" required value={draft[`${key}_model`] || ''} onChange={e => change(`${key}_model`, e.target.value)} autoComplete="off" /></label>
                <label className="profile-input-label">{label}接口地址<input className="input" type="url" required value={draft[url] || ''} onChange={e => change(url, e.target.value)} autoComplete="off" /></label>
                <label className="profile-input-label">{label} API Key<input className="input" type="password" autoComplete="new-password" value={draft[`${key}_api_key`] || ''} placeholder={profile[`${key}_api_key_masked`] ? '已保存；留空保持原密钥' : '填写 API Key'} onChange={e => change(`${key}_api_key`, e.target.value)} /></label>
              </div>
              <p className="profile-url-hint">{GROUPS.find(group => group.key === key)?.hint}</p>
            </fieldset>)}
            <p className="profile-url-hint">普通用户只会看到模型名称，不会获得接口地址或密钥。更新向量模型或维度可能需要重建已有视频索引。</p>
            {error && <p className="hosted-ai-error" role="alert">{error}</p>}
            <div className="hosted-ai-actions"><button className="btn btn-sm btn-primary" type="submit" disabled={busy}>{busy ? '正在保存…' : '保存免费服务配置'}</button><span className="hosted-ai-sync">密钥输入留空会保留已保存的密钥。</span></div>
          </fieldset>
        </form>
      )}
    </div>
  )
}
