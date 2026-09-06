'use client'

import { useState, useEffect } from 'react'
import { api, ApiError } from '@/lib/api'
import type { VideoTask, KnowledgeBase } from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { Modal } from '@/components/ui/Modal'

export default function KBModal({ mode, kb, taskId, indexed, onClose, onChanged }: {
  mode: 'create' | 'manage' | 'assign'
  kb?: KnowledgeBase
  /** assign 模式:要把当前视频加入哪些知识库 */
  taskId?: number
  /** assign 模式:当前视频是否已可检索;未索引时禁止加入 */
  indexed?: boolean
  onClose: () => void
  onChanged: () => void
}) {
  const [name, setName] = useState(kb?.name || '')
  const [description, setDescription] = useState(kb?.description || '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [tasks, setTasks] = useState<VideoTask[]>([])
  const [kbs, setKbs] = useState<KnowledgeBase[]>([])
  const [memberIds, setMemberIds] = useState<Set<number>>(new Set())
  const [filter, setFilter] = useState('')

  useEffect(() => {
    if (mode === 'manage' && kb) {
      setMemberIds(new Set((kb.videos || []).map(v => v.task_id)))
      api.listTasks(1, 200).then(r => setTasks(r.list || [])).catch(() => {})
    }
    if (mode === 'assign' && taskId) {
      void (async () => {
        try {
          const list = await api.listKBs()
          const details = await Promise.all(list.map(item => api.getKB(item.id).catch(() => item)))
          setKbs(details)
          setMemberIds(new Set(details.filter(item => (item.videos || []).some(v => v.task_id === taskId)).map(item => item.id)))
        } catch { /* 列表失败时弹窗仍可关闭 */ }
      })()
    }
  }, [mode, kb, taskId])

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

  const toggleMember = async (id: number) => {
    if (mode === 'manage') {
      if (!kb) return
      const isMember = memberIds.has(id)
      setMemberIds(prev => {
        const next = new Set(prev)
        if (isMember) next.delete(id); else next.add(id)
        return next
      })
      try {
        if (isMember) await api.removeKBVideo(kb.id, id)
        else await api.addKBVideo(kb.id, id)
        onChanged()
      } catch (e) {
        setMemberIds(prev => {
          const next = new Set(prev)
          if (isMember) next.add(id); else next.delete(id)
          return next
        })
        setErr(e instanceof ApiError ? e.message : '操作失败')
      }
      return
    }
    if (mode !== 'assign' || !taskId) return
    const isMember = memberIds.has(id)
    setMemberIds(prev => {
      const next = new Set(prev)
      if (isMember) next.delete(id); else next.add(id)
      return next
    })
    try {
      if (isMember) await api.removeKBVideo(id, taskId)
      else await api.addKBVideo(id, taskId)
      onChanged()
    } catch (e) {
      setMemberIds(prev => {
        const next = new Set(prev)
        if (isMember) next.add(id); else next.delete(id)
        return next
      })
      setErr(e instanceof ApiError ? e.message : '操作失败')
    }
  }

  const dirty = mode === 'create' && !!(name || description)
  const taskList = tasks.filter(t => !filter || taskTitle(t).toLowerCase().includes(filter.toLowerCase()))
  const kbList = kbs.filter(item => !filter || item.name.toLowerCase().includes(filter.toLowerCase()))

  return (
    <Modal
      title={mode === 'create' ? '新建知识库' : mode === 'manage' ? `加入视频 · ${kb?.name}` : '加入知识库'}
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
          {mode === 'manage' && (
            <p style={{ fontSize: 12.5, color: 'var(--tx-3)', margin: '0 0 10px' }}>
              只有已建立检索索引的视频才能加入。未索引的条目勾选后会被拒绝。
            </p>
          )}
          {mode === 'assign' && indexed === false && (
            <p style={{ fontSize: 12.5, color: 'var(--warn)', margin: '0 0 10px' }}>
              当前视频还没有检索索引,需要先在工作台建立索引才能加入知识库。
            </p>
          )}
          <input
            className="input"
            style={{ marginBottom: 10 }}
            placeholder={mode === 'manage' ? '按标题过滤…' : '按知识库名称过滤…'}
            value={filter}
            onChange={e => setFilter(e.target.value)}
          />
          <div style={{ maxHeight: 320, overflowY: 'auto' }}>
            {mode === 'manage' && (
              taskList.length === 0 ? (
                <div className="empty"><b>暂无可选视频</b></div>
              ) : taskList.map(t => {
                const on = memberIds.has(t.id)
                return (
                  <label key={t.id} className="kb-member-row" style={{ cursor: 'pointer' }}>
                    <input type="checkbox" checked={on} onChange={() => void toggleMember(t.id)} />
                    <span className="nm">
                      <b>{taskTitle(t)}</b>
                      <span>{t.has_transcription ? '已转写' : '无转写'}</span>
                    </span>
                  </label>
                )
              })
            )}
            {mode === 'assign' && (
              kbList.length === 0 ? (
                <div className="empty"><b>还没有知识库</b></div>
              ) : kbList.map(item => {
                const on = memberIds.has(item.id)
                const blocked = indexed === false && !on
                return (
                  <label key={item.id} className="kb-member-row" style={{ cursor: blocked ? 'not-allowed' : 'pointer', opacity: blocked ? 0.55 : 1 }}>
                    <input type="checkbox" checked={on} disabled={blocked} onChange={() => void toggleMember(item.id)} />
                    <span className="nm">
                      <b>{item.name}</b>
                      <span>{item.member_count} 个视频</span>
                    </span>
                  </label>
                )
              })
            )}
          </div>
          {err && <div className="login-err" style={{ marginTop: 10 }}>{err}</div>}
        </>
      )}
    </Modal>
  )
}
