import { useCallback, useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { HostedAIStatus } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'
import { HostedAIAdminForm } from './HostedAIAdminForm'
import './HostedAISection.css'

const DEFAULT_NOTICE = '作者为爱发电提供免费 AI 服务，不保证渠道可用性，可能限流或暂停。你可以随时改用自备配置。'

export function HostedAISection({ readOnly, active, onActivated }: {
  readOnly: boolean
  active: boolean
  onActivated: () => Promise<void>
}) {
  const toast = useToast()
  const [status, setStatus] = useState<HostedAIStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [adminOpen, setAdminOpen] = useState(false)
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try { setStatus(await api.hostedAI()) }
    catch { setError('免费服务状态暂时无法加载，请重试。') }
    finally { setLoading(false) }
  }, [])

  useEffect(() => { void load() }, [load])

  const activate = async () => {
    if (busy || readOnly || !status?.enabled) return
    setBusy(true)
    setError('')
    try {
      await api.activateHostedAI()
      await onActivated()
      toast.success('已使用免费 AI 配置，后续请求将自动同步作者的最新配置')
    } catch {
      setError('免费配置暂时无法启用，请稍后重试或使用自备配置。')
    } finally { setBusy(false) }
  }

  const profile = status?.profile
  const models = profile ? [
    ['对话', profile.llm_model], ['视觉', profile.vision_model],
    ['语音识别', profile.asr_model], ['向量检索', profile.embedding_model],
    ['重排序', profile.rerank_model],
  ] : []

  return (
    <section className="hosted-ai" aria-labelledby="hosted-ai-title">
      <div className="hosted-ai-heading">
        <div className="hosted-ai-title"><Icon name="bolt" /><h3 id="hosted-ai-title">作者的免费 AI</h3></div>
        <span className={'chip ' + (status?.enabled ? 'chip-acc' : '')}>
          {loading ? '读取状态中' : status ? status.enabled ? '免费开放' : '暂时停用' : '状态未知'}
        </span>
        {active && <span className="chip chip-acc"><Icon name="check" size="sm" />当前默认</span>}
      </div>
      <p className="hosted-ai-notice">{status?.notice || DEFAULT_NOTICE}</p>
      {models.length > 0 && <dl className="hosted-ai-models">{models.map(([label, model]) => (
        <div key={label}><dt>{label}</dt><dd>{model || '暂未配置'}</dd></div>
      ))}</dl>}
      <p className="hosted-ai-sync">启用后自动跟随作者更新，无需重复配置。已有自备配置会保留；将自备配置设为默认即可切换。</p>
      <div className="hosted-ai-actions">
        <button className="btn btn-sm btn-primary" disabled={loading || busy || readOnly || !status?.enabled || active} onClick={() => void activate()}>
          <Icon name={active ? 'check' : 'bolt'} size="sm" />
          {busy ? '正在启用…' : active ? '已使用免费配置' : '一键使用免费 AI 配置'}
        </button>
        {readOnly && <span className="hosted-ai-sync">注册并登录自己的账号后即可启用。</span>}
        {!loading && status && !status.enabled && <span className="hosted-ai-sync">服务暂停期间请使用自备配置。</span>}
        {status?.can_manage && !readOnly && <button className="btn btn-sm btn-ghost" onClick={() => setAdminOpen(true)} disabled={adminOpen}>
          <Icon name="settings" size="sm" />管理免费服务
        </button>}
      </div>
      {error && <div className="hosted-ai-error" role="alert">{error}<button className="btn btn-sm btn-ghost" disabled={loading} onClick={() => void load()}>重新加载状态</button></div>}
      {adminOpen && status?.can_manage && !readOnly && <HostedAIAdminForm onClose={() => setAdminOpen(false)} onSaved={async () => {
        await load()
        await onActivated()
      }} />}
    </section>
  )
}
