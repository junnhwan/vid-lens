import { processBeats } from '@/lib/processBeats'
import type { TaskStage } from '@/lib/types'

export function ProcessStrip({
  status,
  stage,
  has_transcription,
}: {
  status: number
  stage: TaskStage
  has_transcription: boolean
}) {
  const beats = processBeats({ status, stage, has_transcription })
  return (
    <div className="proc-strip" aria-hidden="true">
      {beats.map(b => (
        <i key={b.id} className={`proc-beat ${b.id} ${b.state}`} />
      ))}
    </div>
  )
}
