'use client'

import { Play } from 'lucide-react'
import { statusBadge, statusLabel } from '@/lib/format'
import type { TaskStatus } from '@/lib/types'

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

export function VideoThumb({ taskId, className = '', showPlay, showStatus, status, running }: {
  taskId: number; className?: string; showPlay?: boolean; showStatus?: boolean; status?: TaskStatus; running?: boolean
}) {
  const badge = status != null ? statusBadge(status) : null
  return (
    <div className={`relative bg-gradient-to-br ${thumbGradient(taskId)} ${className}`}>
      <div className="absolute inset-0 bg-stone-900/15" />
      {showPlay && (
        <div className="absolute inset-0 flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity duration-200">
          <div className="w-10 h-10 rounded-full bg-white/25 backdrop-blur-sm border border-white/30 flex items-center justify-center">
            <Play className="w-4 h-4 text-white ml-0.5" fill="white" />
          </div>
        </div>
      )}
      {showStatus && badge && status != null && (
        <div className="absolute top-2 left-2">
          <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md text-[9px] font-medium bg-black/35 text-white backdrop-blur-sm">
            {badge.live && <span className="w-1 h-1 rounded-full bg-amber-300 ui-pulse" />}
            {statusLabel(status)}
          </span>
        </div>
      )}
      {running && (
        <div className="absolute bottom-0 inset-x-0 h-0.5 bg-black/20">
          <div className="h-full w-full bg-amber-300/90 flow" />
        </div>
      )}
    </div>
  )
}
