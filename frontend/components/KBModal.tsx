'use client'

import { useState, useEffect } from 'react'
import { api, ApiError } from '@/lib/api'
import type { VideoTask, KnowledgeBase } from '@/lib/types'
import { Modal } from '@/components/ui/Modal'

export default function KBModal({ mode, kb, onClose, onChanged }: {
  mode: 'create' | 'manage'
  kb?: KnowledgeBase
  onClose: () => void
  onChanged: () => void
}) {
  const [name, setName] = useState(kb?.name || '')
  const [description, setDescription] = useState(kb?.description || '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [memberIds, setMemberIds] = useState<Set<number>>(new Set())

  useEffect(() => {
    if (mode === 'manage' && kb) {
      setMemberIds(new Set((kb.videos || []).map(v => v.task_id)))
      api.listTasks(1, 200).then(r => setTasks(r.list || [])).catch(() => {})
    }
  }, [mode, kb])

  const create = async () => {
    if (!name.trim()) { setErr('请输入知识库名称'); return }
    setBusy(true); setErr('')
    try {
      await api.createKB(name.trim(), description.trim())
      onChanged()
      onClose()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : '创建失败')
    } finally { setBusy(false) }
  }

  const toggleMember = async (taskId: number) => {
    if (!kb) return
    const isMember = memberIds.has(taskId)
    setMemberIds(prev => {
      const next = new Set(prev)
      if (isMember) next.delete(taskId); else next.add(taskId)
      return next
    })
    try {
      if (isMember) await api.removeKBVideo(kb.id, taskId)
      else await api.addKBVideo(kb.id, taskId)
    } catch (e) {
      setMemberIds(prev => {
        const next = new Set(prev)
        if (isMember) next.add(taskId); else next.delete(taskId)
        return next
      })
      setErr(e instanceof ApiError ? e.message : '操作失败')
    }
  }

  const dirty = mode === 'create' && !!(name || description)

  return (
    <Modal
      title={mode === 'create' ? '新建知识库' : `管理成员 · ${kb?.name}`}
      onClose={onClose}
      confirmOnClose={dirty}
      width={520}
      footer={mode === 'create' ? (
        <>
          <button className="btn" onClick={onClose}>取消</button>
          <button className="btn btn-primary" disabled={busy} onClick={() => void create()}>创建</button>
        </>
      ) : (
        <button className="btn btn-primary" onClick={onClose}>完成</button>
      )}
    >
      {mode === 'create' ? (
        <>
          <label className="field-label">名称</label>
          <input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="名称" autoFocus />
          <label className="field-label" style={{ marginTop: 12 }}>描述</label>
          <textarea className="input" style={{ height: 80, padding: '10px 12px' }} value={description} onChange={e => setDescription(e.target.value)} />
          {err && <div className="login-err" style={{ marginTop: 10 }}>{err}</div>}
        </>
      ) : (
        <>
          <div style={{ maxHeight: 320, overflowY: 'auto' }}>
            {tasks.length === 0 ? (
              <div className="empty"><b>暂无可选视频</b></div>
            ) : tasks.map(t => {
              const on = memberIds.has(t.id)
              return (
                <label key={t.id} className="kb-member-row" style={{ cursor: 'pointer' }}>
                  <input type="checkbox" checked={on} onChange={() => void toggleMember(t.id)} />
                  <span className="nm">
                    <b>{t.title || t.filename}</b>
                    <span>{t.has_transcription ? '已转写' : '无转写'}</span>
                  </span>
                </label>
              )
            })}
          </div>
          {err && <div className="login-err" style={{ marginTop: 10 }}>{err}</div>}
        </>
      )}
    </Modal>
  )
}
