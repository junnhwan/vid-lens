import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { AIProfile, AIProfileRequest } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'
import { ProfileForm } from '@/components/settings/ProfileForm'
import { exportProfile, parseProfileImport } from '@/lib/profileTransfer'
import { CapabilityProbe, type ProbeTarget } from '@/components/settings/CapabilityProbe'
import { ErrorState, LoadingBlock } from '@/components/ui/AsyncState'
import { HostedAISection } from './HostedAISection'
import { ProfileImportHelp } from './ProfileImportHelp'

// BYOK AI 服务配置:profile 列表(一个 profile 覆盖 llm / asr / embedding / vision 四组能力),
// 作者提供的免费配置独立展示，由服务端维护并解析最新配置。
// 每张卡支持 测试 / 编辑 / 删除;密钥只回显脱敏值。

export function AIProfilesSection({ readOnly }: { readOnly: boolean }) {
  const toast = useToast()
  const [profiles, setProfiles] = useState<AIProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<AIProfile | undefined>(undefined)
  const [importDraft, setImportDraft] = useState<AIProfileRequest | null>(null)
  const [importWarning, setImportWarning] = useState(false)
  const [importHelpOpen, setImportHelpOpen] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const chooseImport = async (file?: File) => {
    if (!file) return
    try {
      if (file.size > 100_000) throw new Error('配置文件过大（上限 100 KB）')
      const result = parseProfileImport(await file.text())
      setImportDraft(result.profile)
      setImportWarning(result.ignoredSecrets)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : '读取配置文件失败')
    } finally {
      if (fileInput.current) fileInput.current.value = ''
    }
  }

  const download = (p: AIProfile) => {
    const blob = new Blob([exportProfile(p)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `vidlens-ai-profile-${p.id}.json`
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  }

  const load = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      setProfiles(await api.listProfiles())
    } catch (e) {
      setLoadError(e instanceof ApiError ? e.message : 'AI 配置加载失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const removeProfile = async (p: AIProfile) => {
    if (!window.confirm(`删除配置「${p.name}」?删除后问答会回退到服务端默认策略。`)) return
    try {
      await api.deleteProfile(p.id)
      toast.success('配置已删除')
      void load()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '删除失败')
    }
  }

  if (editorOpen) {
    return (
      <ProfileForm
        profile={editing}
        imported={importDraft || undefined}
        onClose={() => { setEditorOpen(false); setImportDraft(null) }}
        onSaved={() => { setImportDraft(null); void load() }}
      />
    )
  }

  return (
    <>
      <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 14 }}>AI 服务</h3>
      <HostedAISection readOnly={readOnly} active={profiles.some(p => p.source === 'hosted' && p.is_default)} onActivated={load} />
      {!readOnly && (
        <div className="profile-config-actions"><button
          className="btn btn-sm btn-primary"
          onClick={() => { setEditing(undefined); setImportDraft(null); setEditorOpen(true) }}
        >
          <Icon name="plus" size="sm" />新建配置
        </button><span className="profile-import-actions"><button className="btn btn-sm" onClick={() => fileInput.current?.click()}>导入 JSON</button><button type="button" className="profile-import-help" aria-label="JSON 导入格式说明" title="JSON 导入格式说明" aria-haspopup="dialog" onClick={() => setImportHelpOpen(true)}>?</button></span>
        <input ref={fileInput} type="file" accept=".json,application/json" hidden onChange={e => void chooseImport(e.target.files?.[0])} /></div>
      )}
      {importHelpOpen && <ProfileImportHelp onClose={() => setImportHelpOpen(false)} />}

      {importDraft && !editorOpen && <div className="card" style={{ padding: 16, marginBottom: 16 }}>
        <b>导入预览 · {importDraft.name}</b>
        <p>对话 {importDraft.llm_model} · 语音 {importDraft.asr_model} · 向量 {importDraft.embedding_model}（{importDraft.embedding_dim} 维）· 视觉 {importDraft.vision_model || '未配置'}</p>
        <p>将创建一份新配置；API Key 不导入，需在下一步重新填写。不会覆盖现有配置。默认不设为默认；若这是首份配置，服务端会按既有规则将其设为默认。</p>
        {importWarning && <p role="alert">文件包含敏感字段，已忽略，密钥不会进入表单或错误提示。</p>}
        <div style={{ display: 'flex', gap: 8 }}><button className="btn btn-sm btn-primary" onClick={() => { setEditing(undefined); setEditorOpen(true) }}>确认并编辑</button><button className="btn btn-sm" onClick={() => setImportDraft(null)}>取消导入</button></div>
      </div>}

      {loading && <LoadingBlock label="正在加载 AI 配置…" variant="card" />}
      {!loading && loadError && (
        <ErrorState message={loadError} onRetry={() => void load()} />
      )}
      {!loading && !loadError && profiles.length === 0 && (
        <div className="empty card">
          <Icon name="cpu" size="lg" />
          <b>还没有配置 AI 服务</b>
          {!readOnly && (
            <button className="btn btn-sm btn-primary" style={{ marginTop: 10 }} onClick={() => { setEditing(undefined); setImportDraft(null); setEditorOpen(true) }}>
              <Icon name="plus" size="sm" />新建配置
            </button>
          )}
        </div>
      )}

      {profiles.filter(p => p.source !== 'hosted').map(p => (
        <ProfileCard
          key={p.id}
          profile={p}
          readOnly={readOnly}
          onEdit={() => { setEditing(p); setImportDraft(null); setEditorOpen(true) }}
          onExport={() => download(p)}
          onDelete={() => void removeProfile(p)}
        />
      ))}
    </>
  )
}

