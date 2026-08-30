'use client'

import Link from 'next/link'
import { useEffect, useState } from 'react'
import { X, MessageCircle, BookOpen, Layers, Sparkles, FileText, Play } from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { statusLabel, computePhases, sourceLabel } from '@/lib/format'
import { taskTitle } from '@/components/prototype/c/mocks'
import { enrichTaskForPrototype } from '@/components/prototype/c/mockContent'
import { VideoThumb, thumbGradient } from './VideoMedia'

interface Props {
  task: VideoTask
  onClose: () => void
}

type Tab = 'summary' | 'transcript' | 'progress'

/** 居中宽弹窗：封面 + Tab（摘要 / 转写 / 进度），适合阅读长文本 */
export function VideoDetailModal({ task: rawTask, onClose }: Props) {
  const task = enrichTaskForPrototype(rawTask, { demoFill: true })
  const phases = computePhases(task)
  const canChat = task.has_transcription
  const [tab, setTab] = useState<Tab>(task.summary?.content ? 'summary' : task.transcription?.content ? 'transcript' : 'progress')

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  const tabs: { key: Tab; label: string; icon: React.ReactNode; show: boolean }[] = [
    { key: 'summary', label: 'AI 摘要', icon: <Sparkles className="w-3.5 h-3.5" />, show: !!task.summary?.content },
    { key: 'transcript', label: '转写全文', icon: <FileText className="w-3.5 h-3.5" />, show: !!task.transcription?.content },
    { key: 'progress', label: '处理进度', icon: <Layers className="w-3.5 h-3.5" />, show: true },
  ]

  return (
    <>
      <div className="fixed inset-0 bg-ink-0/40 z-40 backdrop-blur-sm proto-backdrop" onClick={onClose} />
      <div
        className="fixed inset-4 sm:inset-8 md:inset-y-10 md:inset-x-[12%] lg:inset-x-[18%] z-50 flex flex-col bg-paper-0 rounded-2xl border border-ink-0/10 overflow-hidden proto-modal-in"
        style={{ boxShadow: 'var(--proto-shadow-lens)' }}
        role="dialog"
        aria-modal="true"
        aria-labelledby="video-detail-title"
      >
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
              <h2 id="video-detail-title" className="text-[20px] sm:text-[22px] font-semibold text-ink-0 leading-tight tracking-tight truncate mt-0.5">
                {taskTitle(task)}
              </h2>
              <p className="text-[12px] text-ink-3 mt-1 tabular-nums">
                {statusLabel(task.status)} · {sourceLabel(task)} · 任务 #{task.id}
              </p>
            </div>
            <button
              onClick={onClose}
              className="w-9 h-9 rounded-lg border border-ink-0/10 bg-paper-0 flex items-center justify-center text-ink-4 hover:text-ink-1 hover:bg-paper-1 shrink-0 transition-colors"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        <div className="shrink-0 flex gap-1 px-5 pt-3 border-b border-ink-0/8 bg-paper-1/60">
          {tabs.filter(t => t.show).map(t => (
            <button
              key={t.key}
              onClick={() => setTab(t.key)}
              className={`flex items-center gap-1.5 px-4 py-2.5 text-[13px] border-b-2 -mb-px transition-colors ${
                tab === t.key
                  ? 'border-sienna-500 text-ink-0 font-medium'
                  : 'border-transparent text-ink-4 hover:text-ink-2'
              }`}
            >
              {t.icon}{t.label}
            </button>
          ))}
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto px-6 py-5 bg-paper-1">
          {tab === 'summary' && task.summary?.content && (
            <article className="max-w-3xl proto-fade-in">
              <SummaryBody content={task.summary.content} />
              <p className="text-[11px] text-ink-5 mt-4">模型：{task.summary.model_name || '-'}</p>
            </article>
          )}

          {tab === 'transcript' && task.transcription?.content && (
            <article className="max-w-3xl proto-fade-in">
              <div className="rounded-xl bg-paper-0 p-5 sm:p-6">
                <pre className="text-[13px] sm:text-[14px] text-ink-2 leading-[1.85] whitespace-pre-wrap font-sans">
                  {task.transcription.content}
                </pre>
              </div>
              <p className="text-[11px] text-ink-5 mt-3 tabular-nums">
                共 {task.transcription.words.toLocaleString()} 词 · 转写全文（原型 mock 数据）
              </p>
            </article>
          )}

          {tab === 'progress' && (
            <div className="max-w-lg space-y-4 proto-fade-in">
              {phases.map(p => (
                <div key={p.label}>
                  <div className="flex justify-between text-[12px] mb-1.5">
                    <span className="text-ink-2">{p.label}</span>
                    <span className="text-ink-4 tabular-nums">{p.pct}%</span>
                  </div>
                  <div className="h-1.5 rounded-full bg-paper-3 overflow-hidden">
                    <div
                      className={`h-full rounded-full proto-progress-bar ${
                        p.state === 'done' ? 'bg-moss' : p.state === 'running' ? 'bg-sienna-500' : 'bg-paper-3'
                      }`}
                      style={{ width: `${p.pct}%` }}
                    />
                  </div>
                </div>
              ))}
              {!task.has_transcription && (
                <p className="text-[13px] text-ink-3 pt-2">转写完成后，摘要与全文会出现在上方 Tab 中。</p>
              )}
            </div>
          )}
        </div>

        <div className="shrink-0 px-6 py-4 border-t border-ink-0/8 bg-paper-0 flex flex-wrap items-center gap-3">
          {canChat ? (
            <Link
              href={`/chat/${task.id}`}
              className="h-10 px-5 rounded-lg bg-ink-0 text-paper-0 text-[13px] font-medium inline-flex items-center gap-2 proto-btn-lift"
            >
              <MessageCircle className="w-4 h-4" />开始引用式问答
            </Link>
          ) : (
            <span className="text-[12px] text-ink-3">转写完成后可问答</span>
          )}
          <Link
            href="/kb"
            className="h-10 px-4 rounded-lg text-ink-3 text-[13px] inline-flex items-center gap-1.5 hover:text-ink-1 proto-btn-lift"
          >
            <BookOpen className="w-3.5 h-3.5" />加入知识库
          </Link>
          <button onClick={onClose} className="ml-auto text-[12px] text-ink-4 hover:text-ink-2">
            Esc 关闭
          </button>
        </div>
      </div>
    </>
  )
}

