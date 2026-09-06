'use client'

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from 'react'
import { Icon } from '@/components/ui/Icon'
import { formatTime } from '@/components/Citation'

// 可复用视频播放器:工作台主播放器与聊天页迷你播放器共用。
// 播放源是 GET /media/task/:id/playback 返回的短时效签名 URL;没有 URL 或加载失败时
// 显示 .player-fallback,不做任何假播放。时间一律以毫秒对外,内部换算秒驱动 <video>。

export interface VideoPlayerHandle {
  /** 跳转到指定毫秒;autoplay 时若暂停中则自动播放 */
  seek: (ms: number, autoplay?: boolean) => void
  currentTime: () => number
  playing: () => boolean
  pause: () => void
}

interface VideoPlayerProps {
  src: string | null
  /** HUD 左上角展示的标题(时间码 · 标题) */
  title?: string
  /** 迷你变体(聊天右栏):更小的按钮与内边距 */
  compact?: boolean
  /** 无播放源 / 加载失败时兜底文案 */
  fallbackText?: string
  /** 播放头变化回调(≈4Hz 节流),供时间轴联动 */
  onPlayhead?: (ms: number, playing: boolean) => void
  /** 播放源加载失败时调用(签名 URL 过期后重新获取);返回新 URL 则原位恢复播放,返回 null 判定为不可用 */
  onNeedRefresh?: () => Promise<string | null>
  className?: string
}

const PLAYHEAD_NOTIFY_MS = 250

