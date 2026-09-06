'use client'

import { useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'

const urlCache = new Map<number, Promise<string | null>>()

function playbackFor(taskId: number): Promise<string | null> {
  let hit = urlCache.get(taskId)
  if (!hit) {
    hit = api.getTaskPlaybackUrl(taskId).then(r => r.playback_url).catch(() => null)
    urlCache.set(taskId, hit)
  }
  return hit
}

function hashHue(seed: string): number {
  let h = 0
  for (let i = 0; i < seed.length; i++) h = (h * 33 + seed.charCodeAt(i)) >>> 0
  return h % 360
}

export function PosterArt({ seed, className }: { seed: string; className?: string }) {
  const hue = hashHue(seed || 'x')
  return (
    <div
      className={`vthumb-art ${className || ''}`}
      style={{
        position: 'absolute', inset: 0,
        background: `radial-gradient(70% 90% at ${28 + (hue % 30)}% 32%, hsla(${hue},42%,48%,.45), transparent 58%),
          radial-gradient(50% 70% at 74% 70%, hsla(${(hue + 40) % 360},35%,42%,.28), transparent 62%),
          linear-gradient(150deg, #2a241c, #12100c 62%, #1c1812)`,
      }}
    />
  )
}

/** 可见时拉播放源,停在指定毫秒,当封面/关键帧。失败则回退到按 seed 生成的画面。 */
export function VideoStill({
  src,
  taskId,
  timeMs = 800,
  seed,
  onDuration,
  className,
}: {
  src?: string | null
  taskId?: number
  timeMs?: number
  seed?: string
  onDuration?: (ms: number) => void
  className?: string
}) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [url, setUrl] = useState<string | null>(src ?? null)
  const [shown, setShown] = useState(false)
  const hostRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (src) setUrl(src)
  }, [src])

  useEffect(() => {
    const el = hostRef.current
    if (!el || shown) return
    const io = new IntersectionObserver(([entry]) => {
      if (entry?.isIntersecting) setShown(true)
    }, { rootMargin: '80px' })
    io.observe(el)
    return () => io.disconnect()
  }, [shown])

  useEffect(() => {
    if (!shown || src || !taskId) return
    let alive = true
    void playbackFor(taskId).then(u => { if (alive) setUrl(u) })
    return () => { alive = false }
  }, [shown, src, taskId])

  useEffect(() => {
    const v = videoRef.current
    if (!v || !url) return
    const seek = () => {
      const t = Math.max(0.05, (timeMs || 800) / 1000)
      try { v.currentTime = Number.isFinite(v.duration) && v.duration > 0 ? Math.min(t, v.duration * 0.2) : t } catch { /* 忽略 */ }
      if (onDuration && Number.isFinite(v.duration) && v.duration > 0) onDuration(v.duration * 1000)
    }
    v.addEventListener('loadedmetadata', seek)
    if (v.readyState >= 1) seek()
    return () => v.removeEventListener('loadedmetadata', seek)
  }, [url, timeMs, onDuration])

  return (
    <div ref={hostRef} className={className} style={{ position: 'relative', overflow: 'hidden', width: '100%', height: '100%' }}>
      <PosterArt seed={seed || String(taskId || '')} />
      {shown && url && (
        <video
          ref={videoRef}
          src={url}
          muted
          playsInline
          preload="metadata"
          style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', objectFit: 'cover' }}
        />
      )}
    </div>
  )
}
