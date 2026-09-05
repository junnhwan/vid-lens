'use client'

import { useRouter } from 'next/navigation'
import type { VideoTask } from '@/lib/types'
import { fmtRelTime, fmtSize, sourceLabel, taskTitle } from '@/lib/format'
import { stageLabel, taskStateView } from '@/lib/taskStatus'

// 视频卡:与原型 .vcard 同构。
// 后端没有封面/时长接口,缩略位用原型给定的渐变占位(.vthumb-art),
// 右下角时间码位置展示文件大小,不虚构时长。

export function VideoCard({ task }: { task: VideoTask }) {
  const router = useRouter()
  const state = taskStateView(task)
  const failed = task.status === 4 || task.status === 5
  const ready = task.status === 3 && task.has_transcription

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
          {failed
            ? <span>需要重试</span>
            : task.status === 2
              ? <><span className="mono" style={{ color: 'var(--acc-strong)' }}>{stageLabel(task.stage)}</span><span>{sourceLabel(task)}</span></>
              : <><span>{task.has_transcription ? '已转写 · 可问答' : '文本处理中'}</span><span>{sourceLabel(task)}</span></>}
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
