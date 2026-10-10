import {
  TaskStatusEnum,
  type TaskStage,
  type VideoTask,
} from './types.ts'

// 任务生命周期与已保存内容；当前模型的检索能力由 retrievable 单独表达。
export type TaskCategory = 'ready' | 'processing' | 'failed'

export function taskCategory(t: Pick<VideoTask, 'status' | 'has_transcription' | 'visual_status' | 'summary_job'>): TaskCategory {
  if (t.status === TaskStatusEnum.Failed || t.status === TaskStatusEnum.Dead) return 'failed'
  if (t.status === TaskStatusEnum.Queued || t.status === TaskStatusEnum.Running) return 'processing'
  if (t.summary_job?.status === TaskStatusEnum.Queued || t.summary_job?.status === TaskStatusEnum.Running) return 'processing'
  if (t.summary_job?.status === TaskStatusEnum.Failed || t.summary_job?.status === TaskStatusEnum.Dead) return 'failed'
  if (t.status === TaskStatusEnum.Completed && (t.has_transcription || t.visual_status === 'completed')) return 'ready'
  return 'processing'
}

export function taskNeedsPolling(t: Pick<VideoTask, 'status' | 'summary_job'>): boolean {
  return t.status === TaskStatusEnum.Queued || t.status === TaskStatusEnum.Running ||
    t.summary_job?.status === TaskStatusEnum.Queued || t.summary_job?.status === TaskStatusEnum.Running ||
    (t.summary_job?.status === TaskStatusEnum.Failed && !!t.summary_job.next_retry_at)
}

export interface TaskStateView {
  chip: string // chip-* 类
  text: string
  live?: boolean // 处理中脉冲
}

export function taskStateView(t: VideoTask): TaskStateView {
  if (t.status === TaskStatusEnum.Failed || t.status === TaskStatusEnum.Dead) {
    return { chip: 'chip-bad', text: t.status === TaskStatusEnum.Dead ? '已废弃' : '失败' }
  }
  if (t.status !== TaskStatusEnum.Queued && t.status !== TaskStatusEnum.Running && t.summary_job) {
    const child = t.summary_job
    if (child.status === TaskStatusEnum.Queued) return { chip: 'chip-mute', text: '摘要排队中' }
    if (child.status === TaskStatusEnum.Running) return { chip: 'chip-acc', text: '摘要生成中', live: true }
    if (child.status === TaskStatusEnum.Failed || child.status === TaskStatusEnum.Dead) return { chip: 'chip-bad', text: '摘要生成失败' }
  }
  if (t.status === TaskStatusEnum.Completed) {
    return t.has_transcription
      ? { chip: 'chip-ok', text: '转写已完成' }
      : { chip: 'chip-mute', text: t.visual_status === 'completed' ? '画面已完成' : '已完成' }
  }
  if (t.status === TaskStatusEnum.Queued) return { chip: 'chip-mute', text: `排队中 · ${queuedStageLabel(t.stage)}` }
  if (t.status === TaskStatusEnum.Running) return { chip: 'chip-acc', text: stageLabel(t.stage), live: true }
  return { chip: 'chip-mute', text: '待处理' }
}

const STAGE_LABELS: Record<TaskStage, string> = {
  none: '等待处理',
  downloading: '下载中',
  uploaded: '已上传',
  text_source: '读取字幕或准备转写中',
  transcribing: 'ASR 转写中',
  aligning: '对齐句子时间中',
  visual_indexing: '画面分析中',
  summarizing: '生成摘要中',
  indexing: '构建检索索引中',
}

export function stageLabel(stage: TaskStage): string {
  return STAGE_LABELS[stage] || '等待处理'
}

function queuedStageLabel(stage: TaskStage): string {
  const names: Partial<Record<TaskStage, string>> = {
    text_source: '等待读取字幕', downloading: '等待下载', transcribing: '等待转写启动或并发名额', aligning: '等待对齐句子时间', visual_indexing: '等待画面分析',
    summarizing: '等待生成摘要', indexing: '等待检索索引',
  }
  return names[stage] || '等待任务启动'
}
