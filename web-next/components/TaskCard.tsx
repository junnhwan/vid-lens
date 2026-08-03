'use client'

import { useState } from 'react'
import { AlertCircle, ArchiveX, ArrowRight, RotateCw, Loader2, Play } from 'lucide-react'
import type { VideoTask } from '@/lib/types'
import { TaskStatusEnum } from '@/lib/types'
import { api, ApiError } from '@/lib/api'
import { statusBadge, statusLabel, computePhases, fmtSize, fmtRelTime, sourceLabel, Phase, PhaseState } from '@/lib/format'
import { useToast } from '@/components/Toast'
import { useRole } from '@/lib/useRole'

// 任务卡片：各状态徽标 + 三阶段进度条 + 失败重试 + 完成态去问答。
// 选中态由父组件给 className is-sel 控制（左侧 accent 指示条）。
export default function TaskCard({ task, index, selected, onSelect, onRetried }: {
  task: VideoTask
  index: number
  selected: boolean
  onSelect: () => void
  onRetried?: () => void
}) {
  const [retrying, setRetrying] = useState(false)
  const [starting, setStarting] = useState(false)
  const toast = useToast()
  const { isDemo } = useRole()
  const badge = statusBadge(task.status)
  const phases = computePhases(task)
  const isDead = task.status === TaskStatusEnum.Dead
  const isFailed = task.status === TaskStatusEnum.Failed
  const isRunning = task.status === TaskStatusEnum.Running

  // 待处理（已上传未转写）任务的入口：开始转写 → 后续摘要/索引在详情面板触发。
  // 演示账号只读：隐藏写入口，只留已完成任务的问答。
  const canStartTranscribe = !isDemo && task.status === TaskStatusEnum.Pending && !task.has_transcription

  const startTranscribe = async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (starting) return
    setStarting(true)
    try {
      await api.transcribe(task.id)
      toast.success('已提交转写，稍后自动刷新')
      onRetried?.()
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : '提交转写失败')
    } finally {
      setStarting(false)
    }
  }

  // 失败重试：按后端 last_job_type 选对应投递端点。
  // 后端常量（internal/model/task_job.go）：transcribe / analyze / download / rag_index
  // download 没有对应前端入口（下载由 upload/upload-url 触发），此时禁用按钮提示重新上传。
  // 演示账号只读：不提供重试入口。
  const retryJob = task.last_job_type
  const canRetry = !isDemo && isFailed && retryJob !== 'download'
  const retryLabel = retryJob === 'rag_index' ? '重建索引'
    : retryJob === 'analyze' ? '重试摘要'
    : retryJob === 'transcribe' ? '重试转写'
    : '重试'

  const retry = async (e: React.MouseEvent) => {
    e.stopPropagation()
    if (retrying) return
    setRetrying(true)
    try {
      if (retryJob === 'analyze') await api.analyze(task.id)
      else if (retryJob === 'rag_index') await api.triggerRagIndex(task.id)
      else await api.transcribe(task.id) // transcribe + 未知 job 兜底走转写（最常见入口）
      toast.success(`已重新投递（${retryLabel}），稍后自动刷新`)
      onRetried?.()
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : '重试失败')
    } finally {
      setRetrying(false)
    }
  }

  return (
    <article
      onClick={onSelect}
      className={`entry ${selected ? 'is-sel' : ''} border-b border-ink-0/15 py-3.5 cursor-pointer ${isDead ? 'opacity-60' : ''}`}
    >
      <div className="flex items-start gap-4">
        <span className="font-mono text-[11px] text-ink-4 w-7 shrink-0 pt-1">{String(index + 1).padStart(2, '0')}</span>
        <div className="min-w-0 flex-1">
          {/* 状态行 */}
          <div className="flex items-center gap-2 mb-1">
            <span className={`inline-flex items-center gap-1.5 px-1.5 py-0.5 ${badge.cls} text-[10px] font-medium`}>
              {badge.live && <span className="w-1.5 h-1.5 rounded-full bg-sienna-500 live" />}
              {task.status === TaskStatusEnum.Failed && <AlertCircle className="w-3 h-3" />}
              {isDead && <ArchiveX className="w-3 h-3" />}
              {statusLabel(task.status)}
            </span>
            <span className="font-mono text-[10px] text-ink-4">{sourceLabel(task)} · {fmtRelTime(task.created_at)}</span>
            {task.status === TaskStatusEnum.Completed && (task.has_transcription || task.has_summary) && (
              <span className="font-mono text-[10px] text-ink-4 border-l border-ink-0/20 pl-2">
                {task.has_transcription && 'ASR'}
                {task.has_transcription && task.has_summary && ' · '}
                {task.has_summary && 'Summary'}
              </span>
            )}
          </div>
          {/* 标题 */}
          <h3 className={`font-sans text-[16px] font-medium tight ${isDead ? 'text-ink-3 line-through decoration-1' : 'text-ink-0'} leading-snug`}>
            {task.title || task.filename}
          </h3>

          {/* Running：三阶段进度条 */}
          {isRunning && (
            <div className="mt-2.5 grid grid-cols-3 gap-5 max-w-xl">
              {phases.map((p) => <PhaseBar key={p.label} phase={p} />)}
            </div>
          )}

          {/* Completed：摘要预览 */}
          {task.status === TaskStatusEnum.Completed && task.summary?.content && (
            <p className="font-sans text-[13px] text-ink-2 leading-relaxed mt-1 line-clamp-1">{stripMd(task.summary.content)}</p>
          )}

          {/* Failed：错误 + 重试 */}
          {isFailed && (
            <div className="mt-1.5 flex items-center gap-3">
              <p className="font-mono text-[11px] text-rust flex items-center gap-2 min-w-0">
                {task.error_msg || task.last_error_msg || '处理失败'}
                <button className="underline underline-offset-2 hover:text-sienna-700 shrink-0" onClick={(e) => { e.stopPropagation(); onSelect() }}>
                  查看详情 →
                </button>
              </p>
              {canRetry && (
                <button
                  onClick={retry}
                  disabled={retrying}
                  className="btn-line h-6 px-2 text-[10px] font-medium shrink-0 flex items-center gap-1 disabled:opacity-50"
                  title={retryJob === 'download' ? '下载失败需重新上传' : `重试 ${retryLabel}`}
                >
                  {retrying ? <Loader2 className="w-3 h-3 animate-spin" /> : <RotateCw className="w-3 h-3" />}{retrying ? '重试中' : retryLabel}
                </button>
              )}
              {retryJob === 'download' && (
                <span className="font-mono text-[10px] text-ink-4 shrink-0">下载失败，请删除重新上传</span>
              )}
            </div>
          )}
        </div>

        {/* 右侧：大小 / 开始转写 / 去问答 */}
        {task.status === TaskStatusEnum.Completed ? (
          <a
            href={`/chat/${task.id}`}
            onClick={(e) => e.stopPropagation()}
            className="btn-line h-7 px-2.5 text-[10px] font-medium shrink-0 mt-0.5 flex items-center gap-1"
          >去问答 <ArrowRight className="w-3 h-3" /></a>
        ) : canStartTranscribe ? (
          <button
            onClick={startTranscribe}
            disabled={starting}
            className="btn-line h-7 px-2.5 text-[10px] font-medium shrink-0 mt-0.5 flex items-center gap-1 disabled:opacity-50"
            title="触发 ASR 转写，完成后可在详情面板生成摘要与索引"
          >
            {starting ? <Loader2 className="w-3 h-3 animate-spin" /> : <Play className="w-3 h-3" />}{starting ? '提交中' : '开始转写'}
          </button>
        ) : (
          <span className="font-mono text-[10px] text-ink-4 shrink-0 pt-1">{fmtSize(task.file_size)}</span>
        )}
      </div>
    </article>
  )
}

