import { useEffect, useState } from 'react'
import { summaryTagsApi, type TagMatch, type UserTag } from '@/lib/summaryTags'
import { TagPicker } from './TagPicker'
import { TagWordbook } from './TagWordbook'
import './tags.css'

export function LibraryTagFilter({ ids, match, onChange, readOnly = false }: { ids: string[]; match: TagMatch; onChange: (ids: string[], match: TagMatch) => void; readOnly?: boolean }) {
  const [open, setOpen] = useState(false)
  const [wordbook, setWordbook] = useState(false)
  const [names, setNames] = useState<Record<string, string>>({})
  const [common, setCommon] = useState<UserTag[]>([])
  useEffect(() => {
    const controller = new AbortController()
    void summaryTagsApi.list(1, '', controller.signal, 'usage').then(result => { if (!controller.signal.aborted) { const rows = result.list || []; setCommon([...rows].sort((a,b) => b.video_count - a.video_count).slice(0,6)); setNames(old => ({...old, ...Object.fromEntries(rows.map(tag => [tag.id,tag.display_name]))})) } }).catch(() => {})
    return () => controller.abort()
  }, [wordbook])
  const select = (tag: UserTag) => { setNames(old => ({ ...old, [tag.id]: tag.display_name })); onChange([...ids, tag.id], match) }
  return <div className="library-tag-filter"><div className="tag-chips"><button className="btn btn-sm" aria-expanded={open} onClick={() => setOpen(!open)}>标签筛选{ids.length ? ` · ${ids.length}` : ''}</button><button className="btn btn-sm btn-ghost" onClick={() => setWordbook(true)}>管理标签词表</button>
    {!!ids.length && <><select aria-label="标签匹配方式" value={match} onChange={event => onChange(ids, event.target.value as TagMatch)}><option value="all">同时包含全部标签</option><option value="any">包含任一标签</option></select>{ids.map((id, i) => <button className="btn btn-sm" key={id} aria-label={`移除筛选标签 ${names[id] || i + 1}`} onClick={() => onChange(ids.filter(value => value !== id), match)}>{names[id] || `已选标签 ${i + 1}`} ×</button>)}<button className="btn btn-sm btn-ghost" onClick={() => onChange([], 'all')}>清空标签筛选</button></>}
  </div>{!!common.length && <div className="tag-chips" style={{marginTop:8}} aria-label="常用标签">{common.map(tag => <button className="btn btn-sm" key={tag.id} aria-pressed={ids.includes(tag.id)} disabled={!ids.includes(tag.id) && ids.length >= 50} onClick={() => ids.includes(tag.id) ? onChange(ids.filter(id => id !== tag.id),match) : select(tag)}>{tag.display_name}</button>)}</div>}{open && <TagPicker onSelect={select} selected={ids} disabled={ids.length >= 50} />}{wordbook && <TagWordbook onClose={() => setWordbook(false)} readOnly={readOnly} />}</div>
}
