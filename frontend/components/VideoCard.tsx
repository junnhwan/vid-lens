'use client'

import { useRouter } from 'next/navigation'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, fmtSize, sourceLabel, taskTitle } from '@/lib/format'
import { taskStateView } from '@/lib/taskStatus'

export function VideoCard({ task }: { task: VideoTask }) {
  const router = useRouter()
  const state = taskStateView(task)
  const ready = task.status === 3 && task.has_transcription
  const failed = task.status === 4 || task.status === 5

  return (
    <div className="vcard" onClick={() => router.push(`/video/${task.id}`)}>
      <div className="vthumb">
        <div className="vthumb-art" />
        {!ready && <span className={`vstate chip ${state.chip}`}>{state.text}</span>}
        <span className="vlen">{fmtSize(task.file_size)}</span>
      </div>
      <div className="vmeta">
        <h4>{taskTitle(task)}</h4>
        <div className="vsub">
          <span>{sourceLabel(task)}</span>
          <span style={{ marginLeft: 'auto' }}>{fmtRelTime(task.updated_at)}</span>
        </div>
        {failed && task.error_msg && (
          <div className="mini-prog">
            <div className="row"><b style={{ color: 'var(--bad)' }}>{task.error_msg}</b></div>
          </div>
        )}
      </div>
    </div>
  )
}
