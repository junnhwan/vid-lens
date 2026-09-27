import type { StudyBody, StudyBlock, Evidence, GenerationRun, Artifact } from './schema.ts'

export const runLabels: Record<GenerationRun['status'], string> = { pending: '排队中', running: '生成中', completed: '已完成 · 待核对', failed: '生成失败', cancelled: '已取消', budget_exhausted: '预算已用尽' }
export function artifactCardStatus(artifact: Artifact): string {
  const latest = artifact.latest_run
  if (!artifact.current_version_id) return latest ? runLabels[latest.status] : '尚无已保存版本'
  const base = `v${artifact.head_version} · 待核对`
  return latest && ['failed', 'cancelled', 'budget_exhausted'].includes(latest.status) ? `${base} · 最近一次：${runLabels[latest.status]}` : base
}
export const stageLabels: Record<string, string> = { queued: '等待后台处理', collecting: '整理视频来源', generating: '生成学习笔记', validating: '核对结构与引用', completed: '学习笔记已保存', failed: '生成未完成', cancelled: '已停止生成', budget_exhausted: '执行预算不足' }
export const errorLabels: Record<string, string> = {
  invalid_request: '提交内容不符合要求，请核对后重试。', invalid_evidence: '引用未通过验证，请重新读取来源。', unsupported_recipe: '当前仅支持单视频学习笔记。', internal_error: '服务暂时不可用，请稍后重试。',
  run_not_terminal: '这个任务还在进行，请刷新状态。', run_terminal: '这个任务已经结束，请刷新查看结果。', unsupported_checkpoint: '任务保存的进度暂时无法恢复，请重新生成。',
  source_not_ready: '视频仍在处理或尚无可用转写，请等待视频处理完成后重试。', source_limit_exceeded: '来源超过首版处理上限，请选择更短的视频。', profile_required: '请先在设置中配置默认 AI 模型。',
  source_changed: '视频来源已更新，请重新读取后再生成。', source_deleted: '来源已删除，相关正文和证据无法继续读取。',
  profile_changed: '执行所用的模型配置已变化，请检查设置后重试。', provider_error: '模型服务暂时不可用。', provider_truncated: '模型输出被截断，未发布不完整内容。', provider_refused: '模型未能完成这次生成。',
  invalid_model_output: '生成内容未通过结构或引用校验。', budget_exhausted: '本次任务已达到执行预算。', queue_expired: '排队等待超时，可以重新发起。',
  version_conflict: '已有新版本，你的编辑仍保留在这里。', idempotency_conflict: '请求内容已变化，请核对任务列表后重新创建。',
  position_conflict: '另一标签页已更新学习位置，请刷新后继续。', block_removed: '目标段落已删除，请刷新笔记重新选择。',
  answer_incomplete: '这条回答尚未完整保存，不能收进笔记。', answer_scope_mismatch: '回答与目标笔记不属于同一视频。',
  citations_unmapped: '部分聊天引用无法对应目标笔记快照。请核对并选择无来源个人补充，或取消。', answer_too_long: '回答超过单块长度上限，暂时无法直接收录。',
}
export function isActiveRun(run: GenerationRun) { return run.status === 'pending' || run.status === 'running' }
export function canReplay(evidence: Evidence) { return evidence.time_range_status !== 'unknown' && evidence.start_ms !== null && evidence.end_ms !== null && evidence.end_ms >= evidence.start_ms }
export function isPointEvidence(evidence: Evidence) { return canReplay(evidence) && (evidence.start_ms === evidence.end_ms || (evidence.modality.startsWith('visual') && evidence.end_ms! - evidence.start_ms! <= 1)) }
export function evidenceTime(evidence: Evidence) {
  if (!canReplay(evidence)) return '时间未知'
  const clock = (ms: number) => `${String(Math.floor(ms / 60000)).padStart(2, '0')}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}`
  if (isPointEvidence(evidence)) return `${evidence.time_range_status === 'coarse' ? '约 ' : ''}${clock(evidence.start_ms!)} · 画面时间点`
  return `${evidence.time_range_status === 'coarse' ? '约 ' : ''}${clock(evidence.start_ms!)} – ${clock(evidence.end_ms!)}`
}
export function warningMessage(code: string) {
  if (code === 'human_edited_unverified') return '人工修改后的引用关系尚待核对。'
  if (code === 'generated_needs_review') return 'AI 整理的内容需要结合原视频核对。'
  if (code === 'coverage_is_observations_not_all_video_frames') return '画面证据来自抽样观察，并未覆盖每一帧。'
  const coverage = /^covered_segments:(\d+)\/(\d+)$/.exec(code)
  if (coverage) return `已处理 ${coverage[1]}/${coverage[2]} 个视频片段。`
  return /^[a-z][a-z0-9_:./-]*$/.test(code) ? '生成提示：请结合来源核对这一处内容。' : code
}
export interface BlockNode { block: StudyBlock; children: BlockNode[] }
export function blockTree(body: StudyBody): BlockNode[] {
  const nodes = new Map<string, BlockNode>()
  const roots: BlockNode[] = []
  for (const block of body.blocks) {
    const node = { block, children: [] as BlockNode[] }
    nodes.set(block.block_id, node)
    const parent = block.parent_id ? nodes.get(block.parent_id) : null
    if (parent) parent.children.push(node)
    else roots.push(node)
  }
  return roots
}
// markmap-view interprets content as HTML. Only app-authored markup surrounds escaped text.
export function escapeMapText(text: string): string { return text.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]!)) }
interface MapNode { content: string; children: MapNode[] }
export function mapTree(body: StudyBody) {
  const node = ({ block, children }: BlockNode): MapNode => ({
    content: `<button type="button" data-block-id="${escapeMapText(block.block_id)}">${escapeMapText(block.title)}</button>`, children: children.map(node),
  })
  return { content: escapeMapText(body.title), children: blockTree(body).map(node) }
}