function ProfileCard({ profile, readOnly, onEdit, onExport, onDelete }: {
  profile: AIProfile
  readOnly: boolean
  onEdit: () => void
  onExport: () => void
  onDelete: () => void
}) {
  const hosted = profile.source === 'hosted'
  const locked = readOnly || !!profile.read_only || hosted
  const targets: ProbeTarget[] = [
    { purpose: 'llm', label: '对话', model: profile.llm_model, base_url: profile.llm_base_url, provider: profile.llm_provider, api_key: '', profile_id: profile.id },
    { purpose: 'asr', label: '语音识别', model: profile.asr_model, base_url: profile.asr_base_url, provider: profile.asr_provider, api_key: '', profile_id: profile.id },
    { purpose: 'embedding', label: '向量', model: profile.embedding_model, base_url: profile.embedding_endpoint, provider: profile.embedding_provider, api_key: '', profile_id: profile.id, embedding_dim: profile.embedding_dim },
    ...(profile.vision_model ? [{ purpose: 'vision' as const, label: '视觉', model: profile.vision_model, base_url: profile.vision_base_url, provider: profile.vision_provider, api_key: '', profile_id: profile.id }] : []),
  ]
  return (
    <div className="profile-card">
      <div className="profile-icon"><Icon name="cpu" /></div>
      <div className="pc-body">
        <div className="t">
          <b>{profile.name}</b>
          {profile.is_default && (
            <span className="chip chip-acc"><Icon name="check" size="sm" />默认</span>
          )}
          {hosted && <span className="chip chip-info">作者免费提供</span>}
          {!hosted && profile.read_only && <span className="chip chip-info">只读配置</span>}
        </div>
        <CapabilityLine label="对话模型" value={profile.llm_model} />
        <CapabilityLine label="上下文窗口" value={profile.llm_context_tokens ? `${profile.llm_context_tokens.toLocaleString()} token` : '未填写 · 按保守预算'} muted={!profile.llm_context_tokens} />
        <CapabilityLine label="语音识别" value={profile.asr_model} />
        <CapabilityLine label="向量模型" value={`${profile.embedding_model} · ${profile.embedding_dim} 维`} />
        <CapabilityLine label="视觉模型" value={profile.vision_model || '未配置'} muted={!profile.vision_model} />
        <CapabilityLine label="重排序" value={profile.rerank_model || '未启用 · 确定性 rerank 生效中'} muted={!profile.rerank_model} />
        {!locked && <CapabilityProbe targets={targets} />}
      </div>
      {!hosted && <><button className="btn btn-sm" disabled={locked} onClick={onExport} title="不含 API Key">导出</button>
      <button
        className="btn btn-sm btn-ghost"
        disabled={locked}
        title={hosted ? '平台内置配置不可修改' : readOnly ? '演示账号不可修改 AI 配置' : undefined}
        onClick={onEdit}
        aria-label={`编辑 ${profile.name}`}
      >
        <Icon name="pencil" size="sm" />
      </button>
      <button
        className="btn btn-sm btn-ghost"
        disabled={locked}
        title={hosted ? '平台内置配置不可删除' : readOnly ? '演示账号不可删除 AI 配置' : undefined}
        onClick={onDelete}
        aria-label={`删除 ${profile.name}`}
      >
        <Icon name="trash" size="sm" />
      </button>
      </>}
    </div>
  )
}

function CapabilityLine({ label, value, muted }: { label: string; value: string; muted?: boolean }) {
  return (
    <div className="pc-line">
      <span className="k">{label}</span>
      <span className="v" style={muted ? { color: 'var(--tx-4)' } : undefined}>{value}</span>
    </div>
  )
}
