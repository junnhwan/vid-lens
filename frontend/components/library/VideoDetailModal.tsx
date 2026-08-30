'use client'

import Link from 'next/link'
import { useEffect, useMemo, useState } from 'react'
import {
  X, MessageCircle, BookOpen, Layers, Sparkles, FileText, Play, Copy, RefreshCw,
  Download, Search, Database, AlertTriangle, Loader2, RotateCw, Trash2,
} from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { TaskStatusEnum } from '@/lib/types'
import { statusLabel, computePhases, sourceLabel, taskTitle, fmtRelTime } from '@/lib/format'
import { VideoThumb, thumbGradient } from '@/components/library/VideoMedia'
import Markdown from '@/components/Markdown'
import { api, ApiError } from '@/lib/api'
import { useToast } from '@/components/Toast'
import { useRole } from '@/lib/useRole'

type Tab = 'summary' | 'transcript' | 'progress'

export default function VideoDetailModal({ task, loading, onClose, onChanged }: {
  task: VideoTask | null
  loading?: boolean
  onClose: () => void
  onChanged?: () => void
}) {
  const toast = useToast()
  const { isDemo } = useRole()
  const [tab, setTab] = useState<Tab>('summary')
  const [search, setSearch] = useState('')
  const [ragStatus, setRagStatus] = useState<{ indexed: boolean; chunks: number } | null>(null)
  const [triggering, setTriggering] = useState(false)
  const [regening, setRegening] = useState(false)
  const [retrying, setRetrying] = useState(false)
  const [starting, setStarting] = useState(false)
  const [deleting, setDeleting] = useState(false)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  useEffect(() => {
    if (!task) return
    setTab(task.summary?.content ? 'summary' : task.transcription?.content ? 'transcript' : 'progress')
    setSearch('')
    setRagStatus(null)
    api.getRagIndex(task.id).then(r => setRagStatus({ indexed: r.indexed, chunks: r.chunks })).catch(() => {})
  }, [task?.id])

  const transcriptParas = useMemo(() => {
    const c = task?.transcription?.content
    if (!c) return []
    return c.split(/\n+/).filter(s => s.trim())
  }, [task?.transcription?.content])

  if (!task && !loading) return null

  const phases = task ? computePhases(task) : []
  const canChat = task?.has_transcription
  const canStartTranscribe = task && !isDemo && task.status === TaskStatusEnum.Pending && !task.has_transcription
  const isFailed = task?.status === TaskStatusEnum.Failed
  const retryJob = task?.last_job_type
  const canRetry = task && !isDemo && isFailed && retryJob !== 'download'

  const tabs: { key: Tab; label: string; icon: React.ReactNode; show: boolean }[] = [
    { key: 'summary', label: 'AI 摘要', icon: <Sparkles className="w-3.5 h-3.5" />, show: !!(task?.summary?.content || task?.has_summary) },
    { key: 'transcript', label: '转写全文', icon: <FileText className="w-3.5 h-3.5" />, show: !!(task?.transcription?.content || task?.has_transcription) },
    { key: 'progress', label: '处理进度', icon: <Layers className="w-3.5 h-3.5" />, show: true },
  ]

  const copySummary = async () => {
    if (!task?.summary?.content) return
    try { await navigator.clipboard.writeText(task.summary.content); toast.success('摘要已复制') }
    catch { toast.error('复制失败') }
  }

  const genSummary = async (force = false) => {
    if (!task || regening) return
    setRegening(true)
    try {
      await api.analyze(task.id, force)
      toast.success(force ? '已提交重新生成' : '已提交摘要生成')
      onChanged?.()
    } catch (e) { toast.error(e instanceof ApiError ? e.message : '操作失败') }
    finally { setRegening(false) }
  }

  const exportTranscript = () => {
    if (!task?.transcription?.content) return
    const name = taskTitle(task).replace(/[\\/:*?"<>|]/g, '_')
    const blob = new Blob([task.transcription.content], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url; a.download = `${name}.txt`; a.click()
    URL.revokeObjectURL(url)
    toast.success('转写已导出')
  }

  const triggerIndex = async () => {
    if (!task) return
    setTriggering(true)
    try {
      const r = await api.triggerRagIndex(task.id)
      setRagStatus({ indexed: r.indexed, chunks: r.chunks })
      toast.success('索引任务已提交')
    } catch (e) { toast.error(e instanceof ApiError ? e.message : '触发失败') }
    finally { setTriggering(false) }
  }

  const startTranscribe = async () => {
    if (!task || starting) return
    setStarting(true)
    try {
      await api.transcribe(task.id)
      toast.success('已提交转写')
      onChanged?.()
    } catch (e) { toast.error(e instanceof ApiError ? e.message : '提交失败') }
    finally { setStarting(false) }
  }

  const retry = async () => {
    if (!task || retrying) return
    setRetrying(true)
    try {
      if (retryJob === 'analyze') await api.analyze(task.id)
      else if (retryJob === 'rag_index') await api.triggerRagIndex(task.id)
      else await api.transcribe(task.id)
      toast.success('已重新投递')
      onChanged?.()
    } catch (e) { toast.error(e instanceof ApiError ? e.message : '重试失败') }
    finally { setRetrying(false) }
  }

  const deleteTask = async () => {
    if (!task || deleting) return
    if (!window.confirm(`确认删除「${taskTitle(task)}」？此操作不可撤销。`)) return
    setDeleting(true)
    try {
      await api.deleteTask(task.id)
      toast.success('视频已删除')
      onChanged?.()
      onClose()
    } catch (e) { toast.error(e instanceof ApiError ? e.message : '删除失败') }
    finally { setDeleting(false) }
  }

  return (
    <>
      <div className="fixed inset-0 bg-ink-0/40 z-40 backdrop-blur-sm ui-backdrop" onClick={onClose} />
      <div
        className="fixed inset-4 sm:inset-8 md:inset-y-10 md:inset-x-[10%] lg:inset-x-[15%] z-50 flex flex-col bg-paper-0 rounded-2xl border border-ink-0/10 shadow-2xl overflow-hidden ui-modal-in"
        role="dialog"
        aria-modal="true"
      >
        {loading || !task ? (
          <div className="flex-1 flex items-center justify-center text-ink-4 text-[14px]">
            <Loader2 className="w-5 h-5 animate-spin mr-2" />加载任务详情…
          </div>
        ) : (
          <>
            <div className="shrink-0 flex border-b border-ink-0/8">
              <div className={`w-48 sm:w-56 shrink-0 relative bg-gradient-to-br ${thumbGradient(task.id)}`}>
                <div className="absolute inset-0 bg-ink-0/25" />
                <div className="absolute inset-0 flex items-center justify-center">
                  <div className="w-12 h-12 rounded-md bg-paper-0/20 backdrop-blur border border-paper-0/25 flex items-center justify-center">
                    <Play className="w-5 h-5 text-paper-0 ml-0.5" fill="currentColor" />
                  </div>
                </div>
              </div>
              <div className="flex-1 min-w-0 px-5 py-4 flex items-start justify-between gap-4">
                <div className="min-w-0">
                  <p className="text-[12px] text-ink-4">视频知识，不是播放器</p>
                  <h2 className="text-[20px] sm:text-[22px] font-semibold text-ink-0 ui-serif truncate mt-0.5">{taskTitle(task)}</h2>
                  <p className="text-[12px] text-ink-3 mt-1">{statusLabel(task.status)} · {sourceLabel(task)} · #{task.id}</p>
                </div>
                <button onClick={onClose} className="w-9 h-9 rounded-lg border border-ink-0/10 bg-paper-0 flex items-center justify-center text-ink-4 hover:text-ink-1 hover:bg-paper-1 shrink-0 transition-colors">
                  <X className="w-4 h-4" />
                </button>
              </div>
            </div>

            <div className="shrink-0 flex gap-1 px-5 pt-3 border-b border-ink-0/8 bg-paper-1/60 overflow-x-auto">
              {tabs.filter(t => t.show).map(t => (
                <button
                  key={t.key}
                  onClick={() => setTab(t.key)}
                  className={`flex items-center gap-1.5 px-4 py-2.5 text-[13px] border-b-2 -mb-px whitespace-nowrap transition-colors ${
                    tab === t.key ? 'border-sienna-500 text-ink-0 font-medium' : 'border-transparent text-ink-4 hover:text-ink-2'
                  }`}
                >
                  {t.icon}{t.label}
                </button>
              ))}
            </div>

            <div className="flex-1 min-h-0 overflow-y-auto px-6 py-5 scroll-thin">
              {tab === 'summary' && (
                <div className="max-w-3xl ui-fade-in">
                  <div className="flex items-center gap-2 mb-3">
                    {task.summary?.model_name && <span className="text-[11px] text-ink-4 font-mono">{task.summary.model_name}</span>}
                    <div className="ml-auto flex gap-2">
                      {!isDemo && task.has_transcription && !task.has_summary && (
                        <button onClick={() => genSummary(false)} disabled={regening} className="text-[11px] text-sienna-700 flex items-center gap-1 disabled:opacity-50">
                          {regening ? <Loader2 className="w-3 h-3 animate-spin" /> : <Sparkles className="w-3 h-3" />}生成摘要
                        </button>
                      )}
                      <button onClick={copySummary} disabled={!task.summary?.content} className="text-[11px] text-ink-3 flex items-center gap-1 disabled:opacity-40">
                        <Copy className="w-3 h-3" />复制
                      </button>
                      {!isDemo && task.has_summary && (
                        <button onClick={() => genSummary(true)} disabled={regening} className="text-[11px] text-ink-3 flex items-center gap-1 disabled:opacity-50">
                          {regening ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}重新生成
                        </button>
                      )}
                    </div>
                  </div>
                  {task.summary?.content ? (
                    <div className="rounded-xl border border-ink-0/8 bg-paper-0 p-5 sm:p-6">
                      <Markdown content={task.summary.content} />
                    </div>
                  ) : task.has_summary ? (
                    <div className="space-y-2"><div className="sk h-4 w-3/4" /><div className="sk h-4 w-1/2" /></div>
                  ) : (
                    <p className="text-[13px] text-ink-3 italic py-8 text-center">摘要尚未生成</p>
                  )}
                </div>
              )}

              {tab === 'transcript' && (
                <div className="max-w-3xl ui-fade-in">
                  <div className="flex items-center gap-2 mb-3">
                    <div className="relative flex-1 max-w-xs">
                      <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-ink-4" />
                      <input
                        value={search}
                        onChange={e => setSearch(e.target.value)}
                        placeholder="检索转写…"
                        className="w-full h-8 pl-8 pr-2 rounded-lg border border-ink-0/8 bg-paper-0 text-[12px]"
                      />
                    </div>
                    <button onClick={exportTranscript} disabled={!task.transcription?.content} className="text-[11px] text-ink-3 flex items-center gap-1 disabled:opacity-40">
                      <Download className="w-3 h-3" />导出
                    </button>
                  </div>
                  {task.transcription?.content ? (
                    <ol className="rounded-xl border border-ink-0/8 bg-paper-0 divide-y divide-stone-100">
                      {transcriptParas.map((p, i) => (
                        <TranscriptRow key={i} index={i + 1} text={p} q={search} />
                      ))}
                    </ol>
                  ) : task.has_transcription ? (
                    <div className="space-y-2"><div className="sk h-4 w-full" /><div className="sk h-4 w-5/6" /></div>
                  ) : (
                    <p className="text-[13px] text-ink-3 italic py-8 text-center">转写尚未生成</p>
                  )}
                  {task.transcription && (
                    <p className="text-[11px] text-ink-4 mt-3">{task.transcription.words.toLocaleString()} 词 · {transcriptParas.length} 段</p>
                  )}
                </div>
              )}

              {tab === 'progress' && (
                <div className="max-w-lg space-y-5 ui-fade-in">
                  {(task.error_msg || task.last_error_msg) && (
                    <div className="flex gap-3 p-4 rounded-xl bg-red-50 border border-red-200">
                      <AlertTriangle className="w-5 h-5 text-red-600 shrink-0" />
                      <p className="text-[13px] text-red-800">{task.error_msg || task.last_error_msg}</p>
                    </div>
                  )}
                  <div className="space-y-3">
                    {phases.map(p => (
                      <div key={p.label}>
                        <div className="flex justify-between text-[12px] mb-1">
                          <span className="text-ink-2">{p.label}</span>
                          <span className={p.state === 'done' ? 'text-moss' : p.state === 'running' ? 'text-sienna-700' : 'text-ink-4'}>
                            {p.state === 'done' ? '完成' : p.state === 'running' ? '处理中' : '排队'}
                          </span>
                        </div>
                        <div className="h-1.5 rounded-full bg-paper-2 overflow-hidden">
                          <div className={`h-full rounded-full ${p.state === 'done' ? 'bg-moss' : p.state === 'running' ? 'flow' : 'bg-paper-3'}`}
                            style={{ width: p.state === 'queued' ? '5%' : '100%' }} />
                        </div>
                      </div>
                    ))}
                  </div>
                  <div>
                    <div className="text-[10px] uppercase tracking-wider text-ink-4 mb-2">RAG 索引</div>
                    {ragStatus ? (
                      <div className="flex items-center gap-2 text-[13px]">
                        {ragStatus.indexed
                          ? <span className="text-moss">已索引 · {ragStatus.chunks} chunks</span>
                          : <>
                              <span className="text-ink-3">未索引</span>
                              {!isDemo && (
                                <button onClick={triggerIndex} disabled={triggering} className="h-7 px-2.5 rounded-lg border border-ink-0/10 text-[11px] flex items-center gap-1 disabled:opacity-50">
                                  <Database className="w-3 h-3" />{triggering ? '触发中' : '触发索引'}
                                </button>
                              )}
                            </>}
                      </div>
                    ) : <div className="sk h-4 w-32" />}
                  </div>
                  <dl className="text-[12px] space-y-1.5 pt-2 border-t border-ink-0/8">
                    <div className="flex justify-between"><dt className="text-ink-4">创建</dt><dd>{fmtRelTime(task.created_at)}</dd></div>
                    {task.finished_at && <div className="flex justify-between"><dt className="text-ink-4">完成</dt><dd>{fmtRelTime(task.finished_at)}</dd></div>}
                  </dl>
                  <div className="flex flex-wrap gap-2 pt-2">
                    {canStartTranscribe && (
                      <button onClick={startTranscribe} disabled={starting} className="h-9 px-4 rounded-lg bg-ink-0 text-paper-0 text-[12px] flex items-center gap-1.5 disabled:opacity-50">
                        {starting ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />}开始转写
                      </button>
                    )}
                    {canRetry && (
                      <button onClick={retry} disabled={retrying} className="h-9 px-4 rounded-lg border border-ink-0/10 text-[12px] flex items-center gap-1.5 disabled:opacity-50">
                        {retrying ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RotateCw className="w-3.5 h-3.5" />}重试
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>

            <div className="shrink-0 px-6 py-4 border-t border-ink-0/8 bg-paper-0 flex flex-wrap items-center gap-3">
              {canChat ? (
                <Link href={`/chat/${task.id}`} className="h-10 px-5 rounded-lg bg-ink-0 text-paper-0 text-[13px] font-medium inline-flex items-center gap-2 ui-btn-lift">
                  <MessageCircle className="w-4 h-4" />开始引用式问答
                </Link>
              ) : (
                <span className="text-[12px] text-ink-3">转写完成后可问答</span>
              )}
              <Link href="/kb" className="h-10 px-4 rounded-lg text-ink-3 text-[13px] inline-flex items-center gap-1.5 hover:text-ink-1 ui-btn-lift">
                <BookOpen className="w-3.5 h-3.5" />知识库
              </Link>
              {!isDemo && (
                <button
                  onClick={deleteTask}
                  disabled={deleting}
                  className="h-10 px-4 rounded-lg border border-rust/25 text-rust text-[13px] inline-flex items-center gap-1.5 ui-btn-lift disabled:opacity-50"
                >
                  {deleting ? <Loader2 className="w-4 h-4 animate-spin" /> : <Trash2 className="w-4 h-4" />}
                  删除视频
                </button>
              )}
              <button onClick={onClose} className="ml-auto text-[12px] text-ink-4 hover:text-ink-2">Esc 关闭</button>
            </div>
          </>
        )}
      </div>
    </>
  )
}

function TranscriptRow({ index, text, q }: { index: number; text: string; q: string }) {
  const hit = q.trim() && text.toLowerCase().includes(q.trim().toLowerCase())
  return (
    <li className={`flex gap-3 px-4 py-3 ${hit ? 'hit' : ''}`}>
      <span className="w-7 shrink-0 text-right font-mono text-[10px] text-ink-4 pt-1">{String(index).padStart(2, '0')}</span>
      <p className="text-[14px] leading-relaxed text-ink-2">{highlight(text, q)}</p>
    </li>
  )
}

function highlight(text: string, q: string) {
  if (!q.trim()) return text
  const kw = q.trim()
  const idx = text.toLowerCase().indexOf(kw.toLowerCase())
  if (idx < 0) return text
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-sienna-500/20 px-0.5 rounded">{text.slice(idx, idx + kw.length)}</mark>
      {text.slice(idx + kw.length)}
    </>
  )
}
