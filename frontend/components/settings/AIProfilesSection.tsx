'use client'

import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { AIProfile } from '@/lib/types'
import { Icon } from '@/components/ui/Icon'
import { useToast } from '@/components/Toast'
import { ProfileForm } from '@/components/settings/ProfileForm'

// BYOK AI 服务配置:profile 列表(一个 profile 覆盖 llm / asr / embedding / vision 四组能力),
// 外加 rerank 的真实状态(服务端为确定性 rerank,无 profile 配置项,按"未启用"如实呈现)。
// 每张卡支持 测试 / 编辑 / 删除;密钥只回显脱敏值。

export function AIProfilesSection({ readOnly }: { readOnly: boolean }) {
  const toast = useToast()
  const [profiles, setProfiles] = useState<AIProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [testingId, setTestingId] = useState<number | null>(null)
  const [editorOpen, setEditorOpen] = useState(false)
  const [editing, setEditing] = useState<AIProfile | undefined>(undefined)

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

  const testProfile = async (p: AIProfile) => {
    if (testingId != null) return
    setTestingId(p.id)
    try {
      await api.testProfile({ id: p.id })
      toast.success(`「${p.name}」连通性正常`)
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : `「${p.name}」测试失败`)
    } finally {
      setTestingId(null)
    }
  }

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
        onClose={() => setEditorOpen(false)}
        onSaved={() => void load()}
      />
    )
  }

  return (
    <>
      <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 14 }}>AI 服务</h3>
      {!readOnly && (
        <button
          className="btn btn-sm btn-primary"
          style={{ marginBottom: 16 }}
          onClick={() => { setEditing(undefined); setEditorOpen(true) }}
        >
          <Icon name="plus" size="sm" />新建配置
        </button>
      )}

      {loading && <div className="empty card"><p>正在加载 AI 配置…</p></div>}
      {!loading && loadError && (
        <div className="empty card">
          <Icon name="alert" size="lg" />
          <b>{loadError}</b>
          <button className="btn btn-sm" style={{ marginTop: 10 }} onClick={() => void load()}>
            <Icon name="refresh" size="sm" />重试
          </button>
        </div>
      )}
      {!loading && !loadError && profiles.length === 0 && (
        <div className="empty card">
          <Icon name="cpu" size="lg" />
          <b>还没有配置 AI 服务</b>
          {!readOnly && (
            <button className="btn btn-sm btn-primary" style={{ marginTop: 10 }} onClick={() => { setEditing(undefined); setEditorOpen(true) }}>
              <Icon name="plus" size="sm" />新建配置
            </button>
          )}
        </div>
      )}

      {profiles.map(p => (
        <ProfileCard
          key={p.id}
          profile={p}
          testing={testingId === p.id}
          readOnly={readOnly}
          onTest={() => void testProfile(p)}
          onEdit={() => { setEditing(p); setEditorOpen(true) }}
          onDelete={() => void removeProfile(p)}
        />
      ))}
    </>
  )
}

function ProfileCard({ profile, testing, readOnly, onTest, onEdit, onDelete }: {
  profile: AIProfile
  testing: boolean
  readOnly: boolean
  onTest: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  const hosted = profile.source === 'hosted' || profile.read_only
  return (
    <div className="profile-card">
      <div className="profile-icon"><Icon name="cpu" /></div>
      <div className="pc-body">
        <div className="t">
          <b>{profile.name}</b>
          {profile.is_default && (
            <span className="chip chip-acc"><Icon name="check" size="sm" />默认</span>
          )}
          {hosted && <span className="chip chip-info">平台内置</span>}
        </div>
        <CapabilityLine label="对话模型" value={profile.llm_model} />
        <CapabilityLine label="语音识别" value={profile.asr_model} />
        <CapabilityLine label="向量模型" value={`${profile.embedding_model} · ${profile.embedding_dim} 维`} />
        <CapabilityLine label="视觉模型" value={profile.vision_model || '未配置'} muted={!profile.vision_model} />
        <CapabilityLine label="重排序" value="未启用 · 确定性 rerank 生效中" muted />
      </div>
      <button className="btn btn-sm" disabled={testing || readOnly} title={readOnly ? '演示账号不可测试 AI 配置' : undefined} onClick={onTest}>
        <Icon name={testing ? 'clock' : 'bolt'} size="sm" />{testing ? '测试中…' : '测试'}
      </button>
      <button
        className="btn btn-sm btn-ghost"
        disabled={hosted || readOnly}
        title={hosted ? '平台内置配置不可修改' : readOnly ? '演示账号不可修改 AI 配置' : undefined}
        onClick={onEdit}
      >
        <Icon name="pencil" size="sm" />
      </button>
      <button
        className="btn btn-sm btn-ghost"
        disabled={hosted || readOnly}
        title={hosted ? '平台内置配置不可删除' : readOnly ? '演示账号不可删除 AI 配置' : undefined}
        onClick={onDelete}
      >
        <Icon name="trash" size="sm" />
      </button>
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
