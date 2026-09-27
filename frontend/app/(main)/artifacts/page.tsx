import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { PageHeading } from '@/components/product/PageHeading'
import { ArtifactCard } from '@/components/artifacts/ArtifactCard'
import { ArtifactCreateDialog } from '@/components/artifacts/ArtifactCreateDialog'
import { EmptyState, ErrorState, ProductSkeleton } from '@/components/ui/AsyncState'
import { Icon } from '@/components/ui/Icon'
import { artifactApi, artifactError } from '@/lib/artifacts/api'

export default function ArtifactsPage({ searchParams }: { searchParams: { source?: string } }) {
  useCrumb([{ label: '成果' }])
  const { user } = useShell()
  const [page, setPage] = useState(1)
  const [create, setCreate] = useState(false)
  const sourceId = Number(searchParams.source) || undefined
  const query = useQuery({ queryKey: ['artifacts', sourceId, page], queryFn: ({ signal }) => artifactApi.list(page, sourceId, signal), enabled: !!user })
  const taskQuery = useQuery({ queryKey: ['product-tasks', 1], queryFn: ({ signal }) => artifactApi.tasks(1, signal), enabled: !!user, refetchInterval: 10_000 })
  return <div className="page">
    <PageHeading eyebrow="YOUR KNOWLEDGE, MADE USEFUL" title="让理解，留下来。" description={sourceId ? '与这个视频关联的学习笔记与导图。' : '从视频到笔记，从概念到连接。每一份成果，都保留回到来源的路。'} actions={<button className="btn btn-primary" disabled={!user || user.role === 'DEMO'} onClick={() => setCreate(true)}><Icon name="plus" />生成学习笔记</button>} />
    {query.isPending ? <ProductSkeleton kind="artifacts" /> : query.error ? <ErrorState message={artifactError(query.error)} onRetry={() => void query.refetch()} /> : query.data.list.length ? <><div className="artifact-grid">{query.data.list.map(artifact => <ArtifactCard key={artifact.id} artifact={artifact} runStatus={taskQuery.data?.list.find(task => task.resource_id === artifact.id)?.run?.status} />)}</div><div className="product-pagination"><button className="btn btn-sm" disabled={page === 1} onClick={() => setPage(page - 1)}>上一页</button><span>{page} / {Math.max(1, Math.ceil(query.data.total / 20))} · {query.data.total} 份成果</span><button className="btn btn-sm" disabled={page * 20 >= query.data.total} onClick={() => setPage(page + 1)}>下一页</button></div></> : <EmptyState icon="layers" title="第一份成果，从一个视频开始" desc="选择已有转写的视频，整理成可编辑、可回看的学习笔记。" action={<button className="btn btn-primary" disabled={!user || user.role === 'DEMO'} onClick={() => setCreate(true)}>选择视频</button>} />}
    {create && <ArtifactCreateDialog onClose={() => setCreate(false)} />}
  </div>
}
