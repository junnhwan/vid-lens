import type { VideoTask } from '@/lib/types'

export interface DashboardPrototypeProps {
  tasks: VideoTask[]
  loading: boolean
  error: string
  onRefresh: () => void
  onUpload: () => void
  selectedId: number | null
  onSelect: (id: number | null) => void
}

export const VARIANTS = [
  { key: 'A', name: '沉浸网格' },
  { key: 'B', name: '流水线看板' },
  { key: 'C', name: '研读工作台' },
] as const

export type VariantKey = (typeof VARIANTS)[number]['key']

export function taskTitle(t: VideoTask) {
  return t.title || t.filename || `任务 #${t.id}`
}
