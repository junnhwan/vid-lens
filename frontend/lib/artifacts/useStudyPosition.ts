import { useCallback, useEffect, useRef, useState } from 'react'
import { artifactApi } from './api'
import type { LearningPosition } from './schema'

type Target = Pick<LearningPosition, 'task_id' | 'artifact_id' | 'version_id' | 'block_id' | 'time_ms'>

// One in-flight write per tab. The database revision protects against another
// tab and against a response arriving after a newer location was saved.
export function useStudyPosition() {
  const [position, setPosition] = useState<LearningPosition | null>(null)
  const [error, setError] = useState('')
  const revision = useRef<number | null>(null)
  const pending = useRef<Target | null>(null)
  const busy = useRef(false)
  const timer = useRef<number | undefined>(undefined)
  const alive = useRef(true)
  const flush = useCallback(async () => {
    if (busy.current || revision.current === null || !pending.current) return
    busy.current = true
    const value = pending.current
    pending.current = null
    try {
      const saved = await artifactApi.savePosition({ ...value, expected_revision: revision.current })
      revision.current = saved.revision
      if (alive.current) { setPosition(saved); setError('') }
    } catch {
      pending.current = null
      if (alive.current) setError('学习位置有更新或网络暂不可用，请刷新后继续。')
      try { const current = await artifactApi.position(); revision.current = current?.revision ?? 0; if (alive.current) setPosition(current) } catch { /* Keep the error visible. */ }
    } finally {
      busy.current = false
      if (pending.current) void flush()
    }
  }, [])
  const record = useCallback((value: Target) => {
    pending.current = value
    if (timer.current) window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => void flush(), 3000)
  }, [flush])
  useEffect(() => {
    alive.current = true
    void artifactApi.position().then(value => { if (alive.current) { revision.current = value?.revision ?? 0; setPosition(value); void flush() } }).catch(() => { if (alive.current) setError('学习位置暂时无法同步') })
    return () => { if (timer.current) window.clearTimeout(timer.current); void flush(); alive.current = false }
  }, [flush])
  useEffect(() => {
    const onHide = () => { if (document.visibilityState==='hidden') { if (timer.current) window.clearTimeout(timer.current); void flush() } }
    document.addEventListener('visibilitychange', onHide)
    return () => document.removeEventListener('visibilitychange', onHide)
  }, [flush])
  return { position, error, record, flush }
}
