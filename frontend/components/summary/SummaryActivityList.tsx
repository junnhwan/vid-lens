import type { SummaryGeneration } from '@/lib/summaryExperience'

const fallbackReasons: Record<string, string> = { auto_summary_disabled: '本次仅刷新文字，未请求生成新摘要。', source_unchanged: '文字来源未变化，沿用已保存摘要。', classification_not_reprocessed: '标签没有重新分类，保留当前选择。', no_useful_visual: '画面未提供额外帮助，采用文字摘要。', visual_enricher_unavailable: '画面处理暂不可用，已保留文字摘要。', invalid_summary_document: '输出未通过来源与结构校验，请检查执行记录后重试。', vision_unavailable: '当前未配置画面理解，已保留文字摘要。', visual_disabled: '画面理解已关闭。', visual_source_refs_missing: '摘要缺少可用于定位画面的来源关联，暂未补图。',
  visual_location_missing: '未找到可核对的画面位置，暂未补图。', visual_not_beneficial: '画面没有提供额外帮助，采用文字摘要。', visual_budget_exhausted: '本次画面检查预算已用尽，保留已完成内容。', requested_visual_mode_unavailable: '无法完成所选画面形式，已保留文字摘要。', visual_enrichment_failed: '补图未完成，文字摘要仍可阅读。' }
export function SummaryActivityList({ generation, error }: { generation: SummaryGeneration | null; error: string }) {
  if (!generation) return error ? <p role="status" className="muted">执行记录暂不可用；已保存摘要仍可阅读。</p> : null
  const rows = generation.activities || [], running = rows.filter(row => row.state === 'running'), recent = rows.slice(-3)
  const reason = generation.fallback_reason || generation.stop_reason
  const activity = (row: typeof rows[number]) => <li key={`${row.id}-${row.attempt}`} className={`summary-activity ${row.state}`}><span aria-hidden="true">{row.state === 'running' ? '◉' : row.state === 'error' || row.state === 'failed' ? '!' : row.state === 'cancelled' ? '−' : '✓'}</span><span>{row.title}{row.detail && <small>{row.detail}</small>}</span>{row.duration_ms != null && <time>{Math.max(0, row.duration_ms / 1000).toFixed(1)}s</time>}</li>
  return <section className="summary-activities" aria-label="摘要真实执行记录" aria-busy={running.length > 0}>
    {rows.length > 0 && <><ol aria-live="polite">{recent.map(activity)}</ol>{rows.length > 3 && <details><summary>展开运行记录 · {rows.length} 条</summary><ol>{rows.slice(0, -3).map(activity)}</ol></details>}</>}
    {generation.operation==='source_refresh'&&['queued','running','pending','retry_waiting'].includes(generation.status)&&<p role="status">正在刷新文字来源，已有摘要仍可阅读。</p>}
    {generation.operation==='source_refresh'&&generation.status==='completed'&&generation.source_status==='source_changed'&&<p role="status">文字来源已更新；已保留原摘要，可为新来源生成摘要。</p>}
    {generation.classification_state==='not_reprocessed'&&<p className="muted">标签未重新分类，保留当前选择；可手动调整。</p>}
    {reason && <p role="status">{fallbackReasons[reason] || (generation.result_state==='ready'?'本次处理已停止，已保存摘要仍可阅读。':'本次生成未完成，可检查运行记录后重试。')}</p>}
    {generation.source && <p className="summary-source muted">{generation.source.kind === 'subtitle' ? '平台字幕' : '音频转写'}{generation.source.language ? ` · ${generation.source.language}` : ''}{generation.visual_state === 'running' ? ' · 文字已可读，正在核对画面' : ''}</p>}
  </section>
}
