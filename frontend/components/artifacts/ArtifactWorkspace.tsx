import { lazy, Suspense, useCallback, useEffect, useRef, useState, type ReactNode, type SetStateAction } from 'react'
import { Icon } from '@/components/ui/Icon'
import { Modal } from '@/components/ui/Modal'
import { useMediaQuery } from '@/components/ui/useMediaQuery'
import { bodySchema, type ArtifactDetail, type StudyBody, type StudyBlock } from '@/lib/artifacts/schema'
import { ApiError } from '@/lib/api'
import { artifactError } from '@/lib/artifacts/api'
import { warningMessage } from '@/lib/artifacts/view'
import { addBlock, deleteBlock, descendantCount, mergeWithNext, moveBlock } from '@/lib/artifacts/edit'
import { blockTree } from '@/lib/artifacts/view'

const StudyMap = lazy(() => import('./StudyMap').then(module => ({ default: module.StudyMap })))
const KnowledgeCanvas = lazy(() => import('./KnowledgeCanvas').then(module => ({ default: module.KnowledgeCanvas })))

export function ArtifactWorkspace({ artifact, readOnly = false, historical = false, preview = false, evidencePanel, selectedEvidence, evidenceOpenRequest, onEvidence, onSave, onReload, onVersions, onExport, onDirtyChange, onAskBlock, onAgentEdit, onStudyBlock, initialBlock }: {
  artifact: ArtifactDetail; readOnly?: boolean; historical?: boolean; preview?: boolean
  evidencePanel: (refs: StudyBlock['evidence_refs'], selectedId: string | undefined, onSelect: (id: string) => void) => ReactNode; selectedEvidence?: string
  evidenceOpenRequest?: { id: string; nonce: number } | null
  onEvidence: (id: string) => void
  onSave: (base: number, body: StudyBody) => Promise<ArtifactDetail>
  onReload: () => Promise<ArtifactDetail>
  onVersions?: () => void
  onExport?: (savedVersionId: string) => Promise<{ markdown: string; filename: string }>
  onDirtyChange?: (dirty: boolean) => void
  onAskBlock?: (blockId: string, versionId: string) => void
  onAgentEdit?: (blockId: string | null, base: ArtifactDetail) => void
  onStudyBlock?: (blockId: string, versionId: string) => void
  initialBlock?: string
}) {
  const [baseline, setBaseline] = useState(artifact)
  const [draft, setDraft] = useState<StudyBody | null>(artifact.version?.body ?? null)
  const draftRevision = useRef(0)
  const editDraft = (next: SetStateAction<StudyBody | null>) => { ++draftRevision.current; setDraft(next) }
  const [view, setView] = useState<'notes' | 'map' | 'canvas'>('notes')
  const [editing, setEditing] = useState(false)
  const [activeEditor, setActiveEditor] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [saved, setSaved] = useState(false)
  const [conflict, setConflict] = useState<ArtifactDetail | null>(null)
  const [showConflict, setShowConflict] = useState(false)
  const [showEvidence, setShowEvidence] = useState(false)
  const [desktopEvidence, setDesktopEvidence] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [deleteUndo, setDeleteUndo] = useState<StudyBody | null>(null)
  const [exportPrompt, setExportPrompt] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [exportError, setExportError] = useState('')
  const [exportReady, setExportReady] = useState<{ url: string; filename: string } | null>(null)
  const [askPrompt, setAskPrompt] = useState<string | null>(null)
  const [agentPrompt, setAgentPrompt] = useState<false | string | null>(false)
  const mobile = useMediaQuery('(max-width: 1100px)')
  const [selectedBlock, setSelectedBlock] = useState<string | null>(initialBlock || artifact.version?.body.blocks[0]?.block_id || null)
  const restoredBlock = useRef<string | undefined>(undefined)
  const reading = useRef<HTMLDivElement>(null)
  const readingPosition = useRef<{ page: number; pane: number; shell: number } | null>(null)
  const handledEvidenceRequest = useRef<number>()
  const dirty = JSON.stringify(draft) !== JSON.stringify(baseline.version?.body ?? null)
  const dirtyRef = useRef(dirty); dirtyRef.current = dirty
  const openEvidence = useCallback(() => {
    if (mobile && !showEvidence) readingPosition.current = { page: window.scrollY, pane: reading.current?.scrollTop ?? 0, shell: reading.current?.closest('.content')?.scrollTop ?? 0 }
    setShowEvidence(true)
    setDesktopEvidence(true)
  }, [mobile, showEvidence])
  useEffect(() => { onDirtyChange?.(dirty); return () => onDirtyChange?.(false) }, [dirty, onDirtyChange])
  useEffect(() => {
    const unload = (event: BeforeUnloadEvent) => { if (dirtyRef.current) { event.preventDefault(); event.returnValue = '' } }
    window.addEventListener('beforeunload', unload)
    return () => window.removeEventListener('beforeunload', unload)
  }, [])
  useEffect(() => () => { if (exportReady) URL.revokeObjectURL(exportReady.url) }, [exportReady])
  useEffect(() => {
    if (!dirtyRef.current && !editing && artifact.head_version >= baseline.head_version) { setBaseline(artifact); setDraft(artifact.version?.body ?? null); setDeleteUndo(null); setExportReady(null) }
  }, [artifact, editing, baseline.head_version])
  useEffect(() => {
    if (restoredBlock.current === initialBlock) return
    if (!initialBlock) { restoredBlock.current = undefined; setSelectedBlock(null); return }
    const block = draft?.blocks.find(item => item.block_id === initialBlock)
    if (!block) return
    restoredBlock.current = initialBlock
    setSelectedBlock(block.block_id)
    if (block.evidence_refs[0]) onEvidence(block.evidence_refs[0].evidence_id)
  }, [initialBlock, draft, onEvidence])
  useEffect(() => {
    if (!initialBlock) return
    const frame = requestAnimationFrame(() => document.getElementById(`block-${initialBlock}`)?.scrollIntoView({ block: 'center' }))
    return () => cancelAnimationFrame(frame)
  }, [initialBlock])
  useEffect(() => {
    if (!evidenceOpenRequest || handledEvidenceRequest.current === evidenceOpenRequest.nonce) return
    handledEvidenceRequest.current = evidenceOpenRequest.nonce
    const block = draft?.blocks.find(item => item.evidence_refs.some(ref => ref.evidence_id === evidenceOpenRequest.id))
    setSelectedBlock(block?.block_id ?? null)
    onEvidence(evidenceOpenRequest.id)
    openEvidence()
  }, [draft, evidenceOpenRequest, onEvidence, openEvidence])
  if (!draft || !baseline.version) return <div className="page"><div className="empty card"><Icon name="clock" size="lg" /><b>当前没有可阅读的笔记版本</b><p>可以在任务中心查看生成结果、失败原因或取消状态，并从视频重新发起生成。</p><a className="btn" href="/tasks">查看任务</a></div></div>
  const body = draft
  function update(blockId: string, patch: Partial<StudyBlock>) {
    setSaved(false)
    setDeleteUndo(null)
    editDraft(current => current ? { ...current, blocks: current.blocks.map(block => block.block_id === blockId ? { ...block, ...patch, claim_origin: 'user' } : block) } : current)
  }
  function chooseBlock(id: string) {
    setSelectedBlock(id)
    if (!dirty && baseline.version && !historical && baseline.version.body.blocks.some(b => b.block_id === id)) onStudyBlock?.(id, baseline.version.id)
    const evidence = body.blocks.find(block => block.block_id === id)?.evidence_refs[0]
    if (evidence) { onEvidence(evidence.evidence_id); openEvidence() }
  }
  function chooseCanvasBlock(id: string) {
    setSelectedBlock(id)
    if (!dirty && baseline.version && !historical && baseline.version.body.blocks.some(b => b.block_id === id)) onStudyBlock?.(id, baseline.version.id)
  }
  function closeEvidence() {
    setShowEvidence(false)
    const position = readingPosition.current
    if (position) requestAnimationFrame(() => { window.scrollTo(0, position.page); if (reading.current) { reading.current.scrollTop = position.pane; const shell = reading.current.closest('.content'); if (shell) shell.scrollTop = position.shell } })
  }
  function structure(action: (current: StudyBody) => StudyBody) {
    try { editDraft(action(body)); setSaveError(''); setSaved(false); setDeleteUndo(null) }
    catch (error) { setSaveError(error instanceof Error ? error.message : '结构调整失败') }
  }
  async function save(): Promise<ArtifactDetail | null> {
    if (readOnly) return null
    const parsed = bodySchema.safeParse(body)
    if (!parsed.success) { setSaveError(parsed.error.issues[0]?.message || '请检查正文'); return null }
    const submittedRevision = draftRevision.current
    setSaving(true); setSaveError(''); setSaved(false)
    try {
      const next = await onSave(baseline.head_version, parsed.data)
      const editedSinceSubmit = draftRevision.current !== submittedRevision
      setBaseline(next)
      if (!editedSinceSubmit) { setDraft(next.version?.body ?? null); setEditing(false); setActiveEditor(null); setSaved(true); setDeleteUndo(null) }
      else setSaved(false)
      setConflict(null); setExportReady(null)
      return editedSinceSubmit ? null : next
    } catch (error) {
      setSaveError(artifactError(error))
      if (error instanceof ApiError && error.status === 409) {
        try { setConflict(await onReload()); setShowConflict(true) } catch { /* Keep the local draft and original error. */ }
      }
    } finally { setSaving(false) }
    return null
  }
  async function exportSaved(savedVersionId = baseline.version?.id) {
    if (!onExport || exporting) return
    setExporting(true); setExportError(''); setExportReady(null)
    try {
      if (!savedVersionId) throw new Error('当前没有可导出的已保存版本')
      const file = await onExport(savedVersionId)
      setExportReady({ url: URL.createObjectURL(new Blob([file.markdown], { type: 'text/markdown;charset=utf-8' })), filename: file.filename })
    } catch (error) { setExportError(error instanceof ApiError ? artifactError(error) : error instanceof Error ? error.message : '导出失败，请稍后重试') }
    finally { setExporting(false) }
  }
  function requestAgentEdit(blockId: string | null) {
    if (!onAgentEdit || readOnly) return
    if (dirty) { setSaveError(''); setAgentPrompt(blockId) }
    else onAgentEdit(blockId, baseline)
  }
  function downloadDraft() {
    const blob = new Blob([JSON.stringify(body, null, 2)], { type: 'application/json;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a'); link.href = url; link.download = 'vidlens-unsaved-draft.json'; link.click(); URL.revokeObjectURL(url)
  }
  const selected = body.blocks.find(block => block.block_id === selectedBlock)
  const evidenceBlock = selected ?? body.blocks.find(block => block.evidence_refs.some(ref => ref.evidence_id === selectedEvidence)) ?? body.blocks.find(block => block.evidence_refs.length)
  const activeRefs = evidenceBlock?.evidence_refs ?? []
  const explicitlyRequestedEvidence = selectedEvidence && evidenceOpenRequest?.id === selectedEvidence
  const panel = evidencePanel(activeRefs, selectedEvidence && (explicitlyRequestedEvidence || activeRefs.some(ref => ref.evidence_id === selectedEvidence)) ? selectedEvidence : activeRefs[0]?.evidence_id, onEvidence)
  const roots = blockTree(body)
  return <div className="artifact-workspace">
    <header className="artifact-titlebar"><span className="artifact-kind-icon"><Icon name="file" /></span><div><h1>{body.title}</h1><p>学习笔记 · {historical ? '历史版本，只读' : '笔记与导图共用同一份内容'}{preview ? ' · 开发样例' : ''}</p></div><div className="product-actions">{onAgentEdit && !readOnly && <button className="btn btn-sm btn-ghost" disabled={saving} onClick={() => requestAgentEdit(null)}><Icon name="wand" size="sm" />让 Agent 修改全文</button>}{onVersions && <button className="btn btn-sm btn-ghost" onClick={onVersions}><Icon name="clock" size="sm" />版本</button>}{onExport && <button className="btn btn-sm" disabled={exporting || saving} onClick={() => dirty ? setExportPrompt(true) : void exportSaved()}><Icon name="download" size="sm" />{exporting ? '导出中…' : '导出 Markdown'}</button>}<button className="btn btn-sm" onClick={() => mobile ? showEvidence ? closeEvidence() : openEvidence() : setDesktopEvidence(!desktopEvidence)}><Icon name="link" size="sm" />证据</button></div></header>
    <div className="artifact-toolbar"><div className="seg" aria-label="成果视图"><button className={view === 'notes' ? 'on' : ''} aria-pressed={view === 'notes'} onClick={() => setView('notes')}>学习笔记</button><button className={view === 'map' ? 'on' : ''} aria-pressed={view === 'map'} onClick={() => setView('map')} title="浏览概念层级与关系">思维导图</button><button className={view === 'canvas' ? 'on' : ''} aria-pressed={view === 'canvas'} onClick={() => setView('canvas')} title="手动摆放概念与连线">知识画布</button></div><span className="artifact-save-state" role="status">{saving ? '正在保存…' : dirty ? '有未保存的修改' : saved ? preview ? '样例已保存（仅当前预览）' : '已保存' : `v${baseline.version.version} · 待核对`}</span><div className="product-actions">{editing && !readOnly ? <><button className="btn btn-sm btn-ghost" disabled={saving} onClick={() => { if (dirty && !window.confirm('放弃未保存的修改？')) return; setDraft(baseline.version?.body ?? null); setEditing(false); setActiveEditor(null); setDeleteUndo(null); setSaveError('') }}>取消编辑</button><button className="btn btn-sm btn-primary" disabled={!dirty || saving} onClick={() => void save()}>保存修改</button></> : !readOnly && <button className="btn btn-sm" onClick={() => { setEditing(true); setActiveEditor(selectedBlock || body.blocks[0]?.block_id || null); setSaved(false) }}><Icon name="file" size="sm" />编辑笔记</button>}</div></div>
    {saveError && <div className="artifact-notice danger" role="alert">{saveError}<div className="product-actions">{conflict && <button className="btn btn-sm" onClick={() => setShowConflict(true)}>比较版本</button>}<button className="btn btn-sm" onClick={downloadDraft}>下载本地草稿</button></div></div>}
    {exportError && <div className="artifact-notice danger" role="alert">{exportError}<button className="btn btn-sm" onClick={() => void exportSaved()}>重试导出</button></div>}
    {exportReady && <div className="artifact-notice" role="status">已核对已保存版本及来源，Markdown 文件已备好。<a className="btn btn-sm" href={exportReady.url} download={exportReady.filename}>下载 Markdown 文件</a></div>}
    {deleteUndo && <div className="artifact-notice" role="status">已从草稿删除所选块及其子块。<button className="btn btn-sm" onClick={() => { editDraft(deleteUndo); setDeleteUndo(null); setSaved(false) }}>撤销删除</button></div>}
    {artifact.head_version > baseline.head_version && <div className="artifact-notice">服务器有更新，当前编辑仍基于 v{baseline.head_version}。保存时会检查版本。</div>}
    <div className={`artifact-columns${!desktopEvidence && !mobile ? " without-evidence" : ""}`}>
      <div className="artifact-reading" ref={reading}>
        {view === 'map' ? <><Suspense fallback={<div className="empty" role="status">正在加载导图…</div>}><StudyMap body={body} onSelect={chooseBlock} /></Suspense>{selected && <div className="selected-concept"><p className="product-eyebrow">SELECTED CONCEPT</p><h3>{selected.title}</h3><p>{selected.content}</p><p>{selected.evidence_refs.length} 条关联依据 · 可在证据栏逐条切换</p>{!selected.evidence_refs.length && <p className="muted">这个节点没有来源引用。</p>}{!readOnly && <div className="product-actions"><button className="btn btn-sm" onClick={() => { setView('notes'); setEditing(true); setActiveEditor(selected?.block_id || body.blocks[0]?.block_id || null) }}>在笔记中编辑</button>{onAgentEdit && <button className="btn btn-sm btn-primary" onClick={() => requestAgentEdit(selected.block_id)}>让 Agent 修改这个节点</button>}</div>}</div>}</> : view === 'canvas' ? <Suspense fallback={<div className="empty" role="status">正在加载知识画布…</div>}><KnowledgeCanvas key={baseline.version.id} artifactId={artifact.id} versionId={baseline.version.id} headVersion={baseline.head_version} body={body} readOnly={readOnly || historical || preview || dirty} selectedBlock={selectedBlock} onSelect={chooseCanvasBlock} onBodyChange={next => { editDraft(next); setEditing(true); setSaved(false); setSaveError('') }} onAgentEdit={onAgentEdit ? requestAgentEdit : undefined} onEvidence={id => { onEvidence(id); openEvidence() }} /></Suspense> : <article className="study-paper">
          <div className="paper-meta"><span>LEARNING NOTES / {String(baseline.version.version).padStart(3, '0')}</span><span>理解，然后应用</span></div>
          {editing && !readOnly ? <label className="artifact-field">笔记标题<input maxLength={200} value={body.title} onChange={e => { setSaved(false); editDraft({ ...body, title: e.target.value }) }} /></label> : <h2>{body.title}</h2>}
          <p className="paper-intro">沿着视频整理概念，保留每一次回到来源的入口。</p>
          <div className="study-concept-index">{body.blocks.filter(block => block.parent_id === null).map((block, i) => <a key={block.block_id} href={`#block-${block.block_id}`}><span className="mono">{String(i + 1).padStart(2, '0')}</span><b>{block.title}</b><Icon name="chev-r" size="sm" /></a>)}</div>
          {body.warnings.length > 0 && <div className="paper-warning"><Icon name="alert" size="sm" /><span>{body.warnings.map(warningMessage).join(' · ')}</span></div>}
          {editing && !readOnly && <div className="study-structure-top"><button className="btn btn-sm" onClick={() => structure(current => addBlock(current, roots.at(-1)!.block.block_id, false))}>新增顶层块</button><span>仅当前段落展开编辑；其他内容保持可读。</span></div>}
          {body.blocks.map((block, i) => <section className={`study-block${block.parent_id ? ' is-child' : ''}${selectedBlock === block.block_id ? ' selected' : ''}`} key={block.block_id} id={`block-${block.block_id}`} onClick={() => { if (!editing) setSelectedBlock(block.block_id); if (!editing && !dirty && !historical && baseline.version) onStudyBlock?.(block.block_id,baseline.version.id) }}>
            <div className="study-block-meta"><span>{block.type === 'section' ? '章节' : block.type === 'concept' ? '概念' : block.type === 'example' ? '示例' : '笔记'} {String(i + 1).padStart(2, '0')}</span><span>{block.claim_origin === 'user' ? '人工编辑 · 引用待核对' : block.claim_origin === 'synthesis' ? '综合理解' : '来源整理'}</span></div>
            {editing && activeEditor === block.block_id && !readOnly ? <><label className="artifact-field">{`第 ${i + 1} 块标题`}<input value={block.title} maxLength={200} onChange={e => update(block.block_id, { title: e.target.value })} /></label><label className="artifact-field">{`第 ${i + 1} 块正文`}<textarea value={block.content} maxLength={8000} rows={Math.max(3, Math.min(10, block.content.split('\n').length + 2))} onChange={e => update(block.block_id, { content: e.target.value })} /></label><div className="study-block-actions"><button onClick={() => structure(current => addBlock(current, block.block_id, false))}>同级新增</button><button onClick={() => structure(current => addBlock(current, block.block_id, true))}>新增子块</button><button onClick={() => structure(current => moveBlock(current, block.block_id, 'up'))}>上移</button><button onClick={() => structure(current => moveBlock(current, block.block_id, 'down'))}>下移</button><button onClick={() => structure(current => moveBlock(current, block.block_id, 'indent'))}>降为子级</button><button onClick={() => structure(current => moveBlock(current, block.block_id, 'outdent'))}>升为同级</button><button onClick={() => structure(current => mergeWithNext(current, block.block_id))}>合并下一同级</button><button className="danger" onClick={() => setDeleteTarget(block.block_id)}>删除…</button></div></> : <><h3>{block.title}</h3><p className="study-block-content">{block.content}</p></>}
            {!readOnly && activeEditor !== block.block_id && <button className="study-edit-block" aria-label={`编辑段落 ${block.title}`} onClick={event => { event.stopPropagation(); setSelectedBlock(block.block_id); setActiveEditor(block.block_id); setEditing(true); setSaved(false) }}><Icon name="pencil" size="sm" />编辑此段</button>}
            <div className="study-citations">{block.evidence_refs.map((ref, n) => <button className={selectedEvidence === ref.evidence_id ? 'selected' : ''} key={`${ref.evidence_id}-${n}`} onClick={() => { setSelectedBlock(block.block_id); onEvidence(ref.evidence_id); openEvidence() }}><Icon name="play" size="sm" />{ref.relation === 'contradicts' ? '相反证据' : ref.relation === 'context' ? '背景证据' : '查看依据'} {n + 1}</button>)}{block.evidence_refs.length === 0 && <span>无来源引用 · 请自行核对</span>}</div>
            {(selectedBlock === block.block_id || activeEditor === block.block_id) && (onAskBlock || (onAgentEdit && !readOnly)) && <div className="study-block-actions">{onAskBlock && <button onClick={() => { if (dirty) setAskPrompt(block.block_id); else if (baseline.version) onAskBlock(block.block_id, baseline.version.id) }}>围绕这段提问</button>}{onAgentEdit && !readOnly && <button onClick={() => requestAgentEdit(block.block_id)}>让 Agent 修改这段</button>}</div>}
          </section>)}
          <footer className="paper-footer"><Icon name="shield-check" size="sm" />笔记帮助组织理解；引用关联不代表结论已经核实。</footer>
        </article>}
      </div>
      {!mobile && desktopEvidence && <aside className="artifact-evidence-desktop" aria-label="视频证据">{panel}</aside>}
    </div>
    {mobile && showEvidence && <div className="artifact-evidence-mobile"><Modal title="回到原视频" onClose={closeEvidence} width={440}>{panel}</Modal></div>}
    {deleteTarget && <Modal title="删除草稿块" onClose={() => setDeleteTarget(null)} width={440} footer={<><button className="btn" onClick={() => setDeleteTarget(null)}>取消</button><button className="btn btn-danger" onClick={() => { try { const next = deleteBlock(body, deleteTarget); setDeleteUndo(body); editDraft(next); setSaved(false); setSaveError(''); if (selectedBlock === deleteTarget) setSelectedBlock(null) } catch (error) { setSaveError(error instanceof Error ? error.message : '删除失败') } finally { setDeleteTarget(null) } }}>删除并保留撤销</button></>}><p>将从当前草稿删除“{body.blocks.find(item => item.block_id === deleteTarget)?.title}”及其 {descendantCount(body, deleteTarget) - 1} 个子块。保存前可撤销；已保存版本不会因此改变。</p></Modal>}
    {exportPrompt && <Modal title="先保存修改，再导出" onClose={() => setExportPrompt(false)} width={440} footer={<><button className="btn" onClick={() => setExportPrompt(false)}>取消导出</button><button className="btn btn-primary" disabled={saving} onClick={() => { setExportPrompt(false); void (async () => { const result = await save(); if (result?.version) await exportSaved(result.version.id) })() }}>保存并导出</button></>}><p>当前有未保存的修改。Markdown 仅导出服务端已保存版本。</p></Modal>}
    {askPrompt && <Modal title="先保存修改，再提问" onClose={() => setAskPrompt(null)} width={440} footer={<><button className="btn" onClick={() => setAskPrompt(null)}>取消提问</button><button className="btn btn-primary" disabled={saving} onClick={() => { const blockId=askPrompt; void (async () => { const result=await save(); if (result?.version) { setAskPrompt(null); onAskBlock?.(blockId,result.version.id) } })() }}>保存并提问</button></>}><p>当前段落有未保存的修改。保存成功后会把已保存段落带入问答；保存冲突时草稿留在这里。</p></Modal>}
    {agentPrompt !== false && <Modal title="先保存修改，再让 Agent 修改" onClose={() => setAgentPrompt(false)} width={440} footer={<><button className="btn" onClick={() => setAgentPrompt(false)}>继续人工编辑</button><button className="btn btn-primary" disabled={saving} onClick={() => { const blockId = agentPrompt; void (async () => { const result = await save(); if (result?.version) { setAgentPrompt(false); onAgentEdit?.(blockId, result) } })() }}>{saving ? '正在保存…' : '保存并交给 Agent'}</button></>}><p>Agent 必须以服务端已保存版本为基线。保存冲突时不会启动 Agent，本地草稿也会保留。</p>{saveError && <div className="artifact-agent-error" role="alert">{saveError}<p>Agent 尚未启动，本地草稿仍保留在当前页面。</p></div>}</Modal>}
    {showConflict && conflict && <Modal title="保留你的修改，核对新版本" onClose={() => setShowConflict(false)} width={780} footer={<><button className="btn" onClick={downloadDraft}>下载我的草稿</button><button className="btn" onClick={() => setShowConflict(false)}>继续编辑</button><button className="btn btn-primary" onClick={() => { if (!window.confirm('加载服务器版本会放弃当前修改。请先下载需要保留的草稿。')) return; setBaseline(conflict); setDraft(conflict.version?.body ?? null); setEditing(false); setActiveEditor(null); setDeleteUndo(null); setShowConflict(false); setConflict(null); setSaveError('') }}>加载服务器版本</button></>}><p className="product-description">当前草稿基于 v{baseline.head_version}，服务器已有 v{conflict.head_version}。不会自动覆盖任一版本。</p><div className="version-compare"><div><h4>我的草稿</h4><h3>{body.title}</h3>{body.blocks.map(block => <p key={block.block_id}><b>{block.title}</b><br />{block.content}</p>)}</div><div><h4>服务器版本</h4><h3>{conflict.version?.body.title}</h3>{conflict.version?.body.blocks.map(block => <p key={block.block_id}><b>{block.title}</b><br />{block.content}</p>)}</div></div></Modal>}
  </div>
}
