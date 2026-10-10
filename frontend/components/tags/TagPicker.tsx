import { useEffect, useState } from 'react'
import { summaryTagsApi, tagError, type UserTag } from '@/lib/summaryTags'

export function TagPicker({ onSelect, selected = [], disabled = false }: { onSelect: (tag: UserTag) => void; selected?: string[]; disabled?: boolean }) {
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [rows, setRows] = useState<UserTag[]>([])
  const [total, setTotal] = useState(0)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  useEffect(() => {
    const controller = new AbortController()
    setLoading(true); setError('')
    const timer = window.setTimeout(() => { void summaryTagsApi.list(page, search, controller.signal).then(result => {
      if (!controller.signal.aborted) { setRows(result.list || []); setTotal(result.total) }
    }).catch(error => { if (!controller.signal.aborted) setError(tagError(error)) }).finally(() => { if (!controller.signal.aborted) setLoading(false) }) }, 180)
    return () => { controller.abort(); window.clearTimeout(timer) }
  }, [page, search])
  return <div className="tag-picker"><input className="input" aria-label="搜索标签词表" value={search} placeholder="搜索标签或已确认的别名" onChange={event => { setSearch(event.target.value); setPage(1) }} />
    {error && <p role="alert">{error}</p>}{loading ? <p role="status">正在读取标签…</p> : <div className="tag-chips">{rows.map(tag => <button className="btn btn-sm" key={tag.id} disabled={disabled || selected.includes(tag.id)} onClick={() => onSelect(tag)}>{tag.display_name}<small> · {tag.video_count} 个视频</small></button>)}{!rows.length && !error && <small>没有匹配的标签</small>}</div>}
    {total > 50 && <div className="tag-pagination"><button className="btn btn-sm" disabled={loading || page === 1} onClick={() => setPage(page - 1)}>上一页标签</button><span>{page} / {Math.ceil(total / 50)}</span><button className="btn btn-sm" disabled={loading || page * 50 >= total} onClick={() => setPage(page + 1)}>下一页标签</button></div>}
  </div>
}
