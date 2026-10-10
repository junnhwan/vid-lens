import { useEffect, useRef, useState } from 'react'
import { Modal } from '@/components/ui/Modal'
import { summaryTagsApi, tagError, type UserTag } from '@/lib/summaryTags'
import { TagPicker } from './TagPicker'
import './tags.css'

export function TagWordbook({ onClose, readOnly = false }: { onClose: () => void; readOnly?: boolean }) {
  const [selected, setSelected] = useState<UserTag | null>(null)
  const [target, setTarget] = useState<UserTag | null>(null)
  const [name, setName] = useState('')
  const [aliases, setAliases] = useState('')
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [revision, setRevision] = useState(0)
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  const mergeKey = useRef(crypto.randomUUID())
  function choose(tag: UserTag) { setSelected(tag); setName(tag.display_name); setAliases((tag.aliases || []).join('\n')); setTarget(null); setError(''); mergeKey.current = crypto.randomUUID() }
  async function mutate(work: () => Promise<UserTag>, merged = false) {
    setBusy(true); setError('')
    try { const result = await work(); if (!active.current) return false; setRevision(old => old + 1); if (merged) { setSelected(null); setTarget(null) } else choose(result); return true }
    catch (error) { if (active.current) setError(tagError(error)); return false }
    finally { if (active.current) setBusy(false) }
  }
  return <Modal title="我的标签词表" onClose={onClose} width={640}><div className="tag-wordbook">
    <p className="tag-feedback">名称和已确认的别名共同匹配同一标签。合并会同步关联视频，需由你明确选择目标标签。</p>
    {!readOnly && <form className="tag-chips" onSubmit={event => { event.preventDefault(); if (newName.trim()) void mutate(() => summaryTagsApi.create(newName)).then(saved => { if (saved && active.current) setNewName('') }) }}><input className="input" aria-label="新标签名称" maxLength={80} value={newName} onChange={event => setNewName(event.target.value)} /><button className="btn btn-sm" disabled={busy || !newName.trim()}>创建标签</button></form>}
    <TagPicker key={revision} onSelect={choose} disabled={busy} />
    {error && <p role="alert">{error}</p>}
    {selected && <section className="tag-editor"><strong>{selected.display_name} · {selected.video_count} 个视频</strong><label>规范名称<input className="input" aria-label="标签规范名称" value={name} maxLength={80} disabled={readOnly || busy} onChange={event => setName(event.target.value)} /></label><button className="btn btn-sm" disabled={readOnly || busy || !name.trim() || name === selected.display_name} onClick={() => void mutate(() => summaryTagsApi.rename(selected, name))}>保存名称</button>
      <label>已确认的别名（每行一个，最多 20 个）<textarea className="input" aria-label="标签别名" rows={3} value={aliases} disabled={readOnly || busy} onChange={event => setAliases(event.target.value)} /></label><button className="btn btn-sm" disabled={readOnly || busy} onClick={() => void mutate(() => summaryTagsApi.aliases(selected, aliases.split('\n').map(value => value.trim()).filter(Boolean)))}>保存别名</button>
      {!readOnly && <details><summary>合并到另一个标签</summary><TagPicker onSelect={tag => { setTarget(tag); mergeKey.current = crypto.randomUUID() }} selected={[selected.id]} disabled={busy} />{target && <div className="tag-chips"><span>将「{selected.display_name}」合并到「{target.display_name}」，同步迁移所有视频关联与别名。</span><button className="btn btn-sm" disabled={busy} onClick={() => void mutate(() => summaryTagsApi.merge(selected, target, mergeKey.current), true)}>确认合并这两个标签</button></div>}</details>}
    </section>}
  </div></Modal>
}
