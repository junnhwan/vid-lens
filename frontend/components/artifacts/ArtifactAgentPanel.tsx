import { useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import { Modal } from '@/components/ui/Modal'
import { Icon } from '@/components/ui/Icon'
import { ApiError } from '@/lib/api'
import { artifactApi, artifactError, ContractError } from '@/lib/artifacts/api'
import type { ArtifactDetail, ArtifactEditMode, ArtifactEditOperation, ArtifactEditRun, StudyBlock, StudyRelation } from '@/lib/artifacts/schema'

type Attempt = { signature: string; key: string }

function requestKey(slot: MutableRefObject<Attempt | null>, signature: string): string {
  if (slot.current?.signature === signature) return slot.current.key
  const random = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(16).slice(2)}`
  const key = `artifact-edit-${random}`
  slot.current = { signature, key }
  return key
}

const activeStatuses = new Set<ArtifactEditRun['status']>(['pending', 'running'])
const basisLabels: Record<ArtifactEditOperation['basis'], string> = {
  user_instruction: '按你的要求',
  evidence_supported: '依据支持',
  evidence_conflict: '依据存在冲突',
}
const stageLabels: Record<string, string> = {
  queued: '等待执行', collecting: '准备已保存笔记', reading: '读取当前笔记', checking: '核对内容与依据',
  modifying: '整理受约束修改', saving: '保存新版本', saved: '新版本已保存', proposal_ready: '修改方案已生成', finalizing: '完成结果',
  completed: '已完成', failed: '执行失败', cancelled: '已取消', budget_exhausted: '本次运行已达上限',
}

const blockTypeLabels: Record<StudyBlock['type'], string> = { section: '章节', concept: '概念', example: '示例', note: '笔记' }
const relationLabels: Record<StudyBlock['evidence_refs'][number]['relation'], string> = { supports: '支持', context: '背景', contradicts: '相反' }
const semanticRelationLabels: Record<StudyRelation['type'], string> = { related_to: '相关', depends_on: '依赖', contrasts_with: '对比' }

function semanticRelationText(relation: StudyRelation, blocks: Map<string, StudyBlock>): string {
  const source = blocks.get(relation.source_block_id)?.title ?? relation.source_block_id
  const target = blocks.get(relation.target_block_id)?.title ?? relation.target_block_id
  const arrow = relation.type === 'depends_on' ? '→' : '↔'
  const origin = relation.origin === 'user' ? '人工整理' : '综合推断'
  return `${source} ${arrow} ${target} · ${semanticRelationLabels[relation.type]} · ${origin} · ${relation.evidence_refs.length ? `${relation.evidence_refs.length} 条依据` : '未附依据'}`
}

function retryableRead(cause: unknown): boolean {
  if (cause instanceof ContractError) return false
  if (cause instanceof ApiError) return cause.status === 408 || cause.status === 429 || cause.status >= 500
  return true
}

function retryDelay(attempt: number): number {
  return Math.min(10_000, 1200 * (2 ** Math.min(attempt, 3)))
}

function hierarchyText(block: StudyBlock, blocks: Map<string, StudyBlock>): string {
  if (!block.parent_id) return '顶层'
  const parents: string[] = []
  const visited = new Set<string>([block.block_id])
  let parentId: string | null = block.parent_id
  while (parentId && !visited.has(parentId)) {
    visited.add(parentId)
    const parent = blocks.get(parentId)
    parents.unshift(parent?.title ?? parentId)
    parentId = parent?.parent_id ?? null
  }
  return parents.join(' › ')
}

function blockText(block: StudyBlock | null | undefined, index: number | undefined, blocks: Map<string, StudyBlock>) {
  if (!block) return <p className="artifact-agent-empty-side">无</p>
  const refs = block.evidence_refs.map(ref => `${relationLabels[ref.relation]} · ${ref.evidence_id}${ref.chat_citation_id ? ` · 对话引用 ${ref.chat_citation_id}` : ''}`)
  return <>
    <b>{block.title}</b>
    {block.content && <p>{block.content}</p>}
    <dl className="artifact-agent-block-meta">
      <div><dt>类型</dt><dd>{blockTypeLabels[block.type]}</dd></div>
      <div><dt>层级</dt><dd>{hierarchyText(block, blocks)}</dd></div>
      <div><dt>正文位置</dt><dd>{index == null ? '未提供' : `第 ${index + 1} 项`}</dd></div>
      <div><dt>依据引用</dt><dd>{refs.length ? refs.join('；') : '无'}</dd></div>
    </dl>
  </>
}

export function ArtifactAgentPanel({ artifact, initialScope, initialRun = null, onClose, onArtifactChanged, onOperationChanged, onOpenEvidence }: {
  artifact: ArtifactDetail
  initialScope: string | null
  initialRun?: ArtifactEditRun | null
  onClose: () => void
  onArtifactChanged: () => Promise<ArtifactDetail>
  onOperationChanged?: (operation: ArtifactEditOperation) => void
  onOpenEvidence: (id: string) => void
}) {
  const scope = initialRun?.selected_block_ids[0] ?? initialScope
  const scopedBlock = scope ? artifact.version?.body.blocks.find(block => block.block_id === scope) : undefined
  const [instruction, setInstruction] = useState(initialRun?.instruction ?? '')
  const [run, setRun] = useState<ArtifactEditRun | null>(initialRun)
  const [operation, setOperation] = useState<ArtifactEditOperation | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [currentHead, setCurrentHead] = useState(artifact.head_version)
  const submitAttempt = useRef<Attempt | null>(null)
  const applyAttempt = useRef<Attempt | null>(null)
  const undoAttempt = useRef<Attempt | null>(null)
  const synchronizedVersions = useRef(new Set<string>())
  const runId = run?.id
  const runStatus = run?.status
  const operationId = run?.result && 'operation_id' in run.result ? run.result.operation_id : ''

  useEffect(() => {
    if (initialRun && (!run || initialRun.id !== run.id)) setRun(initialRun)
  }, [initialRun, run])

  useEffect(() => {
    if (!runId || !runStatus || !activeStatuses.has(runStatus)) return
    const controller = new AbortController()
    let timer: number | undefined
    let attempt = 0
    const poll = async () => {
      try {
        const next = await artifactApi.editRun(runId, controller.signal)
        if (controller.signal.aborted) return
        attempt = 0
        setRun(next)
        setError('')
        if (activeStatuses.has(next.status)) timer = window.setTimeout(() => void poll(), 1200)
      } catch (cause) {
        if (controller.signal.aborted) return
        setError(artifactError(cause))
        if (retryableRead(cause)) timer = window.setTimeout(() => void poll(), retryDelay(attempt++))
      }
    }
    timer = window.setTimeout(() => void poll(), 1200)
    return () => { controller.abort(); if (timer != null) window.clearTimeout(timer) }
  }, [runId, runStatus])

  useEffect(() => {
    if (!operationId) return
    const controller = new AbortController()
    let timer: number | undefined
    let attempt = 0
    const load = async () => {
      try {
        const next = await artifactApi.editOperation(operationId, controller.signal)
        if (controller.signal.aborted) return
        setOperation(next)
        onOperationChanged?.(next)
        if (next.result_version_id && !synchronizedVersions.current.has(next.result_version_id)) {
          const fresh = await onArtifactChanged()
          if (controller.signal.aborted) return
          synchronizedVersions.current.add(next.result_version_id)
          setCurrentHead(fresh.head_version)
        }
        if (!controller.signal.aborted) setError('')
      } catch (cause) {
        if (controller.signal.aborted) return
        setError(artifactError(cause))
        if (retryableRead(cause)) timer = window.setTimeout(() => void load(), retryDelay(attempt++))
      }
    }
    void load()
    return () => { controller.abort(); if (timer != null) window.clearTimeout(timer) }
  }, [operationId, onArtifactChanged, onOperationChanged])

  const statusText = useMemo(() => run ? stageLabels[run.stage] ?? (activeStatuses.has(run.status) ? 'Agent 正在处理' : run.status === 'completed' ? '已完成' : '未完成') : '', [run])

  async function submit(mode: ArtifactEditMode) {
    const normalized = instruction.trim()
    if (!normalized) { setError('请先填写修改要求。'); return }
    const input = { instruction: normalized, expected_head_version: currentHead, selected_block_ids: scope ? [scope] : [], mode }
    const signature = JSON.stringify(input)
    setBusy(true); setError(''); setOperation(null)
    try {
      setRun(await artifactApi.submitEdit(artifact.id, input, requestKey(submitAttempt, signature)))
    } catch (cause) { setError(artifactError(cause)) }
    finally { setBusy(false) }
  }

  async function apply() {
    if (!operation?.can_apply) return
    const signature = JSON.stringify({ operation_id: operation.id, expected_head_version: currentHead })
    setBusy(true); setError('')
    try {
      const next = await artifactApi.applyEdit(operation.id, currentHead, requestKey(applyAttempt, signature))
      setOperation(next)
      onOperationChanged?.(next)
      if (next.result_version_id) {
        const fresh = await onArtifactChanged()
        synchronizedVersions.current.add(next.result_version_id)
        setCurrentHead(fresh.head_version)
      }
    } catch (cause) { setError(artifactError(cause)) }
    finally { setBusy(false) }
  }

  async function undo() {
    if (!operation?.can_undo) return
    const signature = JSON.stringify({ operation_id: operation.id, expected_head_version: currentHead })
    setBusy(true); setError('')
    try {
      const next = await artifactApi.undoEdit(operation.id, currentHead, requestKey(undoAttempt, signature))
      setOperation(next)
      onOperationChanged?.(next)
      const fresh = await onArtifactChanged()
      setCurrentHead(fresh.head_version)
    } catch (cause) { setError(artifactError(cause)) }
    finally { setBusy(false) }
  }

  async function cancel() {
    if (!run?.can_cancel) return
    setBusy(true); setError('')
    try { setRun(await artifactApi.cancelEdit(run.id)) }
    catch (cause) { setError(artifactError(cause)) }
    finally { setBusy(false) }
  }

  const result = run?.result
  const diffBlocks = useMemo(() => {
    const before = new Map<string, StudyBlock>()
    const after = new Map<string, StudyBlock>()
    for (const block of artifact.version?.body.blocks ?? []) { before.set(block.block_id, block); after.set(block.block_id, block) }
    for (const change of operation?.changes ?? []) {
      if (change.before) before.set(change.before.block_id, change.before)
      if (change.after) after.set(change.after.block_id, change.after)
    }
    return { before, after }
  }, [artifact.version?.body.blocks, operation])
  return <Modal title="Agent 修订笔记" onClose={onClose} width={760} className="artifact-agent-modal">
    <div className="artifact-agent-scope"><span className="mono">EDIT SCOPE · v{artifact.version?.version ?? artifact.head_version}</span><b>{scopedBlock ? `只修改「${scopedBlock.title}」及其子段落` : `修改全文 · ${artifact.title}`}</b><p>Agent 只读取当前已保存版本和必要依据；范围外内容、引用与人工修改保持不变。</p></div>

    {!run && <div className="artifact-agent-request">
      <label className="artifact-agent-field">修改要求<textarea aria-label="修改要求" maxLength={2000} rows={5} value={instruction} onChange={event => { setInstruction(event.target.value); submitAttempt.current = null; setError('') }} placeholder="例如：纠正产品名称；把这一段拆成三个步骤；合并重复概念，并保留现有依据。" /></label>
      <div className="artifact-agent-modes">
        <button className="btn" disabled={busy} onClick={() => void submit('answer')}><Icon name="shield-check" size="sm" />只核对，不修改</button>
        <button className="btn" disabled={busy} onClick={() => void submit('preview')}><Icon name="file" size="sm" />先看修改方案</button>
        <button className="btn btn-primary" disabled={busy} onClick={() => void submit('apply')}><Icon name="wand" size="sm" />{busy ? '正在提交…' : '直接保存修改'}</button>
      </div>
      <p className="artifact-agent-hint">“只核对”不会写入；“先看方案”必须再次确认；“直接保存”会创建可撤销的新版本。</p>
    </div>}

    {run && activeStatuses.has(run.status) && <div className="artifact-agent-progress" role="status"><span className="agent-pulse" /><div><b>{statusText}</b><p>关闭面板不会取消任务，可以稍后从成果页继续查看。</p></div>{run.can_cancel && <button className="btn btn-sm" disabled={busy} onClick={() => void cancel()}>取消本次运行</button>}</div>}

    {run && !activeStatuses.has(run.status) && !result && <div className="artifact-agent-result danger" role="alert"><b>{run.status === 'cancelled' ? '本次修订已取消' : '本次修订未完成'}</b><p>{run.error_code ? `错误代码：${run.error_code}。没有创建或覆盖任何笔记版本。` : '没有创建或覆盖任何笔记版本。'}</p></div>}

    {result?.kind === 'answer' && <div className="artifact-agent-result" role="status" aria-live="polite"><span className="mono">READ ONLY</span><h4>核对结果</h4><p>{result.message}</p><EvidenceLinks ids={result.evidence_ids} onSelect={onOpenEvidence} label="核对依据" emptyText="本次核对未引用视频依据。" /></div>}
    {result?.kind === 'no_change' && <div className="artifact-agent-result" role="status" aria-live="polite"><span className="mono">NO CHANGE</span><h4>无需创建新版本</h4><p>{result.message}</p><EvidenceLinks ids={result.evidence_ids} onSelect={onOpenEvidence} label="核对依据" emptyText="本次判断未引用视频依据。" /></div>}

    {operation && <div className="artifact-agent-operation" role="status" aria-live="polite">
      <header><div><span className="mono">{operation.status === 'proposed' ? `PROPOSAL · BASE v${operation.base_version}` : operation.undo_version_id ? `UNDONE · ORIGINAL EDIT v${operation.base_version + 1}` : `SAVED VERSION · v${operation.base_version + 1}`}</span><h4>{operation.summary}</h4></div><span className={`artifact-agent-basis ${operation.basis}`}>{basisLabels[operation.basis]}</span></header>
      <div className="artifact-agent-counts"><span>新增 {operation.counts.added}</span><span>修改 {operation.counts.updated}</span><span>删除 {operation.counts.deleted}</span><span>移动 {operation.counts.moved}</span></div>
      <div className="artifact-agent-diff" aria-label="修改差异">{operation.changes.map((change, index) => <div className="artifact-agent-change" key={`${change.kind}-${change.block_id ?? 'title'}-${index}`}>
        <span className="mono">{change.kind === 'added' || change.kind === 'relation_added' ? 'ADDED' : change.kind === 'deleted' || change.kind === 'relation_removed' ? 'DELETED' : change.kind === 'moved' ? 'MOVED' : 'CHANGED'}</span>
        {(change.before_relation || change.after_relation) ? <div className="artifact-agent-before-after"><div><small>修改前</small><p>{change.before_relation ? semanticRelationText(change.before_relation, diffBlocks.before) : '无关系'}</p></div><div><small>修改后</small><p>{change.after_relation ? semanticRelationText(change.after_relation, diffBlocks.after) : '无关系'}</p></div></div> : (change.before_title != null || change.after_title != null) ? <div className="artifact-agent-before-after"><div><small>修改前</small><p>{change.before_title}</p></div><div><small>修改后</small><p>{change.after_title}</p></div></div> : <div className="artifact-agent-before-after"><div><small>修改前</small>{blockText(change.before, change.before_index, diffBlocks.before)}</div><div><small>修改后</small>{blockText(change.after, change.after_index, diffBlocks.after)}</div></div>}
      </div>)}</div>
      <EvidenceLinks ids={operation.evidence_ids} onSelect={onOpenEvidence} />
      <div className="artifact-agent-actions">{operation.can_apply && <button className="btn btn-primary" disabled={busy} onClick={() => void apply()}>应用这份方案</button>}{operation.can_undo && <button className="btn" disabled={busy} onClick={() => void undo()}>撤销这次修改</button>}{operation.undo_version_id && <span>已用新版本安全撤销；历史版本仍保留。</span>}</div>
    </div>}

    {error && <div className="artifact-agent-error" role="alert">{error}{error.includes('版本') || error.includes('冲突') ? <p>请刷新当前成果后重新发起；不会覆盖服务器上的新版本。</p> : null}</div>}
  </Modal>
}

function EvidenceLinks({ ids, onSelect, label = '修改依据', emptyText = '本次修改没有声称来自视频依据。' }: { ids: string[]; onSelect: (id: string) => void; label?: string; emptyText?: string }) {
  if (!ids.length) return <p className="artifact-agent-evidence empty-evidence">{emptyText}</p>
  return <div className="artifact-agent-evidence"><span>{label}</span>{ids.map((id, index) => <button className="btn btn-sm" key={id} onClick={() => onSelect(id)}><Icon name="play" size="sm" />依据 {index + 1}</button>)}</div>
}
