import type { SummaryContextRef } from '@/lib/summaryExperience'
import { summaryVersionLabel, type SummaryAnnotation } from '@/lib/summaryAnnotations'
import './SummaryContextCards.css'

export function SummaryContextCards({ refs, saved = false, fallbackTitle, onRemove, onReturn }: { refs?: (SummaryContextRef | SummaryAnnotation)[]; saved?: boolean; fallbackTitle: string; onRemove?: (index: number) => void; onReturn?: (index: number) => void }) {
  if (!refs?.length) return null
  return <div className="summary-context-cards" aria-label={saved ? '已保存的摘要选段' : '本次提问的摘要选段'}>{refs.map((ref, index) => <div className="summary-context-card" key={`${ref.task_id}-${ref.block_id}-${index}`}>
    <details><summary><span>{ref.kind === 'summary_screenshot' ? '已验证图注，未输入原图' : '摘要选段'}</span><b>{'source_title' in ref ? ref.source_title : fallbackTitle}</b><small>{summaryVersionLabel(ref)}{saved ? ' · 已保存快照' : ''}</small></summary>
      {'block_title' in ref && <small>{ref.block_title}</small>}<blockquote>{ref.quote}</blockquote>
    </details>{onReturn && <button type="button" className="btn btn-sm btn-ghost" aria-label={`返回摘要选段 ${index + 1}`} onClick={() => onReturn(index)}>返回</button>}{onRemove && <button type="button" className="btn btn-sm btn-ghost" aria-label={`移除摘要选段 ${index + 1}`} onClick={() => onRemove(index)}>移除</button>}
  </div>)}</div>
}
