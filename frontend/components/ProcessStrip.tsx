import { processBeats } from '@/lib/processBeats'
import type { TaskStage } from '@/lib/types'

export function ProcessStrip({
  status,
  stage,
  has_transcription,
  last_job_type,
}: {
  status: number
  stage: TaskStage
  has_transcription: boolean
  last_job_type?: string
}) {
  const beats = processBeats({ status, stage, has_transcription, last_job_type })
  return (
    <div className="proc-strip-wrap" role="group" aria-label="视频处理阶段；灰色等待，脉动进行中，实色完成，红色失败">
      <div className="proc-strip">
        {beats.map(b => {
          const label = { ingest: '入库', asr: '转写', visual: '画面分析', index: '检索索引' }[b.id]
          const state = { queued: '等待', running: '进行中', done: '完成', error: '失败' }[b.state]
          return <span key={b.id} className={`proc-beat ${b.id} ${b.state}`} role="img" aria-label={`${label}：${state}`} title={`${label}：${state}`} />
        })}
      </div>
      <div className="proc-legend" aria-hidden="true"><span>入库</span><span>转写</span><span>画面分析</span><span>检索索引</span></div>
    </div>
  )
}
