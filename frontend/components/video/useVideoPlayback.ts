import { useCallback, useEffect, useRef, useState } from 'react'
import type { VideoPlayerHandle } from '@/components/player/VideoPlayer'
import type { TimelineAtom } from '@/lib/types'
import { useStudyPosition } from '@/lib/artifacts/useStudyPosition'

export function useVideoPlayback(taskId: number) {
  const playerRef = useRef<VideoPlayerHandle>(null)
  const [playheadMs, setPlayheadMs] = useState(0)
  const [videoDurationMs, setVideoDurationMs] = useState(0)
  const [headSnap, setHeadSnap] = useState(false)
  const snapTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const lastPositionWrite = useRef(0)
  const wasPlaying = useRef(false)
  const study = useStudyPosition()
  useEffect(() => () => { clearTimeout(snapTimer.current) }, [])
  const seek = useCallback((ms: number, autoplay = true, cue?: string) => {
    if (!Number.isFinite(ms) || ms < 0) return
    playerRef.current?.seek(ms, autoplay, cue)
    setPlayheadMs(ms)
    setHeadSnap(true)
    clearTimeout(snapTimer.current)
    snapTimer.current = setTimeout(() => setHeadSnap(false), 280)
  }, [])
  const onPlayhead = (ms: number, playing: boolean) => {
    setPlayheadMs(ms)
    if (ms > 0 && (playing || wasPlaying.current) && Date.now() - lastPositionWrite.current > 5000) {
      lastPositionWrite.current = Date.now()
      study.record({ task_id: taskId, artifact_id: '', version_id: '', block_id: '', time_ms: Math.round(ms) })
    }
    if (wasPlaying.current && !playing) void study.flush()
    wasPlaying.current = playing
  }
  const rowAtPlayhead = (a: TimelineAtom) => a.time_range_status !== 'unknown' && Number.isFinite(a.start_ms) && Number.isFinite(a.end_ms) && playheadMs >= a.start_ms && playheadMs < Math.max(a.end_ms, a.start_ms + 1)
  return { studyError: study.error, playerRef, playheadMs, videoDurationMs, setVideoDurationMs, headSnap, seek, onPlayhead, rowAtPlayhead }
}
