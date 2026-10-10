import { ApiError, getToken, req } from './api'

export type SummaryVersionRef = { generated_version: number } | { revision_id: string }
export interface SummarySourceRef { source_id: string; cue_ids: string[]; start_ms: number | null; end_ms: number | null; timing_method: string }
export interface SummaryFigure { id: string; screenshot_ref: string; capture_ms: number | null; caption: string; alt: string; supports: string; canonical_caption?: string }
export interface SummaryBlock { id: string; parent_id: string | null; order: number; title: string; body_markdown: string; source_refs: SummarySourceRef[]; figures: SummaryFigure[] }
export interface SummaryDocument { schema_version: 'summary-v2'; document_id: string; source_id: string; source_digest: string; media_revision: string; presentation_mode: 'text' | 'image_text' | 'keyframes'; title: string; overview: string; blocks: SummaryBlock[] }
export interface SummaryContextRef { kind: 'summary_selection' | 'summary_screenshot'; task_id: number; version_ref: SummaryVersionRef; document_digest: string; block_id: string; block_digest: string; text_start: number; text_end: number; quote: string; screenshot_ref?: string }
export interface SummaryBlockContext { canonical_text: string; block_digest: string; document_digest: string; version_ref: SummaryVersionRef; source_refs: SummarySourceRef[]; figures: SummaryFigure[]; title: string; source_title: string }
export interface SummaryActivity { id: string; kind: string; state: string; title: string; detail?: string; attempt: number; started_at?: string; finished_at?: string; duration_ms?: number }
export interface SummaryGeneration { task_id: number; generation_id: string; result_generation_id?: string; operation?: string; classification_state?: 'retained' | 'not_reprocessed'; parent_generation_id?: string; visual_retry_available?: boolean; generated_content_digest?: string; generated_source_id?: string; generated_source_digest?: string; legacy: boolean; mindmap_enabled?: boolean; requested_mode: string; resolved_mode: string; text_state: string; visual_state: string; result_state: string; stage: string; status: string; stop_reason?: string; fallback_reason?: string; source_status?: string; source?: { id: string; kind: string; digest: string; language: string; quality: string }; generated_version: number; content_digest: string; content_hash_kind: string; activities: SummaryActivity[]; event_high_watermark: number }
export interface TextSourceRefreshRequest { expected_source_id: string; text_source_policy: 'prefer_platform'|'force_asr'; profile_id?: number; auto_summary: boolean }
export interface TextSourceRefreshReceipt { task_id: number; generation_id: string; operation: 'source_refresh'; accepted: true }
export interface SummaryVisualRetryRequest { expected_generation_id: string; expected_generated_version: number; expected_content_digest: string; expected_source_id: string; expected_source_digest: string; authorize_new_visual_budget: true }
export interface SummaryVisualRetryReceipt { task_id: number; generation_id: string; parent_generation_id: string; operation: 'visual_retry'; accepted: true }
export interface SummaryGenerationEvent { seq: number; type: string; data: Record<string, unknown>; created_at: string }
export interface SummaryGenerationEvents { generation_id: string; events: SummaryGenerationEvent[]; high_watermark: number; next_after_seq: number; has_more: boolean; cursor_gap: boolean }

// Equal text after undo is still a different revision. Returning to a quoted
// block requires the exact frozen version as well as its document digest.
export function summaryReferenceMatches(ref: SummaryContextRef, taskID: number, summary: { content_digest?: string; version_ref?: SummaryVersionRef }): boolean {
  const current = summary.version_ref
  if (ref.task_id !== taskID || !current || !summary.content_digest || ref.document_digest !== summary.content_digest) return false
  return 'revision_id' in ref.version_ref
    ? 'revision_id' in current && ref.version_ref.revision_id === current.revision_id
    : 'generated_version' in current && ref.version_ref.generated_version === current.generated_version
}

export const summaryExperienceApi = {
  generation: (id: number, signal?: AbortSignal) => req<SummaryGeneration>(`/media/task/${id}/summary/generation`, 'GET', undefined, undefined, signal),
  refreshSource: (id: number, body: TextSourceRefreshRequest, key: string, signal?: AbortSignal) => req<TextSourceRefreshReceipt>(`/media/task/${id}/text-source/refresh`, 'POST', body, { 'Idempotency-Key': key }, signal),
  visualRetry: (id: number, body: SummaryVisualRetryRequest, key: string, signal?: AbortSignal) => req<SummaryVisualRetryReceipt>(`/media/task/${id}/summary/visual-retry`, 'POST', body, { 'Idempotency-Key': key }, signal),
  events: (id: number, generation: string, after: number, signal?: AbortSignal) => req<SummaryGenerationEvents>(`/media/task/${id}/summary/generation/${encodeURIComponent(generation)}/events?after_seq=${after}`, 'GET', undefined, undefined, signal),
  block: (id: number, block: string, version: SummaryVersionRef, signal?: AbortSignal) => {
    const ref = 'revision_id' in version ? `revision:${version.revision_id}` : `generated:${version.generated_version}`
    return req<SummaryBlockContext>(`/media/task/${id}/summary/blocks/${encodeURIComponent(block)}/context?version_ref=${encodeURIComponent(ref)}`, 'GET', undefined, undefined, signal)
  },
  screenshot: async (id: number, ref: string, signal?: AbortSignal): Promise<Blob> => {
    const token = getToken()
    const response = await fetch(`/api/v1/media/task/${id}/summary/screenshots/${encodeURIComponent(ref)}`, { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal })
    if (!response.ok) throw new ApiError(response.status, '摘要图片暂不可用，仍可查看图注与回放时间')
    return response.blob()
  },
}

// Layout is derived from the current document. It never owns a second content
// version, and uses stable IDs even when a chapter is renamed.
export function orderedSummaryBlocks(doc: SummaryDocument): SummaryBlock[] {
  const children = new Map<string, SummaryBlock[]>()
  for (const block of doc.blocks) { const key = block.parent_id || ''; children.set(key, [...children.get(key) || [], block]) }
  for (const rows of children.values()) rows.sort((a, b) => a.order - b.order || a.id.localeCompare(b.id))
  const result: SummaryBlock[] = [], seen = new Set<string>()
  const visit = (parent: string) => { for (const block of children.get(parent) || []) { if (seen.has(block.id)) continue; seen.add(block.id); result.push(block); visit(block.id) } }
  visit('')
  return result
}

export function uniqueUnicodeQuoteRange(canonical: string, quote: string): { text_start: number; text_end: number } | null {
  if (!quote || Array.from(quote).length > 3000) return null
  const start = canonical.indexOf(quote)
  if (start < 0 || canonical.indexOf(quote, start + 1) >= 0) return null
  return { text_start: Array.from(canonical.slice(0, start)).length, text_end: Array.from(canonical.slice(0, start)).length + Array.from(quote).length }
}
