import { Icon } from '@/components/ui/Icon'

export function QuestionSuggestionsLoading({ followup = false }: { followup?: boolean }) {
  return <div className={`vq-loading${followup ? ' vq-loading-compact' : ''}`} role="status" aria-live="polite">
    <div className="vq-loading-label"><Icon name="bulb" size="sm" /><div><strong>{followup ? '正在整理追问' : '正在整理推荐问题'}</strong><span>{followup ? '沿着这轮回答，继续深入' : '根据这段视频，挑选值得问的内容'}</span></div><span className="vq-loading-dots" aria-hidden="true"><i /></span></div>
    <div className="vq-skeleton-list" aria-hidden="true">{(followup ? [0, 1] : [0, 1, 2]).map(i => <div className="vq-skeleton-row" key={i}><i /><span /><b /></div>)}</div>
  </div>
}
