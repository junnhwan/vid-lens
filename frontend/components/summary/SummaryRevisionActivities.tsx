import type { SummaryEditOperation } from '@/lib/types'
import { formatDuration } from '@/lib/duration'

const stateLabel = { running: '进行中', done: '已完成', error: '失败' }
export function SummaryRevisionActivities({ operation }: { operation: SummaryEditOperation }) {
  const activities = operation.activities || []
  if (!activities.length) return null
  const row = (activity: NonNullable<SummaryEditOperation['activities']>[number]) => <li key={activity.id}><span>{activity.title}</span><small>{stateLabel[activity.status]}{activity.status !== 'running' && activity.duration_ms > 0 ? ` · ${formatDuration(activity.duration_ms)}` : ''}</small></li>
  return <div className="sumrev-activities" aria-label="摘要修订实际活动"><ul>{activities.slice(-3).map(row)}</ul>{activities.length > 3 && <details><summary>展开更早的 {activities.length - 3} 项活动</summary><ul>{activities.slice(0,-3).map(row)}</ul></details>}{operation.status === 'proposed' && <p>预览已就绪，等待你确认应用。</p>}</div>
}
