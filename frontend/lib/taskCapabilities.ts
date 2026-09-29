import { TaskStatusEnum, type VideoTask, type VideoTimeline } from './types'

export function studyAvailability(task?: VideoTask | null, timeline?: VideoTimeline | null): { ready: boolean; reason: string } {
  if (!task || !timeline) return { ready: false, reason: '正在读取来源状态' }
  if (task.status === TaskStatusEnum.Queued || task.status === TaskStatusEnum.Running) return { ready: false, reason: '视频仍在处理，完成后可生成笔记' }
  const reasons: Record<string, string> = {
    processing: '视频仍在处理，完成后可生成笔记', incomplete_transcript: '转写尚未完整保存，请完成或重试转写',
    no_content: '尚无可用内容，请先转写或分析画面', source_limit_exceeded: '来源超过笔记处理上限，请选择更短的视频',
  }
  if (timeline.study_source_ready === true) return { ready: true, reason: '' }
  if (timeline.study_source_reason) return { ready: false, reason: reasons[timeline.study_source_reason] || '来源暂不可用' }
  return { ready: false, reason: '来源能力尚未确认，请刷新后重试' }
}

export function visualAvailability(task: VideoTask): { ready: boolean; reason: string } {
  if (task.status === TaskStatusEnum.Queued || task.status === TaskStatusEnum.Running || ['queued', 'running'].includes(task.visual_status)) return { ready: false, reason: '视频正在处理，完成后可分析画面' }
  if (!task.file_url || task.stage === 'downloading') return { ready: false, reason: '请先完成视频导入' }
  return { ready: true, reason: '' }
}
