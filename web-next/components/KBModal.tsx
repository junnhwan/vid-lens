'use client'

import { useState, useEffect } from 'react'
import { X, Plus, Loader2 } from 'lucide-react'
import { api, ApiError } from '@/lib/api'
import type { VideoTask, KnowledgeBase } from '@/lib/types'

// 知识库 modal：两个模式 —— create(新建) / manage(管理成员)。
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

  // 管理成员态：拉全部任务 + 当前 KB 成员
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
    // 乐观更新
    setMemberIds(prev => {
      const next = new Set(prev)
      if (isMember) next.delete(taskId); else next.add(taskId)
      return next
    })
    try {
      if (isMember) await api.removeKBVideo(kb.id, taskId)
      else await api.addKBVideo(kb.id, taskId)
    } catch (e) {
      // 回滚
      setMemberIds(prev => {
        const next = new Set(prev)
        if (isMember) next.add(taskId); else next.delete(taskId)
        return next
      })
      setErr(e instanceof ApiError ? e.message : '操作失败')
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-ink-0/40" onClick={onClose}>
      <div className="bg-paper-0 border border-ink-2/30 w-full max-w-lg" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center px-5 h-12 border-b border-ink-2/20">
          <div className="text-[14px] font-medium text-ink-0">{mode === 'create' ? '新建知识库' : `管理成员 · ${kb?.name}`}</div>
          <button onClick={onClose} className="ml-auto w-7 h-7 flex items-center justify-center text-ink-3 hover:text-ink-0"><X className="w-4 h-4" /></button>
        </div>

        {mode === 'create' ? (
          <div className="p-5 space-y-4">
            <div>
              <label className="block font-mono text-[10px] text-ink-3 mb-1.5">名称</label>
              <div className="field px-3 h-9 flex items-center">
                <input value={name} onChange={(e) => setName(e.target.value)} autoFocus className="w-full font-mono text-[12.5px] text-ink-1" placeholder="分布式系统 · 核心知识库" />
              </div>
            </div>
            <div>
              <label className="block font-mono text-[10px] text-ink-3 mb-1.5">描述</label>
              <div className="field px-3 h-20 flex items-start py-2">
                <textarea value={description} onChange={(e) => setDescription(e.target.value)} className="w-full font-sans text-[12.5px] text-ink-1 resize-none focus:outline-none" placeholder="跨视频严格 RAG 问答用" />
              </div>
            </div>
            <div className="flex items-center gap-2 pt-1">
              <button onClick={create} disabled={busy} className="btn-ink h-8 px-4 font-sans text-[11px] flex items-center gap-1.5 disabled:opacity-50">
                {busy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}创建
              </button>
              <span className="font-sans text-[10px] text-ink-4">创建后可添加多个视频做跨视频问答</span>
            </div>
            {err && <div className="text-[12px] text-rust">{err}</div>}
          </div>
        ) : (
          <div className="p-5">
            <div className="font-sans text-[11px] text-ink-3 mb-2">勾选视频加入知识库（已完成转写/索引的视频可检索）</div>
            <div className="max-h-80 overflow-y-auto scroll-thin border border-ink-2/15 divide-y divide-ink-0/10">
              {tasks.length === 0 ? (
                <div className="py-8 text-center text-[12px] text-ink-4">暂无可选视频</div>
              ) : tasks.map(t => {
                const on = memberIds.has(t.id)
                return (
                  <label key={t.id} className={`flex items-center gap-3 px-3 py-2.5 cursor-pointer ${on ? 'bg-sienna-500/5' : 'hover:bg-ink-0/[.03]'}`}>
                    <input type="checkbox" checked={on} onChange={() => toggleMember(t.id)} className="accent-sienna-500" />
                    <div className="min-w-0 flex-1">
                      <div className="font-sans text-[13px] text-ink-0 truncate">{t.title || t.filename}</div>
                      <div className="font-mono text-[10px] text-ink-4">{t.source_type === 'url' ? 'URL' : '本地'} · {t.has_transcription ? '已转写' : '无转写'}</div>
                    </div>
                  </label>
                )
              })}
            </div>
            <div className="flex items-center gap-3 pt-3 mt-1">
              <span className="font-mono text-[10px] text-ink-4">已选 {memberIds.size} 个视频</span>
              <button onClick={onClose} className="ml-auto btn-line h-8 px-3 font-sans text-[11px]">完成</button>
            </div>
            {err && <div className="text-[12px] text-rust mt-2">{err}</div>}
          </div>
        )}
      </div>
    </div>
  )
}
