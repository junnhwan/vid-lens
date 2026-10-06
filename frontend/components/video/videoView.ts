import type { VideoTask, VisualMode, RAGIndexResult } from '@/lib/types'
import type { ConfirmAction } from './useVideoActions'
export const visualChoices: { mode: VisualMode; title: string; description: string; icon: 'video' | 'scan' | 'photo' | 'layers' }[] = [
  { mode: 'off', title: '关闭', description: '演讲、访谈或画面变化很少时，转写通常已经足够。', icon: 'video' },
  { mode: 'ocr', title: '文字识别 OCR', description: '适合文字课件、笔记和清晰的板书，提取画面中的文字。', icon: 'scan' },
  { mode: 'caption', title: '画面描述', description: '适合图表、流程图、示意图，调用视觉模型理解画面。', icon: 'photo' },
  { mode: 'both', title: 'OCR + 画面描述', description: '适合文字与图表混合的课程，会增加处理量与模型调用。', icon: 'layers' },
]

export function taskVisualMode(task: VideoTask): VisualMode {
  return task.visual_mode || (task.visual_disabled ? 'off' : 'both')
}

export function visualModeLabel(mode: VisualMode): string {
  return visualChoices.find(choice => choice.mode === mode)?.title || '关闭'
}

export function indexActionLabel(index: RAGIndexResult | null): string {
  if (!index) return '索引状态不可用'
  if (index.status === 'queued') return '索引排队中…'
  if (index.status === 'indexing') return '索引构建中…'
  if (index.status === 'failed') return '索引失败，重试'
  if (index.status === 'needs_rebuild' || index.needs_rebuild) return '需要重建索引'
  return index.indexed ? '重建索引' : '建立索引'
}

export function indexConfirm(index: RAGIndexResult): ConfirmAction {
  const label = indexActionLabel(index)
  const replacing = index.indexed || index.needs_rebuild || index.status === 'needs_rebuild'
  return {
    kind: 'index', title: `${label}？`, confirmLabel: label,
    body: `建立后，视频问答可按内容含义找到相关片段并定位视频位置；只播放视频、查看转写或摘要无需建立索引。系统会将已有转写文字及已生成的画面文字、画面描述（如有）发送给当前配置的向量模型，调用 Embedding 并消耗额度；不会重新转写，也不会修改原视频或这些文字。${replacing ? '现有检索索引将被替换。' : ''}`,
  }
}

export function indexPhase(index: RAGIndexResult): string {
  if (index.status === 'queued') return '等待索引任务启动'
  if (index.status === 'failed') return '索引失败'
  if (index.status === 'indexed') return '已完成'
  if (index.status === 'needs_rebuild') return '等待重建'
  if (index.status === 'not_indexed') return '尚未建立'
  const phases: Record<string, string> = {
    preparing: '准备文本块', embedding: '调用 Embedding 模型',
    waiting: index.wait_reason === 'local_admission' ? '等待本地模型额度' : index.wait_reason === 'provider_rate_limit' ? '等待第三方模型限流重试' : '等待重试',
    writing: '写入向量', completed: '已完成',
  }
  return phases[index.build_phase] || '等待进度更新'
}
