import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ReactFlow, Background, Controls, Handle, MarkerType, Position, ReactFlowProvider, applyNodeChanges, type Connection, type Edge, type Node, type NodeChange, type NodeProps, useReactFlow } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { artifactApi, artifactError } from '@/lib/artifacts/api'
import { addRelation, removeRelation } from '@/lib/artifacts/edit'
import { canvasDescendants, canvasKind, computeCanvasLayout, defaultCanvasNode, emptyCanvasLayout, fillCanvasPositions, visibleCanvasIds } from '@/lib/artifacts/canvas'
import type { CanvasLayout, CanvasLayoutView, StudyBlock, StudyBody, StudyRelation } from '@/lib/artifacts/schema'
import { useMediaQuery } from '@/components/ui/useMediaQuery'
import './knowledge-canvas.css'

type CardData = { block: StudyBlock; kind: string; direction: CanvasLayout['direction']; collapsed: boolean; pinned: boolean; select: () => void }
type CardNode = Node<CardData, 'knowledge'>
const relationNames: Record<StudyRelation['type'], string> = { related_to: '相关', depends_on: '依赖', contrasts_with: '对比' }
const kindNames: Record<string, string> = { section: '章节', concept: '概念', operation: '操作', command: '命令 / 示例', note: '笔记' }

function KnowledgeCard({ data, selected }: NodeProps<CardNode>) {
  const horizontal = data.direction === 'RIGHT'
  return <div className={`knowledge-card ${data.kind}${selected ? ' selected' : ''}`} role="button" tabIndex={0} aria-label={`查看${data.block.title}`} onClick={data.select} onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); data.select() } }}>
    <Handle type="target" id="in" position={horizontal ? Position.Left : Position.Top} />
    <div className="knowledge-card-top"><span>{kindNames[data.kind]}</span>{data.pinned && <span title="位置已固定">◆</span>}{data.collapsed && <span title="子节点已折叠">▸</span>}</div>
    <strong title={data.block.title}>{data.block.title}</strong>
    <p>{data.block.content || '选择卡片查看完整内容'}</p>
    <div className="knowledge-card-bottom"><span>{data.block.evidence_refs.length ? `${data.block.evidence_refs.length} 条依据` : '待核对'}</span><span>↗</span></div>
    <Handle type="source" id="out" position={horizontal ? Position.Right : Position.Bottom} />
  </div>
}
const nodeTypes = { knowledge: KnowledgeCard }

export function KnowledgeCanvas(props: { artifactId: string; versionId: string; headVersion: number; body: StudyBody; readOnly: boolean; selectedBlock: string | null; onSelect: (id: string) => void; onBodyChange: (body: StudyBody) => void; onAgentEdit?: (id: string | null) => void; onEvidence: (id: string) => void }) {
  return <ReactFlowProvider><CanvasInner {...props} /></ReactFlowProvider>
}

