'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, fmtSize, sourceLabel, taskTitle } from '@/lib/format'
import { formatTime } from '@/components/Citation'
import { taskCategory, taskStateView } from '@/lib/taskStatus'
import { ProcessStrip } from '@/components/ProcessStrip'
import { VideoStill } from '@/components/VideoPoster'

export function VideoCard({ task }: { task: VideoTask }) {
  const router = useRouter()
  const state = taskStateView(task)
  const ready = task.status === 3 && task.has_transcription
  const failed = task.status === 4 || task.status === 5
  const cat = taskCategory(task)
  const [durationMs, setDurationMs] = useState(0)

  return (
    <div className="vcard" onClick={() => router.push(`/video/${task.id}`)}>
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
          <ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} />
        )}
        {failed && task.error_msg && (
          <div className="mini-prog">
            <div className="row"><b style={{ color: 'var(--bad)' }}>{task.error_msg}</b></div>
          </div>
        )}
      </div>
    </div>
  )
}
