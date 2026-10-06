import Link from '@/lib/router'
import { useQuery } from '@tanstack/react-query'
import { artifactApi } from '@/lib/artifacts/api'
import { runLabels } from '@/lib/artifacts/view'
import { useShell } from '@/components/shell/AppShell'
import { Icon } from '@/components/ui/Icon'
import { ArtifactCard } from './ArtifactCard'

export function RecentProductWork({ hideEmpty = false }: { hideEmpty?: boolean }) {
  const { user } = useShell()
  const artifacts = useQuery({ queryKey: ['artifacts', undefined, 1], queryFn: ({ signal }) => artifactApi.list(1, undefined, signal), enabled: !!user, refetchInterval: 10_000 })
  const tasks = useQuery({ queryKey: ['product-tasks', 1], queryFn: ({ signal }) => artifactApi.tasks(1, signal), enabled: !!user, refetchInterval: 10_000 })
  const active = tasks.data?.list.filter(task => task.run && ['pending', 'running'].includes(task.run.status)) ?? []
  if (hideEmpty && !artifacts.error && !artifacts.data?.list.length && !active.length) return null
  return <>
    {active.length > 0 && <section className="dashboard-generation"><div><Icon name="activity" /><b>学习笔记在后台继续整理</b></div>{active.slice(0, 3).map(task => <Link key={task.id} href={`/tasks?run=${encodeURIComponent(task.run!.id)}`}><span>{task.title}</span><small>{runLabels[task.run!.status]}<Icon name="chev-r" size="sm" /></small></Link>)}</section>}
    <div className="section-head"><h2>最近成果</h2><Link className="more" href="/artifacts">全部成果<Icon name="chev-r" size="sm" /></Link></div>
    {artifacts.error ? <div className="product-inline-state" role="alert">成果暂时无法读取。<button className="btn btn-sm" onClick={() => void artifacts.refetch()}>重试</button></div> : artifacts.isPending ? <div className="product-inline-state" role="status">正在读取成果…</div> : artifacts.data.list.length ? <div className="artifact-grid">{artifacts.data.list.slice(0, 3).map(artifact => <ArtifactCard key={artifact.id} artifact={artifact} />)}</div> : <div className="product-inline-state"><div><b>整理一份学习笔记</b><p>从已保存的转写或画面内容开始，生成可编辑的笔记和导图。</p></div><Link className="btn" href="/artifacts">创建第一份成果<Icon name="chev-r" size="sm" /></Link></div>}
  </>
}
