import type { CiteRef } from '../components/Citation'

export interface CitationSourceGroup {
  key: string
  citations: CiteRef[]
}

// Group playback sources, not claims. Every citation keeps its own quote and
// support result. Unknown times cannot prove that two sources are the same.
export function groupCitationSources(cites: CiteRef[], fallbackTaskId?: number): CitationSourceGroup[] {
  const groups = new Map<string, CitationSourceGroup>()
  cites.forEach((cite, index) => {
    const task = cite.taskId || fallbackTaskId
    // Titles are not identities: two different videos may have the same name.
    const video = task ? `task:${task}` : ''
    const timed = cite.timeRangeStatus !== 'unknown' && Number.isFinite(cite.startMS) && Number.isFinite(cite.endMS) && cite.startMS! >= 0 &&
      (cite.endMS! > cite.startMS! || (cite.endMS === cite.startMS && cite.timeRangeStatus === 'exact' && (cite.modality === 'visual_ocr' || cite.modality === 'visual_caption')))
    const key = video && timed
      ? JSON.stringify([video, cite.modality || 'transcript', cite.timeRangeStatus || 'legacy', cite.startMS, cite.endMS])
      : `individual:${index}`
    const group = groups.get(key)
    if (group) group.citations.push(cite)
    else groups.set(key, { key, citations: [cite] })
  })
  return [...groups.values()]
}
