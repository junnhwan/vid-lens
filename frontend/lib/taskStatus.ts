import {
  TaskStatusEnum,
  type TaskStage,
  type VideoTask,
} from './types'

// 任务状态 → 原型语义(全部/可问答/处理中/失败)。
// 「可问答」= 转写完成(检索索引由 RAG 投影单独维护,此处不做假判断)。
export type TaskCategory = 'ready' | 'processing' | 'failed'

export function taskCategory(t: Pick<VideoTask, 'status' | 'has_transcription'>): TaskCategory {
  if (t.status === TaskStatusEnum.Failed || t.status === TaskStatusEnum.Dead) return 'failed'
  if (t.status === TaskStatusEnum.Completed && t.has_transcription) return 'ready'
  return 'processing'
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
  if (t.status === TaskStatusEnum.Completed) {
    return t.has_transcription
      ? { chip: 'chip-ok', text: '可问答' }
      : { chip: 'chip-mute', text: '已完成' }
  }
  if (t.status === TaskStatusEnum.Queued) return { chip: 'chip-mute', text: '排队中' }
  if (t.status === TaskStatusEnum.Running) return { chip: 'chip-acc', text: stageLabel(t.stage), live: true }
  return { chip: 'chip-mute', text: '待处理' }
}

const STAGE_LABELS: Record<TaskStage, string> = {
  none: '处理中',
  downloading: '下载中',
  uploaded: '已上传',
  transcribing: '转写中',
  visual_indexing: '画面索引中',
  summarizing: '生成摘要中',
  indexing: '构建索引中',
}

export function stageLabel(stage: TaskStage): string {
  return STAGE_LABELS[stage] || '处理中'
}
