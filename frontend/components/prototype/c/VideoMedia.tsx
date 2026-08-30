'use client'

import { Play } from 'lucide-react'
import { statusBadge, statusLabel } from '@/lib/format'
import type { TaskStatus } from '@/lib/types'

/** 冷纸 + 深琥珀封面，同一色相家族，避免彩虹渐变 */
export const THUMB_GRADIENTS = [
  'from-ink-1 via-ink-2 to-sienna-600/55',
  'from-ink-0 via-sienna-700/70 to-ink-2',
  'from-sienna-700 via-ink-2 to-ink-1',
  'from-ink-2 via-sienna-600/40 to-ink-0',
  'from-ink-1 via-ink-0 to-sienna-500/35',
]

export function thumbGradient(id: number) {
  return THUMB_GRADIENTS[id % THUMB_GRADIENTS.length]
}

interface VideoThumbProps {
  taskId: number
  className?: string
  compact?: boolean
  showPlay?: boolean
  showStatus?: boolean
  status?: TaskStatus
  runningPct?: number
}

export function VideoThumb({ taskId, className = '', compact, showPlay, showStatus, status, runningPct }: VideoThumbProps) {
  const badge = status != null ? statusBadge(status) : null

  return (
    <div className={`relative bg-gradient-to-br ${thumbGradient(taskId)} ${className}`}>
      <div className="absolute inset-0 bg-ink-0/20" />
      {showPlay && (
        <div className="absolute inset-0 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className={`rounded-md bg-paper-0/20 backdrop-blur-sm flex items-center justify-center border border-paper-0/25 ${compact ? 'w-8 h-8' : 'w-11 h-11'}`}>
            <Play className={`text-paper-0 ml-0.5 ${compact ? 'w-3 h-3' : 'w-4 h-4'}`} fill="currentColor" />
          </div>
        </div>
      )}
      {showStatus && badge && status != null && (
        <div className="absolute top-2 left-2">
          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md text-[9px] font-medium bg-ink-0/40 text-paper-0 backdrop-blur-sm">
            {badge.live && <span className="w-1 h-1 rounded-full bg-sienna-400 proto-pulse" />}
            {statusLabel(status)}
          </span>
        </div>
      )}
      {runningPct != null && runningPct > 0 && (
        <div className="absolute bottom-0 inset-x-0 h-0.5 bg-ink-0/25">
          <div className="h-full bg-sienna-400 proto-progress-bar" style={{ width: `${runningPct}%` }} />
        </div>
      )}
    </div>
  )
}
