'use client'

import { useEffect, useState } from 'react'
import { useRouter } from 'next/navigation'
import { api, ApiError } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { fmtRelTime, indexStatusText } from '@/lib/format'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { useToast } from '@/components/Toast'
import { Icon } from '@/components/ui/Icon'
import { ConfirmModal, Modal } from '@/components/ui/Modal'
import KBModal from '@/components/KBModal'

export default function KBDetailPage({ params }: { params: { id: string } }) {
  const kbId = Number(params.id)
  const router = useRouter()
  const toast = useToast()
  const { user } = useShell()
  const readOnly = user?.role === 'DEMO'

  const [kb, setKb] = useState<KnowledgeBase | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [manageOpen, setManageOpen] = useState(false)
  const [editOpen, setEditOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [editName, setEditName] = useState('')
  const [editDesc, setEditDesc] = useState('')

  useCrumb([
    { label: '知识库', href: '/kb' },
    { label: kb?.name || `知识库 #${kbId}` },
  ])

  const reload = async () => {
    const detail = await api.getKB(kbId)
    setKb(detail)
    return detail
  }

  useEffect(() => {
    let active = true
    setLoading(true)
    setLoadError('')
    api.getKB(kbId)
      .then(detail => { if (active) setKb(detail) })
      .catch(e => { if (active) setLoadError(e instanceof ApiError ? e.message : '知识库加载失败') })
      .finally(() => { if (active) setLoading(false) })
    return () => { active = false }
  }, [kbId])

  const saveMeta = async () => {
    if (!editName.trim()) { toast.info('名称不能为空'); return }
    setBusy(true)
    try {
      const updated = await api.updateKB(kbId, editName.trim(), editDesc.trim())
      setKb(prev => prev ? { ...prev, name: updated.name, description: updated.description } : updated)
      setEditOpen(false)
      toast.success('已保存')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const removeVideo = async (taskId: number) => {
    if (readOnly) return
    setBusy(true)
    try {
      await api.removeKBVideo(kbId, taskId)
      toast.success('已移出知识库')
      await reload()
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '移除失败')
    } finally {
      setBusy(false)
    }
  }

  const destroy = async () => {
    setBusy(true)
    try {
      await api.deleteKB(kbId)
      toast.success('知识库已删除')
      router.replace('/kb')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '删除失败')
      setBusy(false)
    }
  }

  if (loading) {
    return <div className="page"><div className="empty"><b>加载中…</b></div></div>
  }
  if (loadError || !kb) {
    return (
      <div className="page">
        <div className="card">
          <div className="empty">
            <Icon name="alert" size="lg" />
            <b>知识库加载失败</b>
            <p>{loadError || '知识库不存在'}</p>
            <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => router.push('/kb')}>
              <Icon name="chev-l" size="sm" />返回知识库
            </button>
          </div>
        </div>
      </div>
    )
  }

  const videos = kb.videos || []

  return (
    <div className="page">
      <div className="kb-detail-head">
        <div className="kb-icon" style={{ background: 'var(--acc-dim)', color: 'var(--acc-strong)', borderColor: 'var(--acc-line)' }}>
          <Icon name="folder" />
        </div>
        <div className="kb-detail-copy">
          <div className="ws-heading">
            <h2>{kb.name}</h2>
            {!readOnly && (
              <button
                className="btn btn-ic btn-ghost"
                aria-label="编辑名称和描述"
                onClick={() => { setEditName(kb.name); setEditDesc(kb.description || ''); setEditOpen(true) }}
              >
                <Icon name="pencil" size="sm" />
              </button>
            )}
          </div>
          {kb.description && <p className="kb-detail-desc">{kb.description}</p>}
          <p className="kb-detail-meta">
            {kb.member_count} 个视频
            {kb.embedding_model ? ` · ${kb.embedding_model}` : ''}
            {` · ${fmtRelTime(kb.updated_at)}更新`}
          </p>
        </div>
      </div>

      <div className="ws-actions" style={{ marginTop: 16 }}>
        <button className="btn" disabled={readOnly} title={readOnly ? '演示账号不可修改知识库' : undefined} onClick={() => setManageOpen(true)}>
          <Icon name="plus" size="sm" />加入视频
        </button>
        {!readOnly && (
          <button className="btn btn-ghost" onClick={() => setDeleteOpen(true)}>
            <Icon name="trash" size="sm" />删除知识库
          </button>
        )}
        <span style={{ flex: 1 }} />
        <button className="btn btn-primary" onClick={() => router.push(`/chat/kb/${kb.id}`)}>
          <Icon name="message" size="sm" />进入问答
        </button>
      </div>

      <div className="section-head">
        <h2>成员视频</h2>
      </div>
      {videos.length === 0 ? (
        <div className="card">
          <div className="empty">
            <Icon name="video" size="lg" />
            <b>还没有成员视频</b>
            <p>只有已建立检索索引的视频才能加入知识库。</p>
            {!readOnly && (
              <button className="btn btn-sm btn-primary" onClick={() => setManageOpen(true)}>
                <Icon name="plus" size="sm" />加入视频
              </button>
            )}
          </div>
        </div>
      ) : (
        <div className="card" style={{ padding: 8 }}>
          {videos.map(v => (
            <div key={v.task_id} className="kb-member-row" onClick={() => router.push(`/video/${v.task_id}`)}>
              <span className="vt" />
              <span className="nm">
                <b>{v.title || `任务 #${v.task_id}`}</b>
                <span className="mono">{indexStatusText(v.index_status, v.retrievable)}</span>
              </span>
              <button className="btn btn-sm btn-ghost" onClick={e => { e.stopPropagation(); router.push(`/chat/v/${v.task_id}`) }}>单独提问</button>
              {!readOnly && (
                <button
                  className="btn btn-sm"
                  disabled={busy}
                  onClick={e => { e.stopPropagation(); void removeVideo(v.task_id) }}
                >
                  移出
                </button>
              )}
            </div>
          ))}
        </div>
      )}

      {manageOpen && (
        <KBModal
          mode="manage"
          kb={kb}
          onClose={() => setManageOpen(false)}
          onChanged={() => { void reload() }}
        />
      )}

      {editOpen && (
        <Modal
          title="编辑知识库"
          onClose={() => setEditOpen(false)}
          confirmOnClose={editName !== kb.name || editDesc !== (kb.description || '')}
          width={460}
          footer={(
            <>
              <button className="btn" onClick={() => setEditOpen(false)}>取消</button>
              <button className="btn btn-primary" disabled={busy} onClick={() => void saveMeta()}>保存</button>
            </>
          )}
        >
          <label className="field-label">名称</label>
          <input className="input" value={editName} onChange={e => setEditName(e.target.value)} autoFocus />
          <label className="field-label" style={{ marginTop: 12 }}>描述(可选)</label>
          <input className="input" value={editDesc} onChange={e => setEditDesc(e.target.value)} />
        </Modal>
      )}

      {deleteOpen && (
        <ConfirmModal
          title="删除知识库?"
          danger
          confirmLabel="删除"
          busy={busy}
          onClose={() => setDeleteOpen(false)}
          onConfirm={() => void destroy()}
        >
          删除「{kb.name}」后,库内问答会话也会被清掉,视频本身仍留在视频库。此操作不可恢复。
        </ConfirmModal>
      )}
    </div>
  )
}