function PhaseBar({ phase }: { phase: Phase }) {
  const { label, state } = phase
  const stateCls = (s: PhaseState) => {
    switch (s) {
      case 'done': return 'bg-moss'
      case 'running': return 'flow'           // 全宽流光，不定态
      default: return 'bg-ink-0/30'
    }
  }
  // 去掉假百分比：running 只显示"处理中"，不再显示编造的数字
  const stateLabel = (s: PhaseState) => s === 'done' ? '完成' : s === 'running' ? '处理中' : '排队'
  const stateColor = (s: PhaseState) => s === 'done' ? 'text-moss' : s === 'running' ? 'text-sienna-700' : 'text-ink-4'
  // running 态进度条满宽流光，done 满宽实色，queued 留 5% 灰头占位
  const width = state === 'done' ? '100%' : state === 'running' ? '100%' : '5%'
  return (
    <div>
      <div className="flex justify-between font-mono text-[10px] mb-1">
        <span className="text-ink-4 wide uppercase">{label}</span>
        <span className={stateColor(state)}>{stateLabel(state)}</span>
      </div>
      <div className="h-px bg-ink-0/15"><div className={`h-full ${stateCls(state)}`} style={{ width }} /></div>
    </div>
  )
}

// 去掉 markdown 标记做单行预览
function stripMd(s: string): string {
  return s.replace(/[#*`_>\-]/g, ' ').replace(/\s+/g, ' ').trim()
}

// 骨架卡片
export function TaskCardSkeleton() {
  return (
    <article className="entry border-b border-ink-0/15 py-3.5">
      <div className="flex items-start gap-4">
        <span className="font-mono text-[11px] text-ink-4 w-7 shrink-0 pt-1">··</span>
        <div className="flex-1">
          <div className="sk h-3 w-24 mb-2" />
          <div className="sk h-4 w-2/3 mb-2" />
          <div className="sk h-2 w-1/2" />
        </div>
      </div>
    </article>
  )
}
