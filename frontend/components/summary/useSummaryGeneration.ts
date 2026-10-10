import { useEffect, useState } from 'react'
import { summaryExperienceApi, type SummaryGeneration } from '@/lib/summaryExperience'

export function useSummaryGeneration(taskId: number, refreshKey = '') {
  const [generation, setGeneration] = useState<SummaryGeneration | null>(null), [error, setError] = useState('')
  useEffect(() => {
    setGeneration(null);setError('')
    const controller = new AbortController(); let timer: ReturnType<typeof setTimeout> | undefined
    let selected = '', cursor = 0
    const poll = async () => {
      try {
        const next = await summaryExperienceApi.generation(taskId, controller.signal)
        if (controller.signal.aborted) return
        if (selected !== next.generation_id) { selected = next.generation_id; cursor = 0 }
        if (next.generation_id && next.event_high_watermark > cursor) {
          let hasMore = true
          while (hasMore && !controller.signal.aborted) {
            const page = await summaryExperienceApi.events(taskId, next.generation_id, cursor, controller.signal)
            if (controller.signal.aborted) return
            if (page.cursor_gap) { cursor = next.event_high_watermark; break }
            if (page.next_after_seq <= cursor) break
            cursor = page.next_after_seq; hasMore = page.has_more
          }
        }
        if (controller.signal.aborted) return
        // Snapshot is authoritative and includes exactly the begun activities;
        // events provide ordered replay/gap detection, never invented progress.
        setGeneration(next); setError('')
        if (['pending', 'queued', 'running', 'retry_waiting'].includes(next.status)) timer = setTimeout(() => void poll(), 1500)
      } catch (reason) {
        if (controller.signal.aborted) return
        setError(reason instanceof Error ? reason.message : '执行记录暂不可用')
        timer = setTimeout(() => void poll(), 4000)
      }
    }
    void poll()
    return () => { controller.abort(); if (timer) clearTimeout(timer) }
  }, [taskId, refreshKey])
  return { generation, error }
}
