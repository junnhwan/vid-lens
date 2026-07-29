'use client'

import { useState, useEffect, useMemo } from 'react'
import Link from 'next/link'
import { ChevronsRight, X, ArrowRight, Check, Copy, RefreshCw, Search, Download, AlertTriangle, Database, PanelRightClose, Columns2, Square, Loader2 } from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { TaskStatusEnum } from '@/lib/types'
import { api, ApiError } from '@/lib/api'
import { statusBadge, statusLabel, computePhases, fmtRelTime } from '@/lib/format'
import { useToast } from '@/components/Toast'
import Markdown from '@/components/Markdown'

type ViewMode = 'narrow' | 'expand' | 'fullscreen'

// 右侧详情面板，三态视图：
// narrow(380px) 只看进度；expand(58%) 摘要+转写上下两块；
// fullscreen(全屏) 摘要+转写左右并排各占 50%，空间翻倍。
export default function TaskDetailPanel({ task, onClose, viewMode, onViewMode }: {
  task: VideoTask | null
  onClose: () => void
  viewMode: ViewMode
  onViewMode: (m: ViewMode) => void
}) {
  const [search, setSearch] = useState('')
  const [ragStatus, setRagStatus] = useState<{ indexed: boolean; chunks: number } | null>(null)
  const [triggering, setTriggering] = useState(false)
  const [ragErr, setRagErr] = useState('')
  const [regening, setRegening] = useState(false)
  const toast = useToast()

  // Copy 摘要到剪贴板
  const copySummary = async () => {
    if (!task?.summary?.content) return
    try {
      await navigator.clipboard.writeText(task.summary.content)
      toast.success('摘要已复制')
    } catch {
      toast.error('复制失败，请手动选择')
    }
  }

  // Regen 摘要：强制重新分析
  const regenSummary = async () => {
    if (!task || regening) return
    setRegening(true)
    try {
      await api.analyze(task.id, true)
      toast.success('已提交重新生成，稍后刷新可见')
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : '重新生成失败')
    } finally {
      setRegening(false)
    }
  }

  // Export 转写全文为 .txt 下载
  const exportTranscript = () => {
    if (!task?.transcription?.content) return
    const name = (task.title || task.filename || `task-${task.id}`).replace(/[\\/:*?"<>|]/g, '_')
    const blob = new Blob([task.transcription.content], { type: 'text/plain;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${name}.txt`
    a.click()
    URL.revokeObjectURL(url)
    toast.success('转写已导出')
  }

  // 拉取 RAG 索引状态（task?.id 变化时）
  useEffect(() => {
    if (!task) return
    setRagStatus(null); setRagErr('')
    api.getRagIndex(task.id).then(r => setRagStatus({ indexed: r.indexed, chunks: r.chunks })).catch(() => {})
  }, [task?.id])

  const triggerIndex = async () => {
    if (!task) return
    setTriggering(true); setRagErr('')
    try {
      const r = await api.triggerRagIndex(task.id)
      setRagStatus({ indexed: r.indexed, chunks: r.chunks })
    } catch (e) {
      setRagErr(e instanceof ApiError ? e.message : '触发失败')
    } finally { setTriggering(false) }
  }

  // 转写分段：按换行或长段落切，mockup 用 01/02 序号
  // 必须在早 return 之前调用，保证 hooks 顺序稳定
  const transcriptParas = useMemo(() => {
    const c = task?.transcription?.content
    if (!c) return []
    return c.split(/\n+/).filter(s => s.trim())
  }, [task?.transcription?.content])

  if (!task) return null
  const badge = statusBadge(task.status)
  const phases = computePhases(task)
  const isCompleted = task.status === TaskStatusEnum.Completed
  const expanded = viewMode !== 'narrow'
  const fullscreen = viewMode === 'fullscreen'

  // 转写分段已在上方 useMemo 计算（hooks 顺序要求）


  return (
    <aside className={`panel ${fullscreen ? 'w-full' : expanded ? 'w-[58%]' : 'w-[380px]'} shrink-0 bg-paper-0 border-l border-ink-0/15 flex flex-col min-h-0`}>
      {/* 卷宗头 */}
      <div className="px-7 py-4 border-b border-ink-0/15 flex items-center gap-3">
        <div className="text-[10px] font-medium text-ink-4">任务 #{task.id}</div>
        <h2 className="font-sans text-[18px] font-medium tight text-ink-0 flex-1 min-w-0 truncate">{task.title || task.filename}</h2>
        <span className={`inline-flex items-center gap-1.5 px-1.5 py-0.5 ${badge.cls} text-[10px] font-medium shrink-0`}>
          {badge.live && <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 live" />}
          {statusLabel(task.status)}
        </span>
        {isCompleted && (
          <Link href={`/chat/${task.id}`} className="btn-ink h-7 px-2.5 text-[10px] font-medium shrink-0 flex items-center gap-1">去问答 <ArrowRight className="w-3 h-3" /></Link>
        )}
        {/* 切换器贴面板右边缘：面板右贴边伸缩只动左边界，故此位置水平固定，连点不跑 */}
        <ViewModeToggle mode={viewMode} onChange={onViewMode} />
        <button onClick={onClose} className="w-7 h-7 flex items-center justify-center text-ink-3 hover:text-ink-0 shrink-0" title="关闭"><X className="w-4 h-4" /></button>
      </div>

      {/* 三阶段进度条 */}
      <div className="px-7 py-2 border-b border-ink-0/15 flex items-center gap-5 text-[10px] font-medium">
        {phases.map((p) => {
          const done = p.state === 'done'
          const running = p.state === 'running'
          return (
            <span key={p.label} className={`flex items-center gap-1.5 ${done ? 'text-moss' : running ? 'text-sienna-700' : 'text-ink-4'}`}>
              {done ? <Check className="w-3 h-3" /> : running ? <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 live" /> : <span className="w-1.5 h-1.5 rounded-full bg-ink-0/30" />}
              {p.label}
            </span>
          )
        })}
        {ragStatus && (
          <span className={`flex items-center gap-1.5 ${ragStatus.indexed ? 'text-moss' : 'text-ink-4'}`}>
            <Check className={`w-3 h-3 ${ragStatus.indexed ? '' : 'opacity-0'}`} />
            Index{ragStatus.indexed && ragStatus.chunks ? ` · ${ragStatus.chunks}` : ''}
          </span>
        )}
        <span className="ml-auto text-ink-4 normal-case" style={{ letterSpacing: 0 }}>
          {task.finished_at ? `完成于 ${fmtRelTime(task.finished_at)}` : task.created_at ? `创建于 ${fmtRelTime(task.created_at)}` : ''}
        </span>
      </div>

      {/* 窄态：只看进度 / 展开态：摘要 + 转写 */}
      {expanded ? (
        <div className={`flex-1 min-h-0 flex ${fullscreen ? 'flex-row' : 'flex-col'}`}>
          {/* 摘要 */}
          <section className={`${fullscreen ? 'w-1/2 border-r' : 'h-[40%]'} shrink-0 border-ink-0/15 flex flex-col min-h-0`}>
            <div className="px-7 pt-4 pb-2 flex items-center gap-3">
              <div className="text-[10px] font-medium text-ink-3">内容摘要</div>
              {task.summary?.model_name && <span className="font-mono text-[10px] text-ink-4">{task.summary.model_name}</span>}
              <div className="ml-auto flex gap-3">
                <button onClick={copySummary} disabled={!task.summary?.content} className="text-[10px] font-medium text-ink-3 hover:text-ink-0 flex items-center gap-1 disabled:opacity-40 disabled:cursor-not-allowed"><Copy className="w-3 h-3" />Copy</button>
                <button onClick={regenSummary} disabled={regening} className="text-[10px] font-medium text-ink-3 hover:text-ink-0 flex items-center gap-1 disabled:opacity-40 disabled:cursor-not-allowed">
                  {regening ? <Loader2 className="w-3 h-3 animate-spin" /> : <RefreshCw className="w-3 h-3" />}Regen
                </button>
              </div>
            </div>
            <div className="flex-1 overflow-y-auto scroll-thin px-7 pb-5">
              {task.summary?.content ? (
                <Markdown content={task.summary.content} />
              ) : task.has_summary ? (
                <div className="sk h-4 w-3/4 mb-3" />
              ) : (
                <EmptyPhase label="摘要尚未生成" />
              )}
            </div>
          </section>

          {/* 转写全文 */}
          <section className="flex-1 min-w-0 min-h-0 flex flex-col">
            <div className="px-7 pt-4 pb-2 flex items-center gap-3 border-b border-ink-0/15">
              <div className="text-[10px] font-medium text-ink-3">
                转写全文 {task.transcription ? <span className="text-ink-4 normal-case">{transcriptParas.length} 段</span> : null}
              </div>
              <div className="ml-auto flex items-center gap-2">
                <div className="relative">
                  <input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    type="search"
                    placeholder="检索全文…"
                    className="h-7 w-48 pl-7 pr-2 bg-paper-1 border-b border-ink-0/30 font-mono text-[11px] placeholder:text-ink-4 focus:outline-none focus:border-sienna-500"
                  />
                  <Search className="w-3.5 h-3.5 absolute left-0 top-1/2 -translate-y-1/2 text-ink-4" />
                </div>
                <button onClick={exportTranscript} disabled={!task.transcription?.content} className="text-[10px] font-medium text-ink-3 hover:text-ink-0 flex items-center gap-1 disabled:opacity-40 disabled:cursor-not-allowed"><Download className="w-3 h-3" />Export</button>
              </div>
            </div>
            {task.transcription?.content ? (
              <ol className="flex-1 overflow-y-auto scroll-thin px-7 py-2">
                {transcriptParas.map((p, i) => (
                  <TranscriptItem key={i} index={i + 1} text={p} q={search} />
                ))}
                <li className="py-3 text-center font-mono text-[10px] text-ink-4 wide uppercase">— 共 {transcriptParas.length} 段 —</li>
              </ol>
            ) : task.has_transcription ? (
              <div className="px-7 py-6 space-y-2">
                <div className="sk h-4 w-full" /><div className="sk h-4 w-5/6" /><div className="sk h-4 w-3/4" />
              </div>
            ) : (
              <div className="flex-1 flex items-center justify-center"><EmptyPhase label="转写尚未生成" /></div>
            )}
          </section>
        </div>
      ) : (
        /* 窄态：进度 + 元信息 + fail-closed 提示 */
        <div className="flex-1 overflow-y-auto scroll-thin px-7 py-5 space-y-5">
          {task.error_msg && (
            <div className="border border-rust/40 bg-rust/5 px-4 py-3 flex items-start gap-2.5">
              <AlertTriangle className="w-4 h-4 text-rust mt-0.5" />
              <div>
                <div className="font-sans text-[13px] font-medium text-rust">处理失败</div>
                <p className="font-sans text-[12px] text-ink-2 mt-1">{task.error_msg}</p>
              </div>
            </div>
          )}
          <div>
            <div className="text-[10px] font-medium text-ink-3 mb-3">三阶段进度</div>
            <div className="space-y-3">
              {phases.map((p) => (
                <div key={p.label}>
                  <div className="flex justify-between font-mono text-[10px] mb-1">
                    <span className="text-ink-4 wide uppercase">{p.label}</span>
                    <span className={p.state === 'done' ? 'text-moss' : p.state === 'running' ? 'text-sienna-700' : 'text-ink-4'}>
                      {p.state === 'done' ? '完成' : p.state === 'running' ? '处理中' : '排队'}
                    </span>
                  </div>
                  <div className="h-1 bg-ink-0/15 rounded-full overflow-hidden">
                    {/* running 全宽流光（不定态），done 全宽实色，queued 留 5% 灰头占位 —— 不再显示编造的百分比 */}
                    <div className={`h-full rounded-full ${p.state === 'done' ? 'bg-moss' : p.state === 'running' ? 'flow' : 'bg-ink-0/30'}`} style={{ width: p.state === 'done' ? '100%' : p.state === 'running' ? '100%' : '5%' }} />
                  </div>
                </div>
              ))}
            </div>
          </div>
          {/* RAG 索引状态 */}
          <div>
            <div className="text-[10px] font-medium text-ink-3 mb-2">RAG 索引</div>
            {ragStatus ? (
              <div className="flex items-center gap-2 text-[12px]">
                {ragStatus.indexed
                  ? <><span className="w-1.5 h-1.5 rounded-full bg-moss" /><span className="text-moss">已索引 · {ragStatus.chunks} chunks</span></>
                  : <><span className="w-1.5 h-1.5 rounded-full bg-ink-0/30" /><span className="text-ink-3">未索引</span>
                    <button onClick={triggerIndex} disabled={triggering} className="btn-line h-6 px-2 text-[10px] ml-2 flex items-center gap-1 disabled:opacity-50">
                      <Database className="w-3 h-3" />{triggering ? '触发中' : '触发索引'}
                    </button></>}
              </div>
            ) : <div className="sk h-4 w-32" />}
            {ragErr && <div className="text-[11px] text-rust mt-1">{ragErr}</div>}
          </div>
          {/* 元信息 */}
          <div className="border-t border-ink-0/15 pt-4">
            <dl className="font-mono text-[11px] text-ink-3 space-y-1.5 leading-relaxed">
              <div className="flex justify-between"><dt className="text-ink-4">来源</dt><dd className="text-ink-2">{task.source_type === 'url' ? 'URL 下载' : '本地上传'}</dd></div>
              <div className="flex justify-between"><dt className="text-ink-4">大小</dt><dd className="text-ink-2">{fmtSize2(task.file_size)}</dd></div>
              <div className="flex justify-between"><dt className="text-ink-4">MD5</dt><dd className="text-ink-2 truncate max-w-[180px]">{task.file_md5}</dd></div>
              {task.trace_id && <div className="flex justify-between"><dt className="text-ink-4">Trace</dt><dd className="text-ink-2 truncate max-w-[180px]">{task.trace_id}</dd></div>}
            </dl>
          </div>
          <button onClick={() => onViewMode('expand')} className="w-full btn-line h-8 text-[11px] font-medium flex items-center justify-center gap-1">
            展开查看摘要与转写 <ChevronsRight className="w-3.5 h-3.5" />
          </button>
        </div>
      )}
    </aside>
  )
}

// ============ 三态视图切换器：精致 segmented control ============
// narrow(380px 只看进度) / expand(58% 摘要+转写上下) / fullscreen(全屏 左右并排)
function ViewModeToggle({ mode, onChange }: { mode: ViewMode; onChange: (m: ViewMode) => void }) {
  const items: { m: ViewMode; icon: typeof PanelRightClose; label: string; title: string }[] = [
    { m: 'narrow', icon: PanelRightClose, label: '窄', title: '窄态：只看进度' },
    { m: 'expand', icon: Columns2, label: '半屏', title: '半屏：摘要 + 转写（上下）' },
    { m: 'fullscreen', icon: Square, label: '全屏', title: '全屏：摘要 + 转写（左右并排）' },
  ]
  return (
    <div className="flex items-center gap-0.5 p-0.5 rounded-md bg-paper-2 border border-ink-0/10 shrink-0">
      {items.map(({ m, icon: Icon, label, title }) => {
        const on = mode === m
        return (
          <button
            key={m}
            onClick={() => onChange(m)}
            title={title}
            className={`group flex items-center gap-1 h-6 px-2 rounded-[5px] text-[10px] font-medium transition-all
              ${on
                ? 'bg-paper-0 text-ink-0 shadow-[0_1px_2px_rgba(0,0,0,0.06),0_0_0_1px_rgba(0,0,0,0.04)]'
                : 'text-ink-4 hover:text-ink-2 hover:bg-paper-3/60'}`}
          >
            <Icon className={`w-3.5 h-3.5 transition-colors ${on ? 'text-sienna-500' : ''}`} strokeWidth={on ? 2 : 1.5} />
            <span className="wide">{label}</span>
          </button>
        )
      })}
    </div>
  )
}

function fmtSize2(b: number) {
  if (!b) return '0B'
  const u = ['B', 'K', 'M', 'G', 'T']
  let i = 0, n = b
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)}${u[i]}`
}

// 转写段：命中检索词加 hit 高亮
function TranscriptItem({ index, text, q }: { index: number; text: string; q: string }) {
  const hit = q && q.trim() && text.toLowerCase().includes(q.trim().toLowerCase())
  return (
    <li className={`flex gap-4 px-2 py-2 hover:bg-ink-0/[.03] ${hit ? 'hit' : ''}`}>
      <span className={`w-8 shrink-0 text-right font-mono text-[10px] ${hit ? 'text-sienna-700' : 'text-ink-4'} pt-1.5`}>{String(index).padStart(2, '0')}</span>
      <p className="font-sans text-[13.5px] leading-[1.75] text-ink-1">{highlight(text, q)}</p>
    </li>
  )
}

function highlight(text: string, q: string) {
  if (!q || !q.trim()) return text
  const kw = q.trim()
  const idx = text.toLowerCase().indexOf(kw.toLowerCase())
  if (idx < 0) return text
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-sienna-500/20 text-ink-0 px-0.5">{text.slice(idx, idx + kw.length)}</mark>
      {text.slice(idx + kw.length)}
    </>
  )
}

function EmptyPhase({ label }: { label: string }) {
  return (
    <div className="h-full flex items-center justify-center text-center">
      <div>
        <div className="font-mono text-[10px] text-ink-4 wide uppercase mb-1">— {label} —</div>
        <p className="text-[12px] text-ink-4">任务完成后自动生成</p>
      </div>
    </div>
  )
}
