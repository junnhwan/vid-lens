'use client'

import { useEffect, useRef, useState, type ReactNode } from 'react'
import dynamic from 'next/dynamic'
import { Icon } from '@/components/ui/Icon'
import { Modal } from '@/components/ui/Modal'
import { useMediaQuery } from '@/components/ui/useMediaQuery'
import { bodySchema, type ArtifactDetail, type StudyBody, type StudyBlock } from '@/lib/artifacts/schema'
import { ApiError } from '@/lib/api'
import { artifactError } from '@/lib/artifacts/api'

const StudyMap = dynamic(() => import('./StudyMap').then(module => module.StudyMap), { ssr: false, loading: () => <div className="empty" role="status">正在加载导图…</div> })

export function ArtifactWorkspace({ artifact, readOnly = false, historical = false, preview = false, evidencePanel, selectedEvidence, onEvidence, onSave, onReload, onVersions, onDirtyChange }: {
  artifact: ArtifactDetail; readOnly?: boolean; historical?: boolean; preview?: boolean
  evidencePanel: ReactNode; selectedEvidence?: string
  onEvidence: (id: string) => void
  onSave: (base: number, body: StudyBody) => Promise<ArtifactDetail>
  onReload: () => Promise<ArtifactDetail>
  onVersions?: () => void
  onDirtyChange?: (dirty: boolean) => void
}) {
  const [baseline, setBaseline] = useState(artifact)
  const [draft, setDraft] = useState<StudyBody | null>(artifact.version?.body ?? null)
  const [view, setView] = useState<'notes' | 'map'>('notes')
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)
  const [conflict, setConflict] = useState<ArtifactDetail | null>(null)
  const [showConflict, setShowConflict] = useState(false)
  const [showEvidence, setShowEvidence] = useState(false)
  const [desktopEvidence, setDesktopEvidence] = useState(true)
  const mobile = useMediaQuery('(max-width: 1100px)')
  const [selectedBlock, setSelectedBlock] = useState<string | null>(null)
  const dirty = JSON.stringify(draft) !== JSON.stringify(baseline.version?.body ?? null)
  const dirtyRef = useRef(dirty); dirtyRef.current = dirty
  useEffect(() => { onDirtyChange?.(dirty); return () => onDirtyChange?.(false) }, [dirty, onDirtyChange])
  useEffect(() => {
    const unload = (event: BeforeUnloadEvent) => { if (dirtyRef.current) { event.preventDefault(); event.returnValue = '' } }
    window.addEventListener('beforeunload', unload)
    return () => window.removeEventListener('beforeunload', unload)
  }, [])
  useEffect(() => {
    if (!dirtyRef.current && !editing) { setBaseline(artifact); setDraft(artifact.version?.body ?? null) }
  }, [artifact, editing])
  if (!draft || !baseline.version) return <div className="page"><div className="empty card"><Icon name="clock" size="lg" /><b>学习笔记还在准备中</b><p>生成完成后会出现在这里，可以在任务中心查看进度。</p><a className="btn" href="/tasks">查看任务</a></div></div>
  const body = draft
  function update(blockId: string, patch: Partial<StudyBlock>) {
    setSaved(false)
    setDraft(current => current ? { ...current, blocks: current.blocks.map(block => block.block_id === blockId ? { ...block, ...patch, claim_origin: 'user' } : block) } : current)
  }
  function chooseBlock(id: string) {
    setSelectedBlock(id)
    const evidence = body.blocks.find(block => block.block_id === id)?.evidence_refs[0]
    if (evidence) { onEvidence(evidence.evidence_id); setShowEvidence(true); setDesktopEvidence(true) }
  }
  async function save() {
    if (readOnly) return
    const parsed = bodySchema.safeParse(body)
    if (!parsed.success) { setSaveError(parsed.error.issues[0]?.message || '请检查正文'); return }
    setSaving(true); setSaveError(''); setSaved(false)
    try {
      const next = await onSave(baseline.head_version, parsed.data)
      setBaseline(next); setDraft(next.version?.body ?? null); setEditing(false); setConflict(null); setSaved(true)
    } catch (error) {
      setSaveError(artifactError(error))
      if (error instanceof ApiError && error.status === 409) {
        try { setConflict(await onReload()); setShowConflict(true) } catch { /* Keep the local draft and original error. */ }
      }
    } finally { setSaving(false) }
  }
  function downloadDraft() {
    const blob = new Blob([JSON.stringify(body, null, 2)], { type: 'application/json;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = 'vidlens-unsaved-draft.json'; link.click(); URL.revokeObjectURL(url)
  }
  const selected = body.blocks.find(block => block.block_id === selectedBlock)
  return <div className="artifact-workspace">
    <header className="artifact-titlebar"><span className="artifact-kind-icon"><Icon name="file" /></span><div><h1>{body.title}</h1><p>学习笔记 · {historical ? '历史版本，只读' : '笔记与导图共用同一份内容'}{preview ? ' · 开发样例' : ''}</p></div><div className="product-actions">{onVersions && <button className="btn btn-sm btn-ghost" onClick={onVersions}><Icon name="clock" size="sm" />版本</button>}<button className="btn btn-sm" onClick={() => mobile ? setShowEvidence(!showEvidence) : setDesktopEvidence(!desktopEvidence)}><Icon name="link" size="sm" />证据</button></div></header>
    <div className="artifact-toolbar"><div className="seg" aria-label="成果视图"><button className={view === 'notes' ? 'on' : ''} aria-pressed={view === 'notes'} onClick={() => setView('notes')}>学习笔记</button><button className={view === 'map' ? 'on' : ''} aria-pressed={view === 'map'} onClick={() => setView('map')}>思维导图</button></div><span className="artifact-save-state" role="status">{saving ? '正在保存…' : dirty ? '有未保存的修改' : saved ? preview ? '样例已保存（仅当前预览）' : '已保存' : `v${baseline.version.version} · 待核对`}</span><div className="product-actions">{editing && !readOnly ? <><button className="btn btn-sm btn-ghost" disabled={saving} onClick={() => { if (dirty && !window.confirm('放弃未保存的修改？')) return; setDraft(baseline.version?.body ?? null); setEditing(false); setSaveError('') }}>取消编辑</button><button className="btn btn-sm btn-primary" disabled={!dirty || saving} onClick={() => void save()}>保存修改</button></> : !readOnly && <button className="btn btn-sm" onClick={() => { setEditing(true); setSaved(false) }}><Icon name="file" size="sm" />编辑笔记</button>}</div></div>
    {saveError && <div className="artifact-notice danger" role="alert">{saveError}<div className="product-actions">{conflict && <button className="btn btn-sm" onClick={() => setShowConflict(true)}>比较版本</button>}<button className="btn btn-sm" onClick={downloadDraft}>下载本地草稿</button></div></div>}
    {artifact.head_version !== baseline.head_version && <div className="artifact-notice">服务器有更新，当前编辑仍基于 v{baseline.head_version}。保存时会检查版本。</div>}
    <div className={`artifact-columns${!desktopEvidence && !mobile ? " without-evidence" : ""}`}>
      <div className="artifact-reading">
        {view === 'map' ? <><StudyMap body={body} onSelect={chooseBlock} />{selected && <div className="selected-concept"><p className="product-eyebrow">SELECTED CONCEPT</p><h3>{selected.title}</h3><p>{selected.content}</p>{!selected.evidence_refs.length && <p className="muted">这个节点没有来源引用。</p>}{!readOnly && <button className="btn btn-sm" onClick={() => { setView('notes'); setEditing(true) }}>在笔记中编辑</button>}</div>}</> : <article className="study-paper">
          <div className="paper-meta"><span>LEARNING NOTES / {String(baseline.version.version).padStart(3, '0')}</span><span>理解，然后应用</span></div>
          {editing && !readOnly ? <label className="artifact-field">笔记标题<input maxLength={200} value={body.title} onChange={e => { setSaved(false); setDraft({ ...body, title: e.target.value }) }} /></label> : <h2>{body.title}</h2>}
          <p className="paper-intro">沿着视频整理概念，保留每一次回到来源的入口。</p>
          <div className="study-concept-index">{body.blocks.filter(block => block.parent_id === null).map((block, i) => <a key={block.block_id} href={`#block-${block.block_id}`}><span className="mono">{String(i + 1).padStart(2, '0')}</span><b>{block.title}</b><Icon name="chev-r" size="sm" /></a>)}</div>
          {body.warnings.length > 0 && <div className="paper-warning"><Icon name="alert" size="sm" /><span>{body.warnings.map(warning => warning === 'human_edited_unverified' ? '人工修改后的引用关系尚待核对。' : warning).join(' · ')}</span></div>}
          {body.blocks.map((block, i) => <section className={`study-block${block.parent_id ? ' is-child' : ''}${selectedBlock === block.block_id ? ' selected' : ''}`} key={block.block_id} id={`block-${block.block_id}`}>
            <div className="study-block-meta"><span>{block.type === 'section' ? '章节' : block.type === 'concept' ? '概念' : block.type === 'example' ? '示例' : '笔记'} {String(i + 1).padStart(2, '0')}</span><span>{block.claim_origin === 'user' ? '人工编辑 · 引用待核对' : block.claim_origin === 'synthesis' ? '综合理解' : '来源整理'}</span></div>
            {editing && !readOnly ? <><label className="artifact-field">{`第 ${i + 1} 块标题`}<input value={block.title} maxLength={200} onChange={e => update(block.block_id, { title: e.target.value })} /></label><label className="artifact-field">{`第 ${i + 1} 块正文`}<textarea value={block.content} maxLength={8000} rows={Math.max(3, Math.min(10, block.content.split('\n').length + 2))} onChange={e => update(block.block_id, { content: e.target.value })} /></label></> : <><h3>{block.title}</h3><p className="study-block-content">{block.content}</p></>}
            <div className="study-citations">{block.evidence_refs.map((ref, n) => <button className={selectedEvidence === ref.evidence_id ? 'selected' : ''} key={`${ref.evidence_id}-${n}`} onClick={() => { setSelectedBlock(block.block_id); onEvidence(ref.evidence_id); setShowEvidence(true); setDesktopEvidence(true) }}><Icon name="play" size="sm" />{ref.relation === 'contradicts' ? '相反证据' : ref.relation === 'context' ? '背景证据' : '查看依据'} {n + 1}</button>)}{block.evidence_refs.length === 0 && <span>无来源引用 · 请自行核对</span>}</div>
          </section>)}
          <footer className="paper-footer"><Icon name="shield-check" size="sm" />笔记帮助组织理解；引用关联不代表结论已经核实。</footer>
        </article>}
      </div>
      {!mobile && desktopEvidence && <aside className="artifact-evidence-desktop" aria-label="视频证据">{evidencePanel}</aside>}
    </div>
    {mobile && showEvidence && <div className="artifact-evidence-mobile"><Modal title="回到原视频" onClose={() => setShowEvidence(false)} width={440}>{evidencePanel}</Modal></div>}
    {showConflict && conflict && <Modal title="保留你的修改，核对新版本" onClose={() => setShowConflict(false)} width={780} footer={<><button className="btn" onClick={downloadDraft}>下载我的草稿</button><button className="btn" onClick={() => setShowConflict(false)}>继续编辑</button><button className="btn btn-primary" onClick={() => { if (!window.confirm('加载服务器版本会放弃当前修改。请先下载需要保留的草稿。')) return; setBaseline(conflict); setDraft(conflict.version?.body ?? null); setEditing(false); setShowConflict(false); setConflict(null); setSaveError('') }}>加载服务器版本</button></>}><p className="product-description">当前草稿基于 v{baseline.head_version}，服务器已有 v{conflict.head_version}。不会自动覆盖任一版本。</p><div className="version-compare"><div><h4>我的草稿</h4><h3>{body.title}</h3>{body.blocks.map(block => <p key={block.block_id}><b>{block.title}</b><br />{block.content}</p>)}</div><div><h4>服务器版本</h4><h3>{conflict.version?.body.title}</h3>{conflict.version?.body.blocks.map(block => <p key={block.block_id}><b>{block.title}</b><br />{block.content}</p>)}</div></div></Modal>}
  </div>
}
