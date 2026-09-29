import { Fragment, useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, ApiError } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import { taskTitle } from '@/lib/format'
import { Modal } from '@/components/ui/Modal'
import { ErrorState, LoadingBlock } from '@/components/ui/AsyncState'

export default function KBModal({ mode, kb, taskId, indexed, onClose, onChanged }: {
  mode: 'create' | 'manage' | 'assign'; kb?: KnowledgeBase; taskId?: number; indexed?: boolean
  onClose: () => void; onChanged: () => void
}) {
  const [name, setName] = useState(kb?.name || '')
  const [description, setDescription] = useState(kb?.description || '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [memberIds, setMemberIds] = useState<Set<number>>(new Set())
  const [filter, setFilter] = useState('')
  const [page, setPage] = useState(1)
  const [pending, setPending] = useState<number | null>(null)
  const videos = useQuery({ queryKey: ['kb-select-videos', page, filter], queryFn: () => api.listTasks(page, 20, filter), enabled: mode === 'manage' })
  const libraries = useQuery({ queryKey: ['kb-assign-libraries', taskId], queryFn: async () => {
    const list = await api.listKBs()
    return Promise.all(list.map(item => api.getKB(item.id)))
  }, enabled: mode === 'assign' && !!taskId })
  useEffect(() => { if (mode === 'manage') setMemberIds(new Set((kb?.videos || []).map(v => v.task_id))) }, [mode, kb])
  useEffect(() => { if (libraries.data) setMemberIds(new Set(libraries.data.filter(item => item.videos?.some(v => v.task_id === taskId)).map(item => item.id))) }, [libraries.data, taskId])
  async function create() {
    if (!name.trim()) { setErr('请输入知识库名称'); return }
    setBusy(true); setErr('')
    try { await api.createKB(name.trim(), description.trim()); onChanged(); onClose() }
    catch (e) { setErr(e instanceof ApiError ? e.message : '创建失败') }
    finally { setBusy(false) }
  }
  async function toggleMember(id: number) {
    if (pending !== null) return
    setPending(id); setErr('')
    const isMember = memberIds.has(id)
    try {
      if (mode === 'manage' && kb) {
        if (isMember) await api.removeKBVideo(kb.id, id); else await api.addKBVideo(kb.id, id)
      } else if (mode === 'assign' && taskId) {
        if (isMember) await api.removeKBVideo(id, taskId); else await api.addKBVideo(id, taskId)
      } else return
      setMemberIds(previous => { const next = new Set(previous); if (isMember) next.delete(id); else next.add(id); return next })
      onChanged()
    } catch (e) { setErr(e instanceof ApiError ? e.message : '成员更新失败') }
    finally { setPending(null) }
  }
  const groupFor = (id:number, retrievable?:boolean) => memberIds.has(id) ? 0 : retrievable ? 1 : 2
  const taskList = [...(videos.data?.list || [])].sort((a, b) => groupFor(a.id,a.retrievable) - groupFor(b.id,b.retrievable))
  const kbList = (libraries.data || []).filter(item => item.name.toLocaleLowerCase().includes(filter.toLocaleLowerCase()))
  const reading = mode === 'manage' ? videos.isPending : libraries.isPending
  const readError = mode === 'manage' ? videos.error : libraries.error
  return <Modal title={mode === 'create' ? '新建知识库' : mode === 'manage' ? `加入视频 · ${kb?.name}` : '加入知识库'} onClose={onClose} confirmOnClose={mode === 'create' && !!(name || description)} width={560}
    footer={mode === 'create' ? <><button className="btn" onClick={onClose}>取消</button><button className="btn btn-primary" disabled={busy} onClick={() => void create()}>创建</button></> : <button className="btn btn-primary" disabled={pending !== null} onClick={onClose}>完成</button>}>
    {mode === 'create' ? <>
      <label className="field-label" htmlFor="kb-name">名称</label><input id="kb-name" className="input" value={name} onChange={e => setName(e.target.value)} autoFocus />
      <label className="field-label" htmlFor="kb-description">描述</label><textarea id="kb-description" className="input" rows={3} value={description} onChange={e => setDescription(e.target.value)} />
    </> : <>
      <p className="product-description">{mode === 'manage' ? '只有当前默认向量模型下可检索的视频才能加入。已有成员即使不可检索，也可以移出。' : indexed === false ? '当前视频尚不可检索。请先建立当前模型的索引；仍可从已有知识库移出。' : '勾选知识库加入，取消勾选移出。'}</p>
      <input className="input" aria-label={mode === 'manage' ? '搜索可加入的视频' : '搜索知识库'} placeholder={mode === 'manage' ? '搜索全部视频…' : '按知识库名称过滤…'} value={filter} onChange={e => { setFilter(e.target.value); setPage(1) }} />
      {reading ? <LoadingBlock label="正在读取可选资料…" /> : readError ? <ErrorState message="资料加载失败" onRetry={() => { void (mode === 'manage' ? videos.refetch() : libraries.refetch()) }} /> : <div className="kb-selection-list">
        {mode === 'manage' ? taskList.length ? taskList.map((task, index) => {
          const checked = memberIds.has(task.id)
          const blocked = !checked && task.retrievable !== true
          const group=groupFor(task.id,task.retrievable), previous=taskList[index-1]
          return <Fragment key={task.id}>{(!previous || group !== groupFor(previous.id,previous.retrievable)) && <h4 className="kb-selection-heading">{['本页已有成员','本页可加入','本页暂不可加入'][group]}</h4>}<label className={`kb-member-row${blocked ? ' unavailable' : ''}`}><input type="checkbox" checked={checked} disabled={blocked || pending !== null} onChange={() => void toggleMember(task.id)} /><span className="nm"><b>{taskTitle(task)}</b><span>{pending === task.id ? '正在更新…' : checked ? task.retrievable ? '已加入 · 可检索' : '已加入 · 不可检索，可移出' : task.retrievable ? '可加入 · 当前模型索引就绪' : '暂不可加入 · 当前模型索引未就绪'}</span></span></label></Fragment>
        }) : <p className="product-description">没有匹配的视频。</p> : kbList.length ? kbList.map(item => {
          const checked = memberIds.has(item.id)
          return <label key={item.id} className="kb-member-row"><input type="checkbox" checked={checked} disabled={pending !== null || (!checked && indexed !== true)} onChange={() => void toggleMember(item.id)} /><span className="nm"><b>{item.name}</b><span>{pending === item.id ? '正在更新…' : `${item.member_count} 个视频`}</span></span></label>
        }) : <p className="product-description">{filter ? '没有匹配的知识库。' : '还没有知识库。'}</p>}
      </div>}
      {mode === 'manage' && videos.data && videos.data.total > 20 && <nav className="product-pagination" aria-label="可选视频分页"><button className="btn btn-sm" disabled={page === 1 || pending !== null} onClick={() => setPage(page - 1)}>上一页</button><span>{page} / {Math.ceil(videos.data.total / 20)}</span><button className="btn btn-sm" disabled={page * 20 >= videos.data.total || pending !== null} onClick={() => setPage(page + 1)}>下一页</button></nav>}
    </>}
    {err && <p className="form-err" role="alert">{err}</p>}
  </Modal>
}
