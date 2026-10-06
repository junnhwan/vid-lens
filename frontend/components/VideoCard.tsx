import { useState } from 'react'
import Link from '@/lib/router'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, fmtSize, sourceLabel, taskTitle } from '@/lib/format'
import { formatTime } from '@/components/Citation'
import { taskCategory, taskStateView } from '@/lib/taskStatus'
import { summaryFailureView } from '@/lib/summaryFailure'
import { ProcessStrip } from '@/components/ProcessStrip'
import { VideoStill } from '@/components/VideoPoster'
import { TranscriptionProgressPanel } from '@/components/TranscriptionProgressPanel'
import { VisualProgressPanel } from '@/components/VisualProgressPanel'
import { Icon } from '@/components/ui/Icon'

export function VideoCard({ task }: { task: VideoTask }) {
  const state = taskStateView(task)
  const cat = taskCategory(task)
  const ready = cat === 'ready' && task.has_transcription
  const failed = cat === 'failed'
  const summaryFailure = summaryFailureView(task)
  const [durationMs, setDurationMs] = useState(0)

  return (
    <article className="vcard">
    <Link className="vcard-open" href={`/video/${task.id}`}>
      <div className="vthumb">
        <VideoStill
          taskId={task.id}
          seed={task.file_md5 || task.filename}
          onDuration={setDurationMs}
          fallbackTitle={taskTitle(task)}
        />
        <span className="vcard-play" aria-hidden="true"><Icon name="play" /></span>
        {!ready && <span className={`vstate chip ${state.chip}`}>{state.text}</span>}
        {durationMs > 0 && <span className="vlen">{formatTime(durationMs)}</span>}
      </div>
      <div className="vmeta">
        <h4>{taskTitle(task)}</h4>
        <div className="vsub">
          <span>{sourceLabel(task)}</span>
          <span>{fmtSize(task.file_size)}</span>
          <span style={{ marginLeft: 'auto' }}>{fmtRelTime(task.updated_at)}</span>
        </div>
        <div className="vcard-capability"><Icon name={task.retrievable ? 'message' : task.has_transcription ? 'file' : 'video'} size="sm" /><span>{task.retrievable ? '可结合原文提问' : task.has_transcription ? '转写可阅读' : task.visual_status === 'completed' ? '画面已分析' : '等待内容处理'}</span></div>
      </div>
    </Link>
      <div className="vcard-status">
        {cat === 'processing' && (task.status === 1 || task.status === 2) && (
          <ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} last_job_type={task.last_job_type} has_rag_index={task.has_rag_index} visual_status={task.visual_status} />
        )}
        {cat === 'processing' && task.summary_job && task.status !== 1 && task.status !== 2 && <div className="mini-prog"><div className="row"><b>{state.text}</b></div></div>}
        {((task.stage === 'transcribing' || task.stage === 'visual_indexing') && (task.status === 1 || task.status === 2)) && <TranscriptionProgressPanel task={task} compact />}
        {((task.stage === 'transcribing' || task.stage === 'visual_indexing') && (task.status === 1 || task.status === 2)) && <VisualProgressPanel task={task} compact />}
        {failed && (summaryFailure || task.error_msg) && (
          <div className="mini-prog">
            <div className="row"><b style={{ color: 'var(--bad)' }}>{summaryFailure ? `${summaryFailure.category} · ${summaryFailure.retry}` : task.error_msg}</b></div>
          </div>
        )}
      </div>
    </article>
  )
}