function CanvasInner({ artifactId, versionId, headVersion, body, readOnly, selectedBlock, onSelect, onBodyChange, onAgentEdit, onEvidence }: Parameters<typeof KnowledgeCanvas>[0]) {
  const flow = useReactFlow<CardNode, Edge>()
  const reduceMotion = useMediaQuery('(prefers-reduced-motion: reduce)')
  const [focusEnabled, setFocusEnabled] = useState(false)
  const [view, setView] = useState<CanvasLayoutView | null>(null)
  const [nodes, setNodes] = useState<CardNode[]>([])
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)
  const [selectedRelation, setSelectedRelation] = useState<string | null>(null)
  const [rename, setRename] = useState('')
  const [relationTarget, setRelationTarget] = useState('')
  const [relationType, setRelationType] = useState<StudyRelation['type']>('related_to')
  const [aiInstruction, setAiInstruction] = useState('')
  const viewRef = useRef<CanvasLayoutView | null>(null)
  const versionRef = useRef(versionId); versionRef.current = versionId
  const bodyRef = useRef(body); bodyRef.current = body
  const layoutGeneration = useRef(0)
  const sessionGeneration = useRef(0)
  const saveGeneration = useRef(0)
  const mounted = useRef(false)
  const saveQueue = useRef(Promise.resolve())
  const saveBlocked = useRef(false)
  const undoStack = useRef<CanvasLayout[]>([])
  const redoStack = useRef<CanvasLayout[]>([])
  const [, setHistoryTick] = useState(0)
  const viewportTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const viewportReady = useRef(false)
  const setCurrent = useCallback((next: CanvasLayoutView) => { viewRef.current = next; setView(next) }, [])
  useEffect(() => {
    let cancelled = false
    mounted.current = true
    sessionGeneration.current++
    setView(null); viewRef.current = null; setError(''); setNotice(''); setBusy(false); saveBlocked.current = false; viewportReady.current = false; undoStack.current = []; redoStack.current = []; setHistoryTick(tick => tick + 1)
    void artifactApi.canvasLayout(artifactId, versionId).then(async loaded => {
      if (cancelled) return
      if (!Object.keys(loaded.layout.nodes).length) {
        try { const arranged = await computeCanvasLayout(bodyRef.current, loaded.layout, loaded.layout.direction); if (!cancelled) loaded = { ...loaded, layout: arranged.layout } }
        catch { /* deterministic grid remains available below */ }
      }
      if (!cancelled) setCurrent({ ...loaded, layout: fillCanvasPositions(bodyRef.current, loaded.layout) })
    }).catch(e => { if (!cancelled) setError(artifactError(e)) })
    return () => { cancelled = true; mounted.current = false; sessionGeneration.current++; layoutGeneration.current++; if (viewportTimer.current) clearTimeout(viewportTimer.current) }
  }, [artifactId, versionId, setCurrent])

  const layout = view?.layout ?? emptyCanvasLayout()
  const visible = useMemo(() => visibleCanvasIds(body, layout), [body, layout])
  const shownBlocks = useMemo(() => body.blocks.filter(block => visible.has(block.block_id)), [body, visible])
  const focusedIds = useMemo(() => {
    if (!focusEnabled || !selectedBlock) return null
    const ids = canvasDescendants(body, selectedBlock)
    let parent = body.blocks.find(block => block.block_id === selectedBlock)?.parent_id
    while (parent && !ids.has(parent)) { ids.add(parent); parent = body.blocks.find(block => block.block_id === parent)?.parent_id }
    for (const relation of body.relations ?? []) {
      if (relation.source_block_id === selectedBlock) ids.add(relation.target_block_id)
      if (relation.target_block_id === selectedBlock) ids.add(relation.source_block_id)
    }
    return ids
  }, [body, selectedBlock, focusEnabled])
  useEffect(() => {
    if (!view) return
    setNodes(previous => shownBlocks.map((block, index) => {
      const saved = layout.nodes[block.block_id] ?? defaultCanvasNode(index, layout.direction)
      const transient = previous.find(node => node.id === block.block_id)
      const kind = saved.style === 'auto' ? canvasKind(block.type, block.content) : saved.style
      return { id: block.block_id, type: 'knowledge', className: focusedIds && !focusedIds.has(block.block_id) ? 'knowledge-muted' : '', position: transient?.dragging ? transient.position : saved.position, width: saved.width, height: saved.height, draggable: !readOnly, selected: block.block_id === selectedBlock, data: { block, kind, direction: layout.direction, collapsed: saved.collapsed, pinned: saved.pinned, select: () => { setSelectedRelation(null); setFocusEnabled(true); onSelect(block.block_id) } } }
    }))
  }, [view, body, shownBlocks, layout, readOnly, selectedBlock, focusedIds, onSelect])

  const edges = useMemo<Edge[]>(() => {
    const tree = body.blocks.filter(block => block.parent_id && visible.has(block.block_id) && visible.has(block.parent_id)).map(block => ({ id: `tree-${block.block_id}`, source: block.parent_id!, target: block.block_id, sourceHandle: 'out', targetHandle: 'in', type: 'smoothstep', className: 'knowledge-tree-edge', selectable: false }))
    const semantic = (body.relations ?? []).filter(rel => visible.has(rel.source_block_id) && visible.has(rel.target_block_id)).map(rel => ({ id: rel.id, source: rel.source_block_id, target: rel.target_block_id, sourceHandle: 'out', targetHandle: 'in', type: 'smoothstep', label: relationNames[rel.type], className: `knowledge-relation-edge ${rel.type}`, animated: false, markerEnd: rel.type === 'depends_on' ? { type: MarkerType.ArrowClosed } : undefined }))
    return [...tree, ...semantic].map(edge => ({ ...edge, className: `${edge.className}${focusedIds ? focusedIds.has(edge.source) && focusedIds.has(edge.target) ? ' knowledge-related' : ' knowledge-muted' : ''}` }))
  }, [body, visible, focusedIds])

  const persist = useCallback((next: CanvasLayoutView, recordHistory = true) => {
    if (readOnly || saveBlocked.current) return
    const before = viewRef.current
    if (recordHistory && before && before.layout !== next.layout) { undoStack.current.push(before.layout); if (undoStack.current.length > 50) undoStack.current.shift(); redoStack.current = []; setHistoryTick(tick => tick + 1) }
    setCurrent(next); setNotice('布局待保存…')
    const generation = saveGeneration.current
    saveQueue.current = saveQueue.current.then(async () => {
      if (generation !== saveGeneration.current || saveBlocked.current || versionRef.current !== next.content_version_id) return
      const current = viewRef.current
      if (!current || current.content_version_id !== next.content_version_id) return
      try {
        const saved = await artifactApi.saveCanvasLayout(artifactId, next.content_version_id, current.revision, next.layout, crypto.randomUUID())
        if (generation !== saveGeneration.current || versionRef.current !== next.content_version_id) return
        // A later local edit keeps its coordinates; only the revision advances.
        const latest = viewRef.current
        const reconciled = latest && latest.layout !== next.layout ? { ...latest, revision: saved.revision } : saved
        viewRef.current = reconciled
        if (mounted.current) { setView(reconciled); setNotice('布局已保存') }
      } catch (e) { if (generation === saveGeneration.current && versionRef.current === next.content_version_id) { saveBlocked.current = true; if (mounted.current) { setError(artifactError(e)); setNotice('布局未保存') } } }
    })
  }, [artifactId, readOnly, setCurrent])

  const undoLayout = async () => {
    const current = viewRef.current
    if (!current) return
    const session = sessionGeneration.current
    let previous = undoStack.current.pop()
    if (!previous && current.revision > 1) {
      try { previous = (await artifactApi.canvasLayout(artifactId, versionId, current.revision - 1)).layout } catch (e) { if (session === sessionGeneration.current) setError(artifactError(e)); return }
    }
    if (session !== sessionGeneration.current || viewRef.current !== current) return
    if (!previous) return
    redoStack.current.push(current.layout); setHistoryTick(tick => tick + 1)
    persist({ ...current, layout: previous }, false)
  }
  const redoLayout = () => {
    const current = viewRef.current
    const next = redoStack.current.pop()
    if (!current || !next) return
    undoStack.current.push(current.layout); setHistoryTick(tick => tick + 1)
    persist({ ...current, layout: next }, false)
  }

  const updateLayout = (change: (layout: CanvasLayout) => CanvasLayout) => {
    const current = viewRef.current
    if (current) persist({ ...current, layout: change(current.layout) })
  }
  const updateNode = (id: string, change: Partial<CanvasLayout['nodes'][string]>) => updateLayout(layout => ({ ...layout, nodes: { ...layout.nodes, [id]: { ...(layout.nodes[id] ?? defaultCanvasNode(body.blocks.findIndex(block => block.block_id === id), layout.direction)), ...change } } }))
  const onNodesChange = useCallback((changes: NodeChange<CardNode>[]) => { setNodes(current => applyNodeChanges(changes, current)) }, [])
  const onConnect = (connection: Connection) => {
    if (readOnly || !connection.source || !connection.target) return
    try { onBodyChange(addRelation(body, connection.source, connection.target, relationType)); setNotice('关系已加入笔记草稿，请保存内容版本') }
    catch (e) { setError(e instanceof Error ? e.message : '无法添加关系') }
  }
  const arrange = async (scope?: Set<string>, direction = layout.direction, density: 'comfortable' | 'compact' = 'comfortable') => {
    const current = viewRef.current
    if (!current) return
    const content = bodyRef.current
    const token = ++layoutGeneration.current
    setBusy(true); setError('')
    try {
      const result = await computeCanvasLayout(content, current.layout, direction, scope, density)
      if (token !== layoutGeneration.current || versionRef.current !== current.content_version_id || viewRef.current?.revision !== current.revision || viewRef.current?.layout !== current.layout || bodyRef.current !== content) return
      if (result.collisions.length) { setNotice(`${result.collisions.length} 张卡片无法避开固定卡片；未应用本次排版`); return }
      persist({ ...current, layout: result.layout })
      requestAnimationFrame(() => void flow.fitView({ padding: .18, duration: reduceMotion ? 0 : 220 }))
    } catch { setError('排版失败。可以继续用文字大纲浏览，或重试排版。') }
    finally { if (token === layoutGeneration.current) setBusy(false) }
  }
  const suggestArrangement = async () => {
    const current = viewRef.current
    if (!current || !aiInstruction.trim()) return
    const content = bodyRef.current
    const token = ++layoutGeneration.current
    setBusy(true); setError(''); setNotice('Agent 正在选择受限排版方案…')
    try {
      const plan = await artifactApi.suggestCanvasLayout(artifactId, { instruction: aiInstruction.trim(), content_version_id: versionId, expected_head_version: headVersion, expected_layout_revision: current.revision, selected_block_id: selectedBlock ?? '' })
      if (token !== layoutGeneration.current || versionRef.current !== versionId || viewRef.current?.revision !== current.revision || viewRef.current?.layout !== current.layout || bodyRef.current !== content) return
      setNotice(`Agent 建议：${plan.summary || '调整画布布局'}`)
      const scope = plan.scope === 'selected' && selectedBlock ? canvasDescendants(bodyRef.current, selectedBlock) : undefined
      await arrange(scope, plan.direction, plan.density)
    } catch (e) { setError(artifactError(e)); setNotice('AI 排版未应用') }
    finally { if (token === layoutGeneration.current) setBusy(false) }
  }
  const selected = body.blocks.find(block => block.block_id === selectedBlock)
  const relation = (body.relations ?? []).find(item => item.id === selectedRelation)
  useEffect(() => { setRename(selected?.title ?? '') }, [selected?.block_id, selected?.title])
  const hiddenBlocks = body.blocks.filter(block => layout.nodes[block.block_id]?.hidden)
  const reloadLayout = async () => {
    const session = sessionGeneration.current
    const current = viewRef.current
    try {
      const loaded = await artifactApi.canvasLayout(artifactId, versionId)
      if (session !== sessionGeneration.current || viewRef.current !== current) return
      // Invalidate work queued against the discarded local layout.
      sessionGeneration.current++; saveGeneration.current++; layoutGeneration.current++; setBusy(false)
      saveBlocked.current = false; undoStack.current = []; redoStack.current = []; setHistoryTick(tick => tick + 1); setError(''); setNotice('已恢复服务器布局'); setCurrent(loaded)
    } catch (e) { if (session === sessionGeneration.current) setError(artifactError(e)) }
  }
  return <div className="knowledge-canvas-shell">
    <div className="knowledge-toolbar">
      <div><span className="knowledge-eyebrow">KNOWLEDGE CANVAS</span><h2>可编辑知识画布</h2><p>正文与图共用块；位置、折叠和隐藏单独保存。</p></div>
      <div className="knowledge-toolbar-actions"><button className="btn btn-sm" disabled={!selectedBlock} aria-pressed={focusEnabled} onClick={() => setFocusEnabled(value => !value)}>{focusEnabled ? '显示全部关系' : '突出关联'}</button>
        <button className="btn btn-sm" disabled={busy || !view || readOnly} onClick={() => void arrange(undefined, 'RIGHT')}>横向排版</button>
        <button className="btn btn-sm" disabled={busy || !view || readOnly} onClick={() => void arrange(undefined, 'DOWN')}>纵向排版</button>
        <button className="btn btn-sm" disabled={busy || !selected || readOnly} onClick={() => selected && void arrange(canvasDescendants(body, selected.block_id))}>排版选中分支</button>
        <button className="btn btn-sm" disabled={!view} onClick={() => void flow.fitView({ padding: .2, duration: reduceMotion ? 0 : 220 })}>适应画布</button>
        <button className="btn btn-sm" disabled={readOnly || !view || !undoStack.current.length && view.revision < 2} onClick={() => void undoLayout()}>撤销布局</button>
        <button className="btn btn-sm" disabled={readOnly || !redoStack.current.length} onClick={redoLayout}>重做布局</button>
        {onAgentEdit && !readOnly && <button className="btn btn-sm btn-primary" onClick={() => onAgentEdit(null)}>让 Agent 修改图内容</button>}
      </div>
    </div>
    {!readOnly && <div className="knowledge-ai"><label htmlFor="canvas-ai-instruction">AI 局部排版</label><input id="canvas-ai-instruction" value={aiInstruction} maxLength={1000} onChange={event => setAiInstruction(event.target.value)} placeholder="例如：把这组排紧凑些，保留固定卡片" /><button className="btn btn-sm" disabled={busy || !view || !aiInstruction.trim()} onClick={() => void suggestArrangement()}>生成并应用排版</button></div>}
    {error && <div className="knowledge-alert" role="alert">{error} <button onClick={() => void reloadLayout()}>载入服务器布局</button></div>}
    {notice && <div className="knowledge-status" role="status">{notice}</div>}
    <div className="knowledge-main">
      <div className="knowledge-flow" aria-label="知识画布">
        {view ? <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} onNodesChange={onNodesChange} onNodeClick={(_, node) => { setSelectedRelation(null); setFocusEnabled(true); onSelect(node.id) }} onNodeDragStop={(_, node) => updateNode(node.id, { position: node.position, pinned: true })} onEdgeClick={(_, edge) => { if (!edge.id.startsWith('tree-')) setSelectedRelation(edge.id) }} onConnect={onConnect} onMoveEnd={(_, viewport) => { if (!viewportReady.current) { viewportReady.current = true; if (viewRef.current?.revision === 0 && bodyRef.current.blocks.length <= 30) return } if (readOnly || !viewRef.current) return; if (viewportTimer.current) clearTimeout(viewportTimer.current); viewportTimer.current = setTimeout(() => { const current = viewRef.current; if (current && JSON.stringify(current.layout.viewport) !== JSON.stringify(viewport)) persist({ ...current, layout: { ...current.layout, viewport } }) }, 450) }} nodesConnectable={!readOnly} elementsSelectable minZoom={.1} maxZoom={3} defaultViewport={layout.viewport} deleteKeyCode={null} colorMode="system" fitView={view.revision === 0 && body.blocks.length <= 30} proOptions={{ hideAttribution: false }}>
          <Background gap={24} size={1} /><Controls showInteractive={false} />
        </ReactFlow> : <div className="knowledge-loading" role="status">正在读取画布布局…</div>}
      </div>
      <aside className="knowledge-inspector" aria-label="画布详情">
        {relation ? <><span className="knowledge-eyebrow">RELATION</span><h3>{relationNames[relation.type]}</h3><p>{body.blocks.find(block => block.block_id === relation.source_block_id)?.title} → {body.blocks.find(block => block.block_id === relation.target_block_id)?.title}</p><p>{relation.origin === 'user' ? '人工整理' : 'AI 推断，依据见下方'}</p><div className="knowledge-evidence"><b>依据 · {relation.evidence_refs.length}</b>{relation.evidence_refs.map((ref, index) => <button key={`${ref.evidence_id}-${index}`} onClick={() => onEvidence(ref.evidence_id)}>查看依据 {index + 1} ↗</button>)}{!relation.evidence_refs.length && <span>无来源引用，请自行核对。</span>}</div>{!readOnly && <button className="btn btn-sm" onClick={() => { onBodyChange(removeRelation(body, relation.id)); setSelectedRelation(null); setNotice('关系已从草稿移除，请保存内容版本') }}>移除关系</button>}</> : selected ? <>
          <span className="knowledge-eyebrow">{kindNames[canvasKind(selected.type, selected.content)]} · {selected.claim_origin === 'user' ? '人工整理' : '来源整理'}</span>
          <h3>{selected.title}</h3><p className="knowledge-full-content">{selected.content || '暂无正文'}</p>
          <div className="knowledge-evidence"><b>依据 · {selected.evidence_refs.length}</b>{selected.evidence_refs.map((ref, index) => <button key={`${ref.evidence_id}-${index}`} onClick={() => onEvidence(ref.evidence_id)}>查看依据 {index + 1} ↗</button>)}{!selected.evidence_refs.length && <span>无来源引用，请自行核对。</span>}</div>
          {!readOnly && <div className="knowledge-edit-controls">
            <label>节点名称<input value={rename} maxLength={200} onChange={event => setRename(event.target.value)} /></label>
            <button className="btn btn-sm" disabled={!rename.trim() || rename === selected.title} onClick={() => { onBodyChange({ ...body, blocks: body.blocks.map(block => block.block_id === selected.block_id ? { ...block, title: rename.trim(), claim_origin: 'user' } : block) }); setNotice('名称已加入笔记草稿，请保存内容版本') }}>改名并同步笔记</button>
            <div className="knowledge-row"><button className="btn btn-sm" onClick={() => updateNode(selected.block_id, { pinned: !layout.nodes[selected.block_id]?.pinned })}>{layout.nodes[selected.block_id]?.pinned ? '取消固定' : '固定位置'}</button><button className="btn btn-sm" onClick={() => updateNode(selected.block_id, { collapsed: !layout.nodes[selected.block_id]?.collapsed })}>{layout.nodes[selected.block_id]?.collapsed ? '展开子级' : '折叠子级'}</button><button className="btn btn-sm" onClick={() => updateNode(selected.block_id, { hidden: true })}>在图中隐藏</button></div>
            <label>连接到<select value={relationTarget} onChange={event => setRelationTarget(event.target.value)}><option value="">选择节点</option>{body.blocks.filter(block => block.block_id !== selected.block_id).map(block => <option key={block.block_id} value={block.block_id}>{block.title}</option>)}</select></label>
            <label>关系类型<select value={relationType} onChange={event => setRelationType(event.target.value as StudyRelation['type'])}><option value="related_to">相关</option><option value="depends_on">依赖（有方向）</option><option value="contrasts_with">对比</option></select></label>
            <button className="btn btn-sm" disabled={!relationTarget} onClick={() => { try { onBodyChange(addRelation(body, selected.block_id, relationTarget, relationType)); setNotice('关系已加入笔记草稿，请保存内容版本') } catch (e) { setError(e instanceof Error ? e.message : '无法添加关系') } }}>添加关系</button>
            {onAgentEdit && <button className="btn btn-sm btn-primary" onClick={() => onAgentEdit(selected.block_id)}>让 Agent 修改内容</button>}
          </div>}
        </> : <div className="knowledge-empty"><span className="knowledge-eyebrow">EXPLORE</span><h3>选择一张卡片</h3><p>查看完整正文、依据和编辑操作。拖动卡片会固定当前位置；连线只添加关系，不改变章节层级。</p></div>}
        {hiddenBlocks.length > 0 && <div className="knowledge-hidden"><b>已隐藏 · {hiddenBlocks.length}</b>{hiddenBlocks.map(block => <button key={block.block_id} disabled={readOnly} onClick={() => updateNode(block.block_id, { hidden: false })}>{block.title} · 显示</button>)}</div>}
      </aside>
    </div>
    <details className="knowledge-outline"><summary>文字大纲 · {body.blocks.length} 块</summary><ol>{body.blocks.map(block => <li key={block.block_id}><button onClick={() => onSelect(block.block_id)}>{block.title}</button><span>{block.parent_id ? '子级' : '章节'} · {block.evidence_refs.length} 条依据</span></li>)}</ol></details>
  </div>
}
