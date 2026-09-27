import Link from '@/lib/router'
import type { ProductTask } from '@/lib/artifacts/schema'
import { runLabels, stageLabels } from '@/lib/artifacts/view'
import { fmtRelTime } from '@/lib/format'
import { Icon } from '@/components/ui/Icon'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { ProcessStrip } from '@/components/ProcessStrip'
import { TranscriptionProgressPanel } from '@/components/TranscriptionProgressPanel'
import { VisualProgressPanel } from '@/components/VisualProgressPanel'

function MediaTaskProgress({ id }: { id: string }) {
  const taskID=Number(id)
  const query=useQuery({ queryKey:['media-task-progress',taskID], queryFn:()=>api.getTask(taskID), enabled:Number.isSafeInteger(taskID)&&taskID>0, refetchInterval:5000 })
  if (query.error) return <p role="status">详细进度暂不可用。<button className="btn btn-sm" onClick={() => void query.refetch()}>重试</button></p>
  if (!query.data) return null
  const task=query.data
  return <div><ProcessStrip status={task.status} stage={task.stage} has_transcription={task.has_transcription} last_job_type={task.last_job_type} has_rag_index={task.has_rag_index} visual_status={task.visual_status} />{task.stage==='transcribing' && <><TranscriptionProgressPanel task={task} compact /><VisualProgressPanel task={task} compact /></>}</div>
}

const mediaLabels: Record<string, string> = { '0': '待处理', '1': '排队中', '2': '处理中', '3': '处理完成', '4': '处理失败', '5': '重试已耗尽' }
export function TaskList({ tasks, onOpen }: { tasks: ProductTask[]; onOpen: (id: string) => void }) {
  return <div className="task-list">{tasks.map(task => <article key={task.id} className="product-task-row"><div className="task-row-icon"><Icon name={task.type === 'artifact_generation' ? 'layers' : 'video'} /></div><div><h2>{task.title}</h2><p>{task.type === 'artifact_generation' ? '学习笔记生成' : '视频处理'} · {fmtRelTime(task.updated_at)}</p>{task.run && <p>来源视频 #{task.run.source_task_id}</p>}{task.type==='video_processing' && <MediaTaskProgress id={task.resource_id} />}</div><div><span className={`task-state ${task.status}`}>{task.run ? runLabels[task.run.status] : mediaLabels[task.status] || '状态待确认'}</span><div className="task-row-stage">{task.run ? stageLabels[task.run.stage] || '后台处理中' : '服务器处理状态'}</div></div><div className="product-actions">{task.run ? <><Link className="btn btn-sm" href={`/video/${task.run.source_task_id}`}>来源视频</Link><button className="btn btn-sm" onClick={() => onOpen(task.run!.id)}>查看详情<Icon name="chev-r" size="sm" /></button>{task.run.result && <Link className="btn btn-sm" href={`/artifacts/${encodeURIComponent(task.run.artifact_id)}${task.run.result.is_candidate ? `?version=${encodeURIComponent(task.run.result.version_id)}&candidate=1` : ''}`}>{task.run.result.is_candidate ? '候选版本' : '打开成果'}</Link>}</> : <Link className="btn btn-sm" href={`/video/${encodeURIComponent(task.resource_id)}`}>打开视频<Icon name="chev-r" size="sm" /></Link>}</div></article>)}</div>
}
