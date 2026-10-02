import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import { TaskStatusEnum, type TranscriptionProgress, type VideoTask } from '@/lib/types'
import { formatClock, fmtDateTime, fmtTimeOfDay } from '@/lib/format'

function elapsed(start?: string) {
  if (!start) return ''
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(start).getTime()) / 1000))
  return `${Math.floor(seconds / 60)}分${seconds % 60}秒`
}

function waitLabel(reason?: string) {
  if (reason === 'local_admission') return '本地模型额度等待'
  if (reason === 'provider_rate_limit') return '模型服务限流等待'
  return '模型调用失败后等待重试'
}

export function TranscriptionProgressPanel({ task, compact = false, onProgress }: { task: VideoTask; compact?: boolean; onProgress?: (progress: TranscriptionProgress | null) => void }) {
  const relevant = task.last_job_type === 'transcribe' || task.stage === 'transcribing' || task.stage === 'visual_indexing'
  const active = (task.stage === 'transcribing' || task.status === TaskStatusEnum.Queued && task.last_job_type === 'transcribe') && (task.status === TaskStatusEnum.Queued || task.status === TaskStatusEnum.Running)
  const [progress, setProgress] = useState<TranscriptionProgress | null>(null)
  const [error, setError] = useState(false)
  const [reload, setReload] = useState(0)
  const [, tick] = useState(0)

  useEffect(() => {
    if (!relevant) return
    let live = true
    const refresh = () => {
      void api.getTranscriptionProgress(task.id).then(value => {
        if (live) { setProgress(value); setError(false); onProgress?.(value) }
      }).catch(() => { if (live) { setError(true); onProgress?.(null) } })
    }
    refresh()
    const poll = active ? window.setInterval(refresh, 5000) : undefined
    const timer = active ? window.setInterval(() => tick(n => n + 1), 1000) : undefined
    return () => {
      live = false
      if (poll) window.clearInterval(poll)
      if (timer) window.clearInterval(timer)
    }
  }, [task.id, task.status, relevant, active, reload, onProgress])

  if (!relevant) return null
  if (error) return <div className="muted" role="status" style={{ fontSize: 12 }}>转写进度暂不可用 <button className="btn btn-sm" onClick={() => setReload(n => n + 1)}>重新读取转写进度</button></div>
  if (!progress) return <div className="muted" style={{ fontSize: 12 }}>正在读取转写进度…</div>
  if (!active && !progress.total) return null

  const current = progress.chunks.filter(c => c.status === 'running' || c.status === 'retry_wait')
  const completed = progress.chunks.filter(c => c.status === 'completed')
  const failed = progress.chunks.filter(c => c.status === 'failed')
  const chunksComplete = progress.total > 0 && progress.completed === progress.total
  const stale = active && !chunksComplete && task.status === TaskStatusEnum.Running && Date.now() - new Date(progress.updated_at).getTime() > 120000
  return (
    <div className="card card-pad" style={{ marginTop: 8, fontSize: 12 }} role="status">
      <b>转写进度</b>
      <div className="muted" style={{ marginTop: 4 }}>
        {task.status === TaskStatusEnum.Queued ? '等待任务启动或转写并发名额' : progress.total ? `已完成 ${progress.completed}/${progress.total} 个分片` : '正在准备音频分片'}
        {progress.total > 0 ? ` · 待调用 ${progress.pending} · 调用中 ${progress.running} · 限流/重试等待 ${progress.retry_waiting} · 失败 ${progress.failed}` : ''}
      </div>
      <div className="muted" style={{ marginTop: 4 }}>
        服务配置：每进程最多 {progress.video_concurrency} 个转写视频，每视频最多 {progress.chunk_concurrency} 个 ASR 分片并发
        {active && !chunksComplete ? ` · 已等待/运行 ${elapsed(progress.started_at || task.created_at)}` : ''}
      </div>
      {chunksComplete && <div className="muted">转写分片已完成，后续处理进度见处理阶段。</div>}
      {!active && !chunksComplete && <div style={{ color: 'var(--bad)' }}>本次转写未完成{task.has_transcription ? '，引用定位尚未更新' : ''}。</div>}
      {progress.job_next_retry_at && <div className="muted">任务自动重试 {progress.job_retry_count}/{progress.job_max_retries} · 下次 {fmtDateTime(progress.job_next_retry_at)}</div>}
      {stale && <div style={{ color: 'var(--warn)' }}>最近状态超过 2 分钟未更新，可能停滞，请稍后刷新核对。</div>}
      {current.map(c => <div key={c.index} style={{ marginTop: 5 }}>
        第 {c.index}/{progress.total} 段 {c.end_ms > c.start_ms ? `(${formatClock(c.start_ms)}–${formatClock(c.end_ms)})` : '（时间未记录）'} · {c.status === 'running' ? '调用中' : waitLabel(c.wait_reason)}
        {c.retry_count > 0 ? ` · 已重试 ${c.retry_count} 次` : ''}
        {c.next_retry_at ? ` · 预计 ${fmtTimeOfDay(c.next_retry_at)} 后重试` : ''}
      </div>)}
      {failed.map(c => <div key={c.index} style={{ marginTop: 5, color: 'var(--bad)' }}>
        第 {c.index}/{progress.total} 段 {c.end_ms > c.start_ms ? `(${formatClock(c.start_ms)}–${formatClock(c.end_ms)})` : '（时间未记录）'} · 转写失败
      </div>)}
      {!compact && completed.length > 0 && <details style={{ marginTop: 8 }} open={active}>
        <summary>已完成分片与文字（{completed.length}）</summary>
        {completed.map(c => <div key={c.index} style={{ marginTop: 8 }}>
          <b>第 {c.index}/{progress.total} 段 {c.end_ms > c.start_ms ? `${formatClock(c.start_ms)}–${formatClock(c.end_ms)}` : '时间未记录'}</b>
          <p style={{ whiteSpace: 'pre-wrap', marginTop: 4 }}>{c.content || '该分片未返回文字'}</p>
        </div>)}
      </details>}
    </div>
  )
}
