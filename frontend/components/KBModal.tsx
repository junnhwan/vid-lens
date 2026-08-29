'use client'

import { useState, useEffect } from 'react'
import { X, Plus, Loader2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import type { VideoTask, KnowledgeBase } from '@/lib/types'

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

  useEffect(() => {
    const h = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [onClose])

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

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-stone-900/40 ui-backdrop" onClick={onClose}>
      <div className="bg-white border border-stone-200 rounded-xl w-full max-w-lg ui-modal-in shadow-xl" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center px-5 h-12 border-b border-stone-200">
          <div className="text-[14px] font-medium text-stone-900">{mode === 'create' ? '新建知识库' : `管理成员 · ${kb?.name}`}</div>
          <button onClick={onClose} className="ml-auto w-7 h-7 rounded-lg flex items-center justify-center text-stone-400 hover:text-stone-700 hover:bg-stone-100">
            <X className="w-4 h-4" />
          </button>
        </div>

        {mode === 'create' ? (
          <div className="p-5 space-y-4">
            <Field label="名称" value={name} onChange={setName} placeholder="分布式系统 · 核心知识库" autoFocus />
            <div>
              <label className="block text-[10px] uppercase tracking-wider text-stone-400 mb-1.5">描述</label>
              <textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                className="w-full h-20 px-3 py-2 rounded-lg border border-stone-200 bg-white text-[13px] resize-none focus:outline-none focus:ring-2 focus:ring-amber-600/20 focus:border-amber-400"
                placeholder="跨视频严格 RAG 问答用"
              />
            </div>
            <div className="flex items-center gap-2 pt-1">
              <button onClick={create} disabled={busy} className="h-8 px-4 rounded-lg bg-stone-900 text-white text-[11px] flex items-center gap-1.5 ui-btn-lift disabled:opacity-50">
                {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}创建
              </button>
              <span className="text-[10px] text-stone-400">创建后可添加多个视频做跨视频问答</span>
            </div>
            {err && <div className="text-[12px] text-red-600">{err}</div>}
          </div>
        ) : (
          <div className="p-5">
            <div className="text-[11px] text-stone-500 mb-2">勾选视频加入知识库（已完成转写/索引的视频可检索）</div>
            <div className="max-h-80 overflow-y-auto scroll-thin border border-stone-200 rounded-lg divide-y divide-stone-100">
              {tasks.length === 0 ? (
                <div className="py-8 text-center text-[12px] text-stone-400">暂无可选视频</div>
              ) : tasks.map(t => {
                const on = memberIds.has(t.id)
                return (
                  <label key={t.id} className={`flex items-center gap-3 px-3 py-2.5 cursor-pointer ui-row-hover ${on ? 'bg-amber-50/60' : ''}`}>
                    <input type="checkbox" checked={on} onChange={() => toggleMember(t.id)} className="accent-amber-600" />
                    <div className="min-w-0 flex-1">
                      <div className="text-[13px] text-stone-900 truncate">{t.title || t.filename}</div>
                      <div className="text-[10px] text-stone-400 font-mono">{t.source_type === 'url' ? 'URL' : '本地'} · {t.has_transcription ? '已转写' : '无转写'}</div>
                    </div>
                  </label>
                )
              })}
            </div>
            <div className="flex items-center gap-3 pt-3 mt-1">
              <span className="text-[10px] text-stone-400 font-mono">已选 {memberIds.size} 个视频</span>
              <button onClick={onClose} className="ml-auto h-8 px-3 rounded-lg border border-stone-200 text-[11px] ui-btn-lift hover:bg-stone-50">完成</button>
            </div>
            {err && <div className="text-[12px] text-red-600 mt-2">{err}</div>}
          </div>
        )}
      </div>
    </div>
  )
}

function Field({ label, value, onChange, placeholder, autoFocus }: {
  label: string; value: string; onChange: (v: string) => void; placeholder?: string; autoFocus?: boolean
}) {
  return (
    <div>
      <label className="block text-[10px] uppercase tracking-wider text-stone-400 mb-1.5">{label}</label>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        autoFocus={autoFocus}
        className="w-full h-10 px-3 rounded-lg border border-stone-200 bg-white text-[13px] focus:outline-none focus:ring-2 focus:ring-amber-600/20 focus:border-amber-400"
      />
    </div>
  )
}
