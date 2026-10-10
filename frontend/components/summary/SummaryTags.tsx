import { useEffect, useRef, useState } from 'react'
import { summaryTagsApi, tagError, type TaskTagState, type UserTag } from '@/lib/summaryTags'
import { TagPicker } from '@/components/tags/TagPicker'
import { TagWordbook } from '@/components/tags/TagWordbook'
import '@/components/tags/tags.css'
import { Modal } from '@/components/ui/Modal'

export function SummaryTags({ taskId, readOnly = false, generationVersion, onChanged, compact = false }: { taskId: number; readOnly?: boolean; generationVersion?: number; compact?: boolean; onChanged?: () => void }) {
  const [state, setState] = useState<TaskTagState | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [adding, setAdding] = useState(false)
  const [wordbook, setWordbook] = useState(false)
  const [managing, setManaging] = useState(false)
  useEffect(() => { setManaging(false) }, [taskId, generationVersion])
  const [name, setName] = useState('')
  const [reload, setReload] = useState(0)
  const scopeKey = `${taskId}:${generationVersion ?? ''}:${reload}`
  const scope = useRef(scopeKey)
  const active = useRef(true)
  useEffect(() => { active.current = true; return () => { active.current = false } }, [])
  scope.current = scopeKey
  useEffect(() => {
    const controller = new AbortController(); setState(null); setError(''); setBusy(false); setAdding(false); setName('')
    void summaryTagsApi.task(taskId, controller.signal).then(result => { if (!controller.signal.aborted) setState(result) }).catch(error => { if (!controller.signal.aborted) setError(tagError(error)) })
    return () => controller.abort()
  }, [taskId, generationVersion, reload])
  useEffect(() => {
    if (state?.classification?.status !== 'pending') return
    const controller = new AbortController(); let timer: number | undefined
    const poll = async () => {
      try {
        const result = await summaryTagsApi.task(taskId,controller.signal)
        if (!controller.signal.aborted) { setState(previous => previous && previous.version > result.version ? previous : result); setError('') }
      } catch (error) { if (!controller.signal.aborted) { setError(tagError(error)); timer = window.setTimeout(() => void poll(),4000) } }
    }
    timer = window.setTimeout(() => void poll(),1500)
    return () => { controller.abort(); window.clearTimeout(timer) }
  }, [taskId, scopeKey, state])
  async function mutate(work: () => Promise<TaskTagState>) {
    if (busy || readOnly) return
    const target = scopeKey; setBusy(true); setError('')
    try { const result = await work(); if (active.current && scope.current === target) { setState(result); onChanged?.() } }
    catch (error) { if (active.current && scope.current === target) setError(tagError(error)) }
    finally { if (active.current && scope.current === target) setBusy(false) }
  }
  const add = (tag: UserTag) => { if (state) void mutate(() => summaryTagsApi.patch(taskId, { expected_version: state.version, add_ids: [tag.id] })) }
  const management = <section className="summary-tags" aria-label="视频标签"><div className="summary-tags-heading"><strong>标签</strong>{!readOnly && <button className="btn btn-sm btn-ghost" onClick={() => setAdding(!adding)}>添加标签</button>}<button className="btn btn-sm btn-ghost" onClick={() => { setManaging(false); setWordbook(true) }}>词表</button></div>
    {error && <div role="alert">{error}<button className="btn btn-sm" onClick={() => setReload(old => old + 1)}>刷新标签</button></div>}
    {!state && !error && <small role="status">正在读取标签…</small>}
    {state?.classification?.enabled && state.classification.status === 'pending' && <small role="status">正文已就绪，正在处理标签…</small>}
    {state?.classification?.enabled && state.classification.status === 'failed' && <small role="status">标签处理未完成，摘要正文仍可阅读。{state.classification.error_code === 'version_conflict' ? '你已调整过标签，请以当前选择为准。' : '可手动添加标签。'}</small>}
    {state && <><div className="tag-chips">{state.assignments.map(assignment => <span key={assignment.tag_id} className="tag-chips"><span className="chip chip-mute">{assignment.tag.display_name}{assignment.origin === 'auto' ? ' · 自动' : ''}</span>{!readOnly && <>{assignment.origin === 'auto' && <button className="btn btn-sm btn-ghost" disabled={busy} onClick={() => void mutate(() => summaryTagsApi.patch(taskId, { expected_version: state.version, keep_auto_ids: [assignment.tag_id] }))}>保留</button>}<button className="btn btn-sm btn-ghost" disabled={busy} aria-label={`移除标签 ${assignment.tag.display_name}`} onClick={() => void mutate(() => summaryTagsApi.patch(taskId, { expected_version: state.version, remove_ids: [assignment.tag_id] }))}>×</button></>}</span>)}{!state.assignments.length && <span className="tag-feedback">尚未添加标签</span>}</div>
      {state.suggestions.map(suggestion => ({...suggestion, status: suggestion.effective_status ?? suggestion.status})).filter(suggestion => suggestion.status === 'pending' || suggestion.status === 'rejected').map(suggestion => <div className="tag-suggestion" key={suggestion.id}><div><strong>{suggestion.display_name}</strong><small> · {suggestion.status === 'pending' ? '待确认' : '已拒绝'}</small><p>{suggestion.reason}</p></div>{!readOnly && (suggestion.status === 'pending' ? <><button className="btn btn-sm" disabled={busy} onClick={() => void mutate(() => summaryTagsApi.decide(taskId, suggestion.id, 'accept', state.version))}>接受</button><button className="btn btn-sm btn-ghost" disabled={busy} onClick={() => void mutate(() => summaryTagsApi.decide(taskId, suggestion.id, 'reject', state.version))}>拒绝</button></> : <button className="btn btn-sm" disabled={busy} onClick={() => void mutate(() => summaryTagsApi.decide(taskId, suggestion.id, 'restore', state.version))}>恢复建议</button>)}</div>)}
      {adding && !readOnly && <div><TagPicker onSelect={add} selected={state.assignments.map(row => row.tag_id)} disabled={busy} /><form className="tag-chips" onSubmit={event => { event.preventDefault(); if (!name.trim()) return; void mutate(async () => { const tag = await summaryTagsApi.create(name); return summaryTagsApi.patch(taskId, { expected_version: state.version, add_ids: [tag.id] }) }) }}><input className="input" aria-label="创建并添加标签" placeholder="新标签名称" value={name} maxLength={80} onChange={event => setName(event.target.value)} /><button className="btn btn-sm" disabled={busy || !name.trim()}>创建并添加</button></form></div>}
    </>}
  </section>
  const pending = state?.suggestions.filter(row => (row.effective_status ?? row.status) === 'pending').length ?? 0
  return <>{compact ? <section className="summary-tags-inline" aria-label="视频分类"><div className="summary-tag-labels">{state?.assignments.slice(0, 3).map(row => <span className="chip chip-mute" key={row.tag_id}>{row.tag.display_name}</span>)}{state && state.assignments.length > 3 && <button className="summary-tags-count" onClick={() => setManaging(true)} aria-label="查看全部分类">+{state.assignments.length - 3}</button>}{!state && <small className="muted">{error ? '分类暂不可用' : '正在读取分类…'}</small>}{state && !state.assignments.length && <small className="muted">{state.classification?.status === 'pending' ? '正在整理分类…' : '尚无分类'}</small>}</div><button className="btn btn-sm btn-ghost" onClick={() => setManaging(true)}>管理分类{pending ? ` · ${pending} 个建议` : ''}</button>{managing && <Modal title="管理视频分类" onClose={() => setManaging(false)}>{management}</Modal>}</section> : management}{wordbook && <TagWordbook readOnly={readOnly} onClose={() => { setWordbook(false); setReload(old => old + 1) }} />}</>

}
