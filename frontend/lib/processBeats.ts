export type ProcessBeatId = 'ingest' | 'asr' | 'visual' | 'index'
export type ProcessBeatState = 'queued' | 'running' | 'done' | 'error' | 'skipped'

export interface ProcessBeat {
  id: ProcessBeatId
  state: ProcessBeatState
}

const STAGE_ORDER = [
  'none', 'downloading', 'uploaded', 'transcribing', 'visual_indexing', 'summarizing', 'indexing',
] as const

function stageIndex(stage: string): number {
  const i = (STAGE_ORDER as readonly string[]).indexOf(stage)
  return i < 0 ? 0 : i
}

function beatOfStage(stage: string): ProcessBeatId {
  const i = stageIndex(stage)
  if (i <= 2) return 'ingest'
  if (i === 3) return 'asr'
  if (i === 4) return 'visual'
  return 'index'
}

/** 入库 → 转写 → 画面 → 索引,由真实 stage / 转写标记驱动,无文案。 */
export function processBeats(task: {
  status: number
  stage: string
  has_transcription: boolean
  last_job_type?: string
  has_rag_index?: boolean
  visual_status?: string
}): ProcessBeat[] {
  const ids: ProcessBeatId[] = ['ingest', 'asr', 'visual', 'index']
  if (task.status === 4 || task.status === 5) {
    const err = beatOfStage(task.stage)
    const errAt = ids.indexOf(err)
    return ids.map((id, i) => ({
      id,
      state: i < errAt ? 'done' : i === errAt ? 'error' : 'queued',
    }))
  }

  const i = stageIndex(task.stage)
  const running = task.status === 2
  const completed = task.status === 3

  const ingest: ProcessBeatState = i >= 2 || completed ? 'done' : running && i <= 1 ? 'running' : 'queued'
  const asr: ProcessBeatState = task.has_transcription
    ? 'done'
    : running && i === 3
      ? 'running'
      : 'queued'
  const visual: ProcessBeatState = task.visual_status === 'completed'
    ? 'done'
    : task.visual_status === 'failed' || task.visual_status === 'interrupted'
      ? 'error'
      : task.visual_status === 'skipped' || (completed && !task.visual_status)
        ? 'skipped'
        : running && (i === 4 || task.visual_status === 'running')
          ? 'running'
          : 'queued'
  const index: ProcessBeatState = task.has_rag_index || (task.has_rag_index === undefined && completed && task.last_job_type === 'rag_index')
    ? 'done'
    : running && task.stage === 'indexing'
      ? 'running'
      : 'queued'

  return [
    { id: 'ingest', state: ingest },
    { id: 'asr', state: asr },
    { id: 'visual', state: visual },
    { id: 'index', state: index },
  ]
}
