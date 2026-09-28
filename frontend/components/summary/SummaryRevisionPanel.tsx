import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { EffectiveSummaryView, SummaryEditOperation, VideoTermRuleSet } from '@/lib/types'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { artifactApi } from '@/lib/artifacts/api'
import type { Artifact, ArtifactEditRun } from '@/lib/artifacts/schema'

type Scope = 'once' | 'remember'

function errorText(error: unknown): string {
  if (error instanceof ApiError) return `${error.message}${error.code ? ` (${error.code})` : ''}`
  return error instanceof Error ? error.message : '操作失败，请稍后重试'
}

export function SummaryRevisionPanel({ taskId, readOnly, onChanged }: { taskId: number; readOnly: boolean; onChanged: () => Promise<void> | void }) {
  const [summary, setSummary] = useState<EffectiveSummaryView | null>(null)
  const [rules, setRules] = useState<VideoTermRuleSet | null>(null)
  const [operation, setOperation] = useState<SummaryEditOperation | null>(null)
  const [instruction, setInstruction] = useState('')
  const [scope, setScope] = useState<Scope>('once')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [context, setContext] = useState('')
  const [exclusions, setExclusions] = useState('')
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [targetResults, setTargetResults] = useState<string[]>([])
  const [ruleSaveFailed, setRuleSaveFailed] = useState(false)
  const [notes, setNotes] = useState<Artifact[]>([])
  const [noteTarget, setNoteTarget] = useState('')
  const [noteRun, setNoteRun] = useState<ArtifactEditRun | null>(null)
  const [noteFailed, setNoteFailed] = useState(false)
  const [submittedNoteTarget, setSubmittedNoteTarget] = useState('')
  const noteAttempt = useRef<{ id: string; prompt: string; head: number; key: string } | null>(null)

  const refresh = useCallback(async () => {
    const [nextSummary, nextRules] = await Promise.all([api.getSummary(taskId), api.getTermRules(taskId)])
    setSummary(nextSummary)
    setRules(nextRules)
    await onChanged()
  }, [taskId, onChanged])

  useEffect(() => {
    let active = true
    void Promise.all([api.getSummary(taskId), api.getTermRules(taskId), api.getLatestSummaryOperation(taskId)]).then(([nextSummary, nextRules, latest]) => {
      if (active) { setSummary(nextSummary); setRules(nextRules); setOperation(latest); if (latest?.instruction) setInstruction(latest.instruction) }
    }).catch(error => { if (active) setMessage(errorText(error)) })
    return () => { active = false }
  }, [taskId])

  const operationID = operation?.id
  const operationStatus = operation?.status
  useEffect(() => {
    if (!operationID || operationStatus !== 'running') return
    let active = true
    let timer: number | undefined
    const poll = async () => {
      try {
        const next = await api.getSummaryOperation(taskId, operationID)
        if (!active) return
        setOperation(next)
        if (next.status === 'failed') { setKey(''); setMessage(`摘要修订失败：${next.error_code || '请重试'}`) }
        if (next.status === 'committed') await refresh()
        if (next.status === 'running') timer = window.setTimeout(() => void poll(), 1500)
      } catch (error) {
        if (!active) return
        setMessage(`读取摘要执行状态失败：${errorText(error)}；仍可刷新页面恢复。`)
        timer = window.setTimeout(() => void poll(), 4000)
      }
    }
    timer = window.setTimeout(() => void poll(), 1500)
    return () => { active = false; if (timer != null) window.clearTimeout(timer) }
  }, [taskId, operationID, operationStatus, refresh])

  useEffect(() => {
    let active = true
    void artifactApi.list(1, taskId).then(page => { if (active) setNotes(page.list) }).catch(() => { if (active) setNotes([]) })
    return () => { active = false }
  }, [taskId])

  useEffect(() => {
    if (!noteRun || (noteRun.status !== 'pending' && noteRun.status !== 'running')) return
    const timer = window.setTimeout(() => {
      void artifactApi.editRun(noteRun.id).then(next => {
        setNoteRun(next)
        if (next.status === 'completed') {
          setTargetResults(previous => [...previous, next.result?.kind === 'committed' ? `笔记：已保存新版本（操作 ${next.result.operation_id}）。` : `笔记：未写入（${next.result?.kind === 'no_change' ? '无需修改' : '返回了非修订结果'}）。`])
        } else if (next.status !== 'pending' && next.status !== 'running') {
          setNoteFailed(true)
          setTargetResults(previous => [...previous, `笔记：修改失败（${next.error_code || next.status}）；摘要已独立保存。`])
        }
      }).catch(error => setMessage(`读取笔记执行状态失败：${errorText(error)}；可在笔记页继续查看。`))
    }, 1500)
    return () => window.clearTimeout(timer)
  }, [noteRun])

  const submitNote = async (id: string, prompt: string) => {
    setSubmittedNoteTarget(id)
    if (noteRun && noteRun.status !== 'pending' && noteRun.status !== 'running') noteAttempt.current = null
    const detail = await artifactApi.get(id)
    const attempt = noteAttempt.current?.id === id && noteAttempt.current.prompt === prompt
      ? noteAttempt.current
      : { id, prompt, head: detail.head_version, key: `summary-note-${crypto.randomUUID()}` }
    noteAttempt.current = attempt
    const run = await artifactApi.submitEdit(id, { instruction: prompt, expected_head_version: attempt.head, selected_block_ids: [], mode: 'apply' }, attempt.key)
    setNoteRun(run)
    setNoteFailed(false)
    setTargetResults(previous => [...previous, `笔记「${detail.title}」：已提交独立修订，正在等待结果（run ${run.id}）。`])
    if (run.status === 'completed') setTargetResults(previous => [...previous, run.result?.kind === 'committed' ? `笔记：已保存新版本（操作 ${run.result.operation_id}）。` : '笔记：运行完成但未写入修订。'])
    if (run.status !== 'pending' && run.status !== 'running' && run.status !== 'completed') {
      setNoteFailed(true)
      setTargetResults(previous => [...previous, `笔记：修改失败（${run.error_code || run.status}）；摘要已独立保存。`])
    }
  }

  const saveRememberedRule = async (linkedOperationId?: string) => {
    const currentRules = await api.getTermRules(taskId)
    const nextRules = await api.saveTermRule(taskId, {
      expected_version: currentRules.version, linked_operation_id: linkedOperationId,
      from: from.trim(), to: to.trim(), context: context.trim(), exclusions: exclusions.split('\n').map(value => value.trim()).filter(Boolean),
    })
    setRules(nextRules)
    setRuleSaveFailed(false)
    setTargetResults(previous => [...previous, `术语规则：已保存 v${nextRules.version}；只影响后续新运行。`])
  }

  const preview = async () => {
    if (!summary || !instruction.trim() || busy) return
    setBusy(true); setMessage(''); setTargetResults([])
    setNoteRun(null); setNoteFailed(false)
    const requestKey = key || crypto.randomUUID()
    setKey(requestKey)
    try {
      const next = await api.editSummary(taskId, { instruction: instruction.trim(), expected_revision: summary.revision, mode: 'preview' }, requestKey)
      setOperation(next)
      if (next.status === 'failed') { setKey(''); setMessage(`摘要修订失败：${next.error_code || '请重试'}`) }
    } catch (error) { setMessage(`${errorText(error)}；可用同一请求重试，避免重复修改。`) }
    finally { setBusy(false) }
  }

  const apply = async () => {
    if (!summary || !operation || busy) return
    setBusy(true); setMessage(''); setTargetResults([])
    setRuleSaveFailed(false)
    try {
      const committed = await api.applySummaryOperation(taskId, operation.id, summary.revision)
      setOperation(committed)
      setTargetResults([`摘要：已保存修订 v${summary.revision + 1}。`])
      try { await refresh() }
      catch (error) { setMessage(`摘要已保存，但刷新显示失败：${errorText(error)}；可刷新页面恢复。`) }
      if (noteTarget) {
        try { await submitNote(noteTarget, instruction.trim()) }
        catch (error) { setNoteFailed(true); setTargetResults(previous => [...previous, `笔记：提交失败，摘要已保存；可只重试笔记。${errorText(error)}`]) }
      }
      if (scope === 'remember') {
        try { await saveRememberedRule(committed.id) }
        catch (error) { setRuleSaveFailed(true); setTargetResults(previous => [...previous, `术语规则：保存失败，摘要已保存；可单独重试。${errorText(error)}`]) }
      }
    } catch (error) { setTargetResults([`摘要：保存失败。${errorText(error)}`]) }
    finally { setBusy(false); setKey('') }
  }

  const undo = async () => {
    if (!summary || !operation || busy) return
    setBusy(true); setMessage('')
    try {
      const undone = await api.undoSummaryOperation(taskId, operation.id, summary.revision)
      setOperation(undone)
      setTargetResults([`摘要：已创建撤销版本 v${summary.revision + 1}。`, '术语规则保持原状态；如需停用请单独操作。'])
      await refresh()
    } catch (error) { setMessage(errorText(error)) }
    finally { setBusy(false) }
  }

  const resolveBase = async (choice: 'keep_revision' | 'use_generated') => {
    if (!summary || busy) return
    setBusy(true); setMessage('')
    try { await api.resolveSummaryBase(taskId, summary.revision, choice); await refresh(); setMessage(choice === 'keep_revision' ? '已保留修订并确认新原稿。' : '已切换到新原稿，旧修订仍在历史版本中。') }
    catch (error) { setMessage(errorText(error)) }
    finally { setBusy(false) }
  }

  const download = () => {
    if (!summary) return
    const blob = new Blob([summary.content], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url; anchor.download = `video-${taskId}-summary-v${summary.revision}.md`
    document.body.appendChild(anchor); anchor.click(); anchor.remove()
    window.setTimeout(() => URL.revokeObjectURL(url), 10_000)
  }

  if (!summary) return <p role="status">{message || '正在读取摘要…'}</p>
  return <div style={{ display: 'grid', gap: 16 }}>
    <p className="muted">{summary.has_revision ? `你的修订 v${summary.revision}` : '共享生成原稿'} · 此视频的修订仅属于当前账号</p>
    {summary.source_status === 'needs_merge' && <div role="alert" className="card" style={{ padding: 12 }}>
      <p>生成原稿已更新。当前仍显示你的修订，请选择如何处理。</p>
      {!readOnly && <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}><button className="btn btn-sm" disabled={busy} onClick={() => void resolveBase('keep_revision')}>保留我的修订</button><button className="btn btn-sm" disabled={busy} onClick={() => void resolveBase('use_generated')}>改用新原稿</button></div>}
    </div>}
    {summary.source_status === 'generated_missing' && <p role="status">生成原稿暂不可用；你的修订仍保留。来源恢复后可核对差异。</p>}
    <div className="summary-body"><MarkdownAnswer content={summary.content} domainTags /></div>
    <button className="btn btn-sm" onClick={download}>下载当前摘要 Markdown</button>
    {!readOnly && <section style={{ display: 'grid', gap: 10 }} aria-label="AI 修改摘要">
      <label>让 AI 修改这份摘要<textarea value={instruction} onChange={event => { setInstruction(event.target.value); setKey(''); setOperation(null) }} maxLength={2000} rows={3} placeholder="例如：只把安装章节中的错误名称改正，保留原话引用" /></label>
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
        <label><input type="radio" checked={scope === 'once'} onChange={() => setScope('once')} /> 只改这次</label>
        <label><input type="radio" checked={scope === 'remember'} onChange={() => setScope('remember')} /> 此视频以后都使用这个名称</label>
      </div>
      {!!notes.length && <label>同时修改一份笔记（独立保存，逐项报告结果）<select value={noteTarget} onChange={event => setNoteTarget(event.target.value)}><option value="">不修改笔记</option>{notes.map(note => <option key={note.id} value={note.id}>{note.title} · v{note.head_version}</option>)}</select></label>}
      {scope === 'remember' && <div style={{ display: 'grid', gap: 8 }}>
        <p className="muted">规则仅适用于此视频的新运行；原始转写和画面记录保持原样。没有证据时会标记为用户指定。</p>
        <label>原写法<input value={from} onChange={event => setFrom(event.target.value)} maxLength={80} /></label>
        <label>使用名称<input value={to} onChange={event => setTo(event.target.value)} maxLength={80} /></label>
        <label>适用上下文<input value={context} onChange={event => setContext(event.target.value)} maxLength={240} placeholder="例如：安装工具的产品名；不用于代码示例" /></label>
        <label>排除条件（每行一项）<textarea value={exclusions} onChange={event => setExclusions(event.target.value)} rows={2} placeholder="原话引用\n命令参数" /></label>
      </div>}
      <button className="btn btn-sm" disabled={busy || operation?.status === 'running' || !instruction.trim() || (scope === 'remember' && (!from.trim() || !to.trim() || !context.trim()))} onClick={() => void preview()}>{busy ? '提交中…' : operation?.status === 'proposed' ? '重新预览' : '预览 AI 修改'}</button>
      {operation?.status === 'running' && <p role="status">AI 正在准备摘要差异。离开页面或刷新不会取消后台任务。</p>}
      {operation?.status === 'proposed' && <div className="card" style={{ padding: 12 }}><h4>逐处差异</h4>{operation.edits.map((edit, index) => <div key={index} style={{ borderTop: '1px solid var(--bd-1)', padding: '8px 0' }}><p>原文：{edit.old_text}</p><p>修改：{edit.new_text || '删除'}</p></div>)}<button className="btn btn-sm" disabled={busy} onClick={() => void apply()}>确认保存摘要{scope === 'remember' ? '并记住规则' : ''}</button></div>}
      {operation?.status === 'committed' && !operation.undo_revision_id && <button className="btn btn-sm" disabled={busy} onClick={() => void undo()}>撤销这次摘要修改</button>}
      {operation?.status === 'committed' && scope === 'remember' && ruleSaveFailed && <button className="btn btn-sm" disabled={busy || !from.trim() || !to.trim() || !context.trim()} onClick={() => { setBusy(true); void saveRememberedRule(operation.id).catch(error => setMessage(errorText(error))).finally(() => setBusy(false)) }}>只重试保存术语规则</button>}
      {operation?.status === 'committed' && submittedNoteTarget && noteFailed && <button className="btn btn-sm" disabled={busy} onClick={() => { setBusy(true); void submitNote(submittedNoteTarget, noteAttempt.current?.prompt ?? instruction.trim()).catch(error => setMessage(`笔记重试失败：${errorText(error)}`)).finally(() => setBusy(false)) }}>只重试笔记</button>}
    </section>}
    {!!rules?.rules.length && <section><h4>此视频的术语规则</h4>{rules.rules.map(rule => <div key={rule.id} style={{ padding: '8px 0', borderTop: '1px solid var(--bd-1)' }}><p>{rule.from} → {rule.to} · {rule.enabled ? rule.basis === 'evidence_supported' && rule.evidence_status === 'current' ? '视频证据支持' : rule.evidence_status === 'pending_review' ? '证据待核对' : '用户指定' : '已停用'}</p><p className="muted">适用：{rule.context}{rule.exclusions.length ? `；排除：${rule.exclusions.join('、')}` : ''}</p>{rule.enabled && !readOnly && <button className="btn btn-sm" disabled={busy} onClick={() => { setBusy(true); void api.disableTermRule(taskId, rule.id, rules.version).then(setRules).catch(error => setMessage(errorText(error))).finally(() => setBusy(false)) }}>停用规则</button>}</div>)}</section>}
    {targetResults.map((result, index) => <p key={index} role="status">{result}</p>)}
    {message && <p role="alert">{message}</p>}
  </div>
}
