import { useEffect, useLayoutEffect, type ComponentProps, type RefObject } from 'react'
import type { EffectiveSummaryView } from '@/lib/types'
import { useShell } from '@/components/shell/AppShell'
import { getSummaryTaskView, patchSummaryTaskView } from '@/lib/summaryViewState'
import { SummaryDocumentView, summaryBlockElementID } from './SummaryDocumentView'

// Both routes render the same effective body and use a version-bound semantic
// reading anchor. A changed document never reuses an old block position.
export function SummaryReadView({ readerRef, onSummaryCommitted, ...props }: ComponentProps<typeof SummaryDocumentView> & { readerRef: RefObject<HTMLElement>; onSummaryCommitted?: (summary: EffectiveSummaryView | null, taskID: number) => void }) {
  const { user } = useShell()
  const digest = props.summary.content_digest
  useLayoutEffect(() => {
    onSummaryCommitted?.(props.summary, props.taskId)
    return () => onSummaryCommitted?.(null, props.taskId)
  }, [props.summary, props.taskId, onSummaryCommitted])
  useEffect(() => {
    const root = readerRef.current
    if (!root || !user?.id) return
    const saved = getSummaryTaskView(user.id, props.taskId).reader
    const frame = requestAnimationFrame(() => {
      if (saved && saved.contentDigest === digest) {
        const chapter = saved.blockID ? root.querySelector<HTMLElement>(`[id="${summaryBlockElementID(saved.blockID)}"]`) : null
        root.scrollTop = chapter ? root.scrollTop + chapter.getBoundingClientRect().top - root.getBoundingClientRect().top - (saved.offset || 0) : saved.scrollTop
      } else if (saved) root.scrollTop = 0
    })
    const save = () => {
      const top = root.getBoundingClientRect().top
      const blocks = Array.from(root.querySelectorAll<HTMLElement>('.summary-chapter'))
      const nearest = [...blocks].reverse().find(block => block.getBoundingClientRect().top <= top + 40) || blocks[0]
      patchSummaryTaskView(user.id, props.taskId, { reader: { scrollTop: root.scrollTop, contentDigest: digest, ...(nearest ? { blockID: decodeURIComponent(nearest.id.slice('summary-block-'.length)), offset: nearest.getBoundingClientRect().top - top } : {}) } })
    }
    root.addEventListener('scroll', save, { passive: true })
    return () => { cancelAnimationFrame(frame); root.removeEventListener('scroll', save) }
  }, [user?.id, props.taskId, digest, readerRef])
  return <SummaryDocumentView {...props} />
}
