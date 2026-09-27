import { useState } from 'react'
import { useRouter } from '@/lib/router'
import { useQuery } from '@tanstack/react-query'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { PageHeading } from '@/components/product/PageHeading'
import { TaskList } from '@/components/artifacts/TaskList'
import { RunDetail } from '@/components/artifacts/RunDetail'
import { EmptyState, ErrorState, LoadingBlock } from '@/components/ui/AsyncState'
import { artifactApi, artifactError } from '@/lib/artifacts/api'

export default function TasksPage({ searchParams }: { searchParams: { run?: string } }) {
  useCrumb([{ label: '任务' }])
  const { user } = useShell()
  const router = useRouter()
  const [page, setPage] = useState(1)
  const query = useQuery({ queryKey: ['product-tasks', page], queryFn: ({ signal }) => artifactApi.tasks(page, signal), enabled: !!user, refetchInterval: 5000 })
  const openRun = (id: string) => router.push(`/tasks?run=${encodeURIComponent(id)}`)
  return <div className="page"><PageHeading eyebrow="WORK CONTINUES IN THE BACKGROUND" title="每一步，都有回音。" description="视频处理与成果生成的进度都在这里。随时离开，再回来继续。" actions={<button className="btn" disabled={query.isFetching} onClick={() => void query.refetch()}>刷新状态</button>} />
    {query.isPending ? <LoadingBlock label="正在读取任务…" variant="card" /> : query.error ? <ErrorState message={artifactError(query.error)} onRetry={() => void query.refetch()} /> : query.data.list.length ? <><TaskList tasks={query.data.list} onOpen={openRun} /><div className="product-pagination"><button className="btn btn-sm" disabled={page === 1} onClick={() => setPage(page - 1)}>上一页</button><span>{page} / {Math.max(1, Math.ceil(query.data.total / 20))}</span><button className="btn btn-sm" disabled={page * 20 >= query.data.total} onClick={() => setPage(page + 1)}>下一页</button></div></> : <EmptyState icon="check" title="暂时没有任务" desc="导入视频或生成学习笔记后，可以在这里查看处理进度。" />}
    {searchParams.run && user && <RunDetail key={searchParams.run} id={searchParams.run} readOnly={user.role === 'DEMO'} onClose={() => router.push('/tasks')} onRun={openRun} />}
  </div>
}