export const VideoPlayer = forwardRef<VideoPlayerHandle, VideoPlayerProps>(
  function VideoPlayer({ src, title, compact, fallbackText, onPlayhead, onNeedRefresh, className }, ref) {
    const videoRef = useRef<HTMLVideoElement | null>(null)
    const fillRef = useRef<HTMLDivElement | null>(null)
    const curRef = useRef<HTMLDivElement | null>(null)
    const timeRef = useRef<HTMLElement | null>(null)
    const hudRef = useRef<HTMLDivElement | null>(null)
    const rafRef = useRef(0)
    const draggingRef = useRef(false)
    const lastNotifyRef = useRef(0)
    const onPlayheadRef = useRef(onPlayhead)
    onPlayheadRef.current = onPlayhead

    const [durationMs, setDurationMs] = useState(0)
    const [playing, setPlaying] = useState(false)
    const [failed, setFailed] = useState(false)
    const [srcOverride, setSrcOverride] = useState<string | null>(null)
    const activeSrc = srcOverride ?? src
    const playable = !!activeSrc && !failed
    // 签名 URL 过期原位恢复:换源后把播放头与播放状态还原
    const resumeRef = useRef<{ ms: number; autoplay: boolean } | null>(null)
    const onNeedRefreshRef = useRef(onNeedRefresh)
    onNeedRefreshRef.current = onNeedRefresh

    const paint = useCallback(() => {
      const video = videoRef.current
      if (!video) return
      const ms = video.currentTime * 1000
      const dur = durationMs || (Number.isFinite(video.duration) ? video.duration * 1000 : 0)
      if (dur > 0) {
        const pct = `${Math.min(100, (ms / dur) * 100)}%`
        if (fillRef.current) fillRef.current.style.width = pct
        if (curRef.current) curRef.current.style.left = pct
      }
      if (timeRef.current) timeRef.current.textContent = formatTime(ms)
      if (hudRef.current) hudRef.current.textContent = `${formatTime(ms)}${title ? ` · ${title}` : ''}`
    }, [durationMs, title])

    // 播放头:用 rAF 直接写 DOM(避免 60Hz 重渲染),对父级按 ~4Hz 节流通知
    useEffect(() => {
      const loop = () => {
        const video = videoRef.current
        if (video) {
          paint()
          const ms = video.currentTime * 1000
          const isPlaying = !video.paused && !video.ended
          const now = performance.now()
          if (onPlayheadRef.current && now - lastNotifyRef.current > PLAYHEAD_NOTIFY_MS) {
            lastNotifyRef.current = now
            onPlayheadRef.current(ms, isPlaying)
          }
        }
        rafRef.current = requestAnimationFrame(loop)
      }
      rafRef.current = requestAnimationFrame(loop)
      return () => cancelAnimationFrame(rafRef.current)
    }, [paint])

    useEffect(() => {
      setSrcOverride(null)
      setFailed(false)
    }, [src])

    const handleVideoError = useCallback(() => {
      if (!onNeedRefreshRef.current) {
        setFailed(true)
        return
      }
      void (async () => {
        const video = videoRef.current
        const resume = video ? { ms: video.currentTime * 1000, autoplay: !video.paused } : null
        try {
          const fresh = await onNeedRefreshRef.current?.()
          if (fresh && fresh !== (videoRef.current?.currentSrc || videoRef.current?.src || '')) {
            resumeRef.current = resume
            setSrcOverride(fresh)
            return
          }
        } catch { /* 刷新失败按不可用处理 */ }
        setFailed(true)
      })()
    }, [])

    const seek = useCallback((ms: number, autoplay = false) => {
      const video = videoRef.current
      if (!video || !playable) return
      const dur = Number.isFinite(video.duration) && video.duration > 0 ? video.duration * 1000 : durationMs
      const clamped = Math.max(0, Math.min(dur > 0 ? dur - 250 : ms, ms))
      try {
        video.currentTime = clamped / 1000
      } catch { /* 元数据未就绪时忽略,loadedmetadata 后由播放头自然同步 */ }
      if (autoplay && video.paused) void video.play().catch(() => {})
      paint()
      lastNotifyRef.current = 0
    }, [playable, durationMs, paint])

    useImperativeHandle(ref, () => ({
      seek,
      currentTime: () => (videoRef.current ? videoRef.current.currentTime * 1000 : 0),
      playing: () => !!videoRef.current && !videoRef.current.paused && !videoRef.current.ended,
      pause: () => videoRef.current?.pause(),
    }), [seek])

    const toggle = useCallback(() => {
      const video = videoRef.current
      if (!video || !playable) return
      if (video.paused) void video.play().catch(() => {})
      else video.pause()
    }, [playable])

    const seekFromPointer = useCallback((clientX: number, target: HTMLElement) => {
      const rect = target.getBoundingClientRect()
      const ratio = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width))
      seek(ratio * (durationMs || 0), playing)
    }, [seek, durationMs, playing])

    const onScrubDown = (e: React.PointerEvent<HTMLDivElement>) => {
      if (!playable) return
      draggingRef.current = true
      e.currentTarget.setPointerCapture(e.pointerId)
      seekFromPointer(e.clientX, e.currentTarget)
    }
    const onScrubMove = (e: React.PointerEvent<HTMLDivElement>) => {
      if (!draggingRef.current) return
      seekFromPointer(e.clientX, e.currentTarget)
    }
    const onScrubUp = (e: React.PointerEvent<HTMLDivElement>) => {
      draggingRef.current = false
      e.currentTarget.releasePointerCapture(e.pointerId)
    }

    return (
      <div className={`player-card${compact ? ' compact' : ''}${className ? ` ${className}` : ''}`}>
        <div className={`player-stage${playable ? '' : ' novideo'}`}>
          {activeSrc && (
            <video
              ref={videoRef}
              src={activeSrc}
              playsInline
              preload="metadata"
              onLoadedMetadata={e => {
                const d = e.currentTarget.duration
                if (Number.isFinite(d)) setDurationMs(d * 1000)
                const resume = resumeRef.current
                if (resume) {
                  resumeRef.current = null
                  try { e.currentTarget.currentTime = resume.ms / 1000 } catch { /* 忽略 */ }
                  if (resume.autoplay) void e.currentTarget.play().catch(() => {})
                }
                paint()
              }}
              onPlay={() => setPlaying(true)}
              onPause={() => setPlaying(false)}
              onEnded={() => setPlaying(false)}
              onError={handleVideoError}
            />
          )}
          <div className="player-fallback">
            <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center', textAlign: 'center', color: 'var(--tx-3)' }}>
              <div>
                <Icon name="video" size="lg" />
                <div style={{ marginTop: 8, fontSize: 12.5 }}>
                  {failed ? '播放源加载失败,请稍后重试' : fallbackText || '暂无可用播放源'}
                </div>
              </div>
            </div>
          </div>
          <div className="player-hud mono" ref={hudRef}>00:00{title ? ` · ${title}` : ''}</div>
        </div>
        <div className="player-controls">
          <button className="pp-btn" disabled={!playable} onClick={toggle} aria-label={playing ? '暂停' : '播放'}>
            <Icon name={playing ? 'pause' : 'play'} />
          </button>
          <div
            className="scrub"
            style={playable ? undefined : { pointerEvents: 'none', opacity: 0.5 }}
            onPointerDown={onScrubDown}
            onPointerMove={onScrubMove}
            onPointerUp={onScrubUp}
          >
            <div className="scrub-track"><div className="scrub-fill" ref={fillRef} style={{ width: '0%' }} /></div>
            <div className="cur" ref={curRef} style={{ left: '0%' }} />
          </div>
          <div className="ptime mono">
            <b ref={node => { timeRef.current = node }}>00:00</b>
            <span> / {durationMs > 0 ? formatTime(durationMs) : '--:--'}</span>
          </div>
        </div>
      </div>
    )
  },
)
