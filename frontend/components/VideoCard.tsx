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

export function VideoCard({ task }: { task: VideoTask }) {
  const state = taskStateView(task)
  const ready = task.status === 3 && task.has_transcription
  const failed = task.status === 4 || task.status === 5
  const summaryFailure = summaryFailureView(task)
  const cat = taskCategory(task)
  const [durationMs, setDurationMs] = useState(0)

  return (
    <Link className="vcard" href={`/video/${task.id}`}>
      <div className="vthumb">
        <VideoStill
          taskId={failed ? undefined : task.id}
          seed={task.file_md5 || task.filename}
          onDuration={setDurationMs}
        />
        {!ready && <span className={`vstate chip ${state.chip}`}>{state.text}</span>}
        <span className="vlen">{durationMs > 0 ? formatTime(durationMs) : fmtSize(task.file_size)}</span>
      </div>
      <div className="vmeta">
        <h4>{taskTitle(task)}</h4>
        <div className="vsub">
          <span>{sourceLabel(task)}</span>
          <span style={{ marginLeft: 'auto' }}>{fmtRelTime(task.updated_at)}</span>
        </div>
        {cat === 'processing' && (
          <ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} last_job_type={task.last_job_type} has_rag_index={task.has_rag_index} visual_status={task.visual_status} />
        )}
        {((task.stage === 'transcribing' || task.stage === 'visual_indexing') && (task.status === 1 || task.status === 2)) && <TranscriptionProgressPanel task={task} compact />}
        {((task.stage === 'transcribing' || task.stage === 'visual_indexing') && (task.status === 1 || task.status === 2)) && <VisualProgressPanel task={task} compact />}
        {failed && (summaryFailure || task.error_msg) && (
          <div className="mini-prog">
            <div className="row"><b style={{ color: 'var(--bad)' }}>{summaryFailure ? `${summaryFailure.category} · ${summaryFailure.retry}` : task.error_msg}</b></div>
          </div>
        )}
      </div>
    </Link>
  )
}