/** @deprecated 使用 VideoDetailModal */
export const VideoDrawer = VideoDetailModal

function SummaryBody({ content }: { content: string }) {
  const lines = content.split('\n')
  return (
    <div className="rounded-xl bg-paper-0 p-5 sm:p-6 space-y-3">
      {lines.map((line, i) => {
        if (line.startsWith('## ')) {
          return <h3 key={i} className="text-[16px] font-semibold text-ink-0 tracking-tight pt-2 first:pt-0">{line.slice(3)}</h3>
        }
        if (line.startsWith('### ')) {
          return <h4 key={i} className="text-[14px] font-medium text-ink-1 mt-2">{line.slice(4)}</h4>
        }
        if (line.startsWith('- ')) {
          return <li key={i} className="text-[14px] text-ink-2 leading-relaxed ml-4 list-disc">{line.slice(2)}</li>
        }
        if (line.trim() === '') return <div key={i} className="h-1" />
        return <p key={i} className="text-[14px] text-ink-2 leading-relaxed">{line}</p>
      })}
    </div>
  )
}

/** 画廊条 / 网格用的小卡片 */
export function GalleryCard({ task, selected, onClick }: { task: VideoTask; selected?: boolean; onClick: () => void }) {
  const phases = computePhases(task)
  const running = phases.find(p => p.state === 'running')
  const enriched = enrichTaskForPrototype(task, { demoFill: true })
  const hasText = enriched.has_transcription || enriched.has_summary

  return (
    <button
      onClick={onClick}
      className={`group text-left rounded-xl overflow-hidden bg-paper-0 transition-all duration-200 proto-card-hover ${
        selected ? 'ring-2 ring-sienna-500/40' : 'ring-1 ring-ink-0/8 hover:ring-ink-0/16'
      }`}
    >
      <VideoThumb taskId={task.id} className="aspect-[16/10]" showPlay showStatus status={task.status} runningPct={running?.pct} />
      <div className="p-3">
        <div className="text-[13px] font-medium text-ink-0 truncate">{taskTitle(task)}</div>
        {hasText && enriched.summary?.content && (
          <p className="text-[11px] text-ink-4 mt-1 line-clamp-2 leading-snug">
            {enriched.summary.content.replace(/[#*]/g, '').split('\n').find(l => l.trim() && !l.startsWith('#'))?.trim()}
          </p>
        )}
        <div className="flex gap-1 mt-2">
          {phases.map(p => (
            <span
              key={p.label}
              className={`text-[9px] px-1.5 py-0.5 rounded-md ${
                p.state === 'done' ? 'bg-moss/10 text-moss'
                : p.state === 'running' ? 'bg-sienna-500/10 text-sienna-700'
                : 'bg-paper-2 text-ink-5'
              }`}
            >
              {p.label}
            </span>
          ))}
        </div>
      </div>
    </button>
  )
}

export function TableThumb({ taskId, onClick }: { taskId: number; onClick: (e: React.MouseEvent) => void }) {
  return (
    <button
      onClick={onClick}
      className="w-16 h-10 rounded-md overflow-hidden shrink-0 proto-card-hover ring-0 hover:ring-2 hover:ring-sienna-500/40 transition-shadow"
      title="查看转写与摘要"
    >
      <VideoThumb taskId={taskId} className="w-full h-full" compact />
    </button>
  )
}
