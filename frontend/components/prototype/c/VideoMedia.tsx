'use client'

import { Play } from 'lucide-react'
import { statusBadge, statusLabel } from '@/lib/format'
import type { TaskStatus } from '@/lib/types'

/** 暖色版渐变封面（融合 A 的视觉 + C 的色调） */
export const THUMB_GRADIENTS = [
  'from-amber-600/90 via-orange-500/75 to-stone-400/70',
  'from-stone-600/85 via-amber-700/60 to-amber-500/65',
  'from-emerald-700/75 via-teal-600/60 to-amber-400/55',
  'from-rose-700/70 via-amber-600/55 to-stone-500/60',
  'from-stone-700/85 via-stone-600/70 to-amber-600/50',
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
      <div className="absolute inset-0 bg-stone-900/15" />
      {showPlay && (
        <div className="absolute inset-0 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className={`rounded-full bg-white/25 backdrop-blur-sm flex items-center justify-center border border-white/30 ${compact ? 'w-8 h-8' : 'w-11 h-11'}`}>
            <Play className={`text-white ml-0.5 ${compact ? 'w-3 h-3' : 'w-4 h-4'}`} fill="white" />
          </div>
        </div>
      )}
      {showStatus && badge && status != null && (
        <div className="absolute top-2 left-2">
          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md text-[9px] font-medium bg-black/35 text-white backdrop-blur-sm">
            {badge.live && <span className="w-1 h-1 rounded-full bg-amber-300 proto-pulse" />}
            {statusLabel(status)}
          </span>
        </div>
      )}
      {runningPct != null && runningPct > 0 && (
        <div className="absolute bottom-0 inset-x-0 h-0.5 bg-black/20">
          <div className="h-full bg-amber-300 proto-progress-bar" style={{ width: `${runningPct}%` }} />
        </div>
      )}
    </div>
  )
}
