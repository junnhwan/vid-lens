import Link from '@/lib/router'
import type { ProductTask } from '@/lib/artifacts/schema'
import { runLabels, stageLabels } from '@/lib/artifacts/view'
import { fmtRelTime } from '@/lib/format'
import { Icon } from '@/components/ui/Icon'

const mediaLabels: Record<string, string> = { '0': '待处理', '1': '排队中', '2': '处理中', '3': '处理完成', '4': '处理失败', '5': '已停止' }
export function TaskList({ tasks, onOpen }: { tasks: ProductTask[]; onOpen: (id: string) => void }) {
  return <div className="task-list">{tasks.map(task => <article key={task.id} className="product-task-row"><div className="task-row-icon"><Icon name={task.type === 'artifact_generation' ? 'layers' : 'video'} /></div><div><h2>{task.title}</h2><p>{task.type === 'artifact_generation' ? '学习笔记生成' : '视频处理'} · {fmtRelTime(task.updated_at)}</p>{task.run && <p>来源视频 #{task.run.source_task_id}</p>}</div><div><span className={`task-state ${task.status}`}>{task.run ? runLabels[task.run.status] : mediaLabels[task.status] || '状态待确认'}</span><div className="task-row-stage">{task.run ? stageLabels[task.run.stage] || '后台处理中' : '查看视频获取详细进度'}</div></div><div className="product-actions">{task.run ? <><Link className="btn btn-sm" href={`/video/${task.run.source_task_id}`}>来源视频</Link><button className="btn btn-sm" onClick={() => onOpen(task.run!.id)}>查看详情<Icon name="chev-r" size="sm" /></button>{task.run.result && <Link className="btn btn-sm" href={`/artifacts/${encodeURIComponent(task.run.artifact_id)}${task.run.result.is_candidate ? `?version=${encodeURIComponent(task.run.result.version_id)}&candidate=1` : ''}`}>{task.run.result.is_candidate ? '候选版本' : '打开成果'}</Link>}</> : <Link className="btn btn-sm" href={`/video/${encodeURIComponent(task.resource_id)}`}>打开视频<Icon name="chev-r" size="sm" /></Link>}</div></article>)}</div>
}
