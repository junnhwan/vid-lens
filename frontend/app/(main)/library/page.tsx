import { useEffect, useState } from 'react'
import { useSearchParams } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { VideoCard } from '@/components/VideoCard'
import { CardSkeleton, EmptyState, ErrorState } from '@/components/ui/AsyncState'
import { useCrumb, useShell } from '@/components/shell/AppShell'
import { PageHeading } from '@/components/product/PageHeading'
import { Icon } from '@/components/ui/Icon'
import Link from '@/lib/router'
import { fmtRelTime, fmtSize, taskTitle, sourceLabel } from '@/lib/format'
import { taskStateView } from '@/lib/taskStatus'

const PAGE_SIZE = 24
const filters = [{ key: 'all', label: '全部' }, { key: 'ready', label: '已有内容' }, { key: 'pending', label: '待处理' }, { key: 'processing', label: '处理中' }, { key: 'failed', label: '失败' }]

export default function LibraryPage() {
  const { openUpload, uploadRevision } = useShell()
  useCrumb([{ label: '视频库' }])
  const [params, setParams] = useSearchParams()
  const pageNumber = Number(params.get('page'))
  const page = Number.isFinite(pageNumber) ? Math.max(1, Math.floor(pageNumber) || 1) : 1
  const view = params.get('view') === 'list' ? 'list' : 'grid'
  const filter = filters.some(f => f.key === params.get('activity')) ? params.get('activity')! : 'all'
  const keyword = params.get('q') || ''
  const [draft, setDraft] = useState(keyword)
  useEffect(() => { setDraft(keyword) }, [keyword])
  useEffect(() => {
    if (draft === keyword) return
    const timer = window.setTimeout(() => {
      setParams(previous => { const next = new URLSearchParams(previous); next.set('q', draft); next.delete('page'); return next }, { replace: true })
    }, 250)
    return () => window.clearTimeout(timer)
  }, [draft, keyword, setParams])
  const query = useQuery({ queryKey: ['library-videos', page, keyword, filter, uploadRevision], queryFn: () => api.listTasks(page, PAGE_SIZE, keyword, filter), refetchInterval: query => query.state.data?.list.some(t => t.status === 1 || t.status === 2) ? 5000 : false })
  const tasks = query.data?.list || []
  const total = query.data?.total || 0
  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  useEffect(() => {
    if (query.data && page > pages) setParams(previous => { const next = new URLSearchParams(previous); next.set('page', String(pages)); return next }, { replace: true })
  }, [query.data, page, pages, setParams])
  function setPage(nextPage: number) { setParams(previous => { const next = new URLSearchParams(previous); next.set('page', String(nextPage)); return next }) }
  return <div className="page page-wide">
    <PageHeading title="视频库" description={query.error && !query.data ? '视频资料读取失败，重试后可查看数量' : query.isPending ? '正在读取视频资料…' : `${total} 个视频${keyword || filter !== 'all' ? '符合当前条件' : ''} · 已有内容可阅读，检索能力按当前 AI 配置确认`} actions={<button className="btn btn-primary" onClick={openUpload}><Icon name="plus" />导入视频</button>} />
    <div className="lib-toolbar">
      <input aria-label="搜索视频" className="input" placeholder="搜索全部视频的标题或文件名…" value={draft} onChange={e => setDraft(e.target.value)} />
      <div className="seg">{filters.map(f => <button key={f.key} aria-pressed={filter === f.key} className={filter === f.key ? 'on' : ''} onClick={() => setParams(previous => { const next = new URLSearchParams(previous); next.set('activity', f.key); next.delete('page'); return next })}>{f.label}</button>)}</div>
      <div className="seg lib-view" aria-label="视频库视图">{([{key:'grid',label:'卡片'}, {key:'list',label:'列表'}] as const).map(item => <button key={item.key} aria-pressed={view === item.key} className={view === item.key ? 'on' : ''} onClick={() => setParams(previous => { const next = new URLSearchParams(previous); next.set('view', item.key); return next }, {replace:true})}>{item.label}</button>)}</div>
    </div>
    {query.error && <ErrorState message="视频列表加载失败" onRetry={() => void query.refetch()} />}
    {query.isPending && <CardSkeleton count={8} />}
    {!!tasks.length && (view === 'grid' ? <div className="video-grid">{tasks.map(task => <VideoCard key={task.id} task={task} />)}</div> : <div className="library-list">{tasks.map(task => { const state=taskStateView(task); return <Link className="library-row" key={task.id} href={`/video/${task.id}`}><div><strong>{taskTitle(task)}</strong><span>{sourceLabel(task)} · {fmtSize(task.file_size)}</span></div><span className={`chip ${state.chip}`}>{state.text}</span><span className="library-capability">{task.retrievable ? '可检索问答' : task.has_transcription ? '转写可阅读' : '检索未就绪'}</span><span className="library-updated">{fmtRelTime(task.updated_at)}</span><Icon name="chev-r" size="sm" /></Link> })}</div>)}
    {!query.isPending && !query.error && !tasks.length && <EmptyState icon={keyword || filter !== 'all' ? 'search' : 'video'} title={keyword || filter !== 'all' ? '没有匹配的视频' : '还没有视频'} action={!keyword && filter === 'all' ? <button className="btn btn-primary" onClick={openUpload}>导入视频</button> : undefined} />}
    {total > 0 && <nav className="product-pagination" aria-label="视频库分页"><button className="btn btn-sm" disabled={page <= 1 || query.isFetching} onClick={() => setPage(page - 1)}>上一页</button><span>{page} / {pages} · 共 {total} 个</span><button className="btn btn-sm" disabled={page >= pages || query.isFetching} onClick={() => setPage(page + 1)}>下一页</button></nav>}
  </div>
}
