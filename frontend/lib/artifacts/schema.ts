import { z } from 'zod'

// Wire authority: docs/architecture/artifact-api-contract.md and artifact-canvas-contract.md.
const id = z.string().min(1)
const integer = z.number().int().nonnegative()
export const blockSchema = z.object({
  block_id: id, parent_id: id.nullable(), type: z.enum(['section', 'concept', 'example', 'note']),
  title: z.string().min(1).max(200), content: z.string().max(8000),
  claim_origin: z.enum(['source', 'synthesis', 'user']),
  evidence_refs: z.array(z.object({ evidence_id: id, relation: z.enum(['supports', 'context', 'contradicts']), chat_citation_id: z.string().max(32).optional() })),
  source_block_ids: z.array(z.string().min(1).max(100)).max(200).optional(),
})
export const relationSchema = z.object({
  id, source_block_id: id, target_block_id: id,
  type: z.enum(['related_to', 'depends_on', 'contrasts_with']),
  origin: z.enum(['user', 'synthesis']),
  evidence_refs: z.array(z.object({ evidence_id: id, relation: z.enum(['supports', 'context', 'contradicts']), chat_citation_id: z.string().max(32).optional() })).max(100),
})
export const bodySchema = z.object({
  schema_version: z.union([z.literal(1), z.literal(2)]), kind: z.literal('study'), title: z.string().min(1).max(200),
  blocks: z.array(blockSchema).min(1).max(200), relations: z.array(relationSchema).max(300).optional(), warnings: z.array(z.string()),
}).superRefine((body, ctx) => {
  const depths = new Map<string, number>()
  for (const [i, block] of body.blocks.entries()) {
    const depth = block.parent_id === null ? 1 : (depths.get(block.parent_id) ?? 99) + 1
    if (depths.has(block.block_id) || depth > 8) ctx.addIssue({ code: 'custom', path: ['blocks', i], message: '块 ID 重复、父节点顺序无效或层级超过 8 层' })
    if (block.claim_origin === 'source' && !block.evidence_refs.length) ctx.addIssue({ code: 'custom', path: ['blocks', i], message: '来源结论缺少引用' })
    depths.set(block.block_id, depth)
  }
  if (body.schema_version === 1 && body.relations?.length) ctx.addIssue({ code: 'custom', path: ['relations'], message: '旧版本不支持关系' })
  const relationIds = new Set<string>(), relationKeys = new Set<string>(), dependencies = new Map<string, string[]>()
  for (const [i, rel] of (body.relations ?? []).entries()) {
    if (relationIds.has(rel.id) || !depths.has(rel.source_block_id) || !depths.has(rel.target_block_id) || rel.source_block_id === rel.target_block_id || rel.origin === 'synthesis' && !rel.evidence_refs.length) ctx.addIssue({ code: 'custom', path: ['relations', i], message: '关系端点、来源或 ID 无效' })
    relationIds.add(rel.id)
    const [source, target] = rel.type === 'depends_on' ? [rel.source_block_id, rel.target_block_id] : [rel.source_block_id, rel.target_block_id].sort()
    const key = `${rel.type}:${source}:${target}`
    if (relationKeys.has(key)) ctx.addIssue({ code: 'custom', path: ['relations', i], message: '关系重复' })
    relationKeys.add(key)
    if (rel.type === 'depends_on') dependencies.set(source, [...(dependencies.get(source) ?? []), target])
  }
  const visited = new Set<string>(), active = new Set<string>()
  const visit = (node: string): boolean => { if (active.has(node)) return false; if (visited.has(node)) return true; active.add(node); for (const target of dependencies.get(node) ?? []) if (!visit(target)) return false; active.delete(node); visited.add(node); return true }
  if ([...dependencies.keys()].some(node => !visit(node))) ctx.addIssue({ code: 'custom', path: ['relations'], message: '依赖关系不能形成环' })
  if (new TextEncoder().encode(JSON.stringify(body)).length > 512 * 1024) ctx.addIssue({ code: 'custom', message: '正文超过 512 KiB' })
})
const artifactMetadataSchema = z.object({
  id, kind: z.literal('study'), title: z.string(), head_version: integer,
  current_version_id: id.nullable(), created_at: z.string(), updated_at: z.string(),
})
export const versionSchema = z.object({
  id, artifact_id: id, version: integer, base_version: integer,
  origin: z.enum(['generated', 'user', 'agent', 'undo']), run_id: z.string().nullable(), edit_operation_id: z.string().nullable().optional().default(null), manifest_id: id,
  body: bodySchema, quality: z.string(), created_at: z.string(),
  source_status: z.enum(['current', 'outdated']),
  was_candidate: z.boolean(), adopted_from_version_id: id.nullable(),
})
export const versionSummarySchema = versionSchema.omit({ body: true, source_status: true })
export const evidenceSchema = z.object({
  id, manifest_id: id, source_id: z.number().int().positive(), source_title: z.string(),
  source_identity: z.string(), modality: z.string(), content: z.string(), content_hash: z.string(),
  start_ms: integer.nullable(), end_ms: integer.nullable(), time_range_status: z.enum(['precise', 'coarse', 'unknown']),
})
export const sourceSchema = z.object({ manifest_id: id, source_id: z.number().int().positive(), title: z.string(), evidence: z.array(evidenceSchema) })
export const runSchema = z.object({
  id, artifact_id: id, source_task_id: z.number().int().positive(), parent_run_id: z.string().nullable(), status: z.enum(['pending', 'running', 'completed', 'failed', 'cancelled', 'budget_exhausted']),
  stage: z.string(), cancel_requested: z.boolean(), can_cancel: z.boolean(), can_retry: z.boolean(), can_resume: z.boolean(),
  progress: z.object({ stage: z.string(), covered_segments: integer, total_segments: integer }).optional(),
  budget: z.object({ stop_reason: z.string(), max_llm_calls: integer, max_input_tokens: integer, max_output_tokens: integer }).optional(),
  result: z.object({ artifact_id: id, version_id: id, quality: z.string(), is_candidate: z.boolean() }).nullable(),
  error_code: z.string().nullable(), created_at: z.string(), started_at: z.string().nullable(), finished_at: z.string().nullable(), last_seq: integer,
  usage: z.object({ llm_calls: integer, prompt_tokens: integer, completion_tokens: integer, token_source: z.enum(['unknown', 'estimated', 'actual', 'mixed']) }),
})
const usageSchema = z.object({ llm_calls: integer, prompt_tokens: integer, completion_tokens: integer, token_source: z.enum(['unknown', 'estimated', 'actual', 'mixed']) })
export const editResultSchema = z.discriminatedUnion('kind', [
  z.object({ kind: z.literal('committed'), operation_id: id, result_version_id: id }),
  z.object({ kind: z.literal('proposal'), operation_id: id }),
  z.object({ kind: z.literal('answer'), message: z.string(), evidence_ids: z.array(id).optional().default([]) }),
  z.object({ kind: z.literal('no_change'), message: z.string(), evidence_ids: z.array(id).optional().default([]) }),
])
export const editRunSchema = z.object({
  id, artifact_id: id, instruction: z.string(), expected_head_version: z.number().int().positive(), base_version_id: id,
  selected_block_ids: z.array(id), mode: z.enum(['answer', 'apply', 'preview']),
  status: z.enum(['pending', 'running', 'completed', 'failed', 'cancelled', 'budget_exhausted']), stage: z.string(),
  cancel_requested: z.boolean(), can_cancel: z.boolean(), result: editResultSchema.nullable(), error_code: z.string().nullable(),
  created_at: z.string(), started_at: z.string().nullable(), finished_at: z.string().nullable(), last_seq: integer, usage: usageSchema,
})
const patchCountsSchema = z.object({ added: integer, updated: integer, deleted: integer, moved: integer })
const patchChangeSchema = z.object({
  kind: z.enum(['title_updated', 'added', 'updated', 'deleted', 'moved', 'relation_added', 'relation_removed', 'relation_updated']), block_id: z.string().optional(), relation_id: z.string().optional(),
  before: blockSchema.nullable().optional(), after: blockSchema.nullable().optional(),
  before_relation: relationSchema.nullable().optional(), after_relation: relationSchema.nullable().optional(),
  before_title: z.string().nullable().optional(), after_title: z.string().nullable().optional(),
  before_index: integer.optional(), after_index: integer.optional(),
})
const blockMappingSchema = z.object({ kind: z.string(), from_block_ids: z.array(id), to_block_ids: z.array(id) })
export const editOperationSchema = z.object({
  id, artifact_id: id, status: z.enum(['proposed', 'committed']), base_version: z.number().int().positive(), base_version_id: id,
  result_version_id: id.nullable(), undo_version_id: id.nullable(), basis: z.enum(['user_instruction', 'evidence_supported', 'evidence_conflict']),
  evidence_ids: z.array(id), summary: z.string(), counts: patchCountsSchema, changes: z.array(patchChangeSchema), block_mappings: z.array(blockMappingSchema),
  can_apply: z.boolean(), can_undo: z.boolean(), created_at: z.string(), updated_at: z.string(), committed_at: z.string().nullable(),
})
export const artifactSchema = artifactMetadataSchema.extend({ latest_run: runSchema.nullable(), latest_edit_run: editRunSchema.nullable().optional().default(null) })
export const detailSchema = artifactSchema.extend({ version: versionSchema.nullable() })
export const taskSchema = z.object({
  id, type: z.enum(['artifact_generation', 'video_processing']), resource_id: id, title: z.string(),
  status: z.string(), stage: z.string(), can_cancel: z.boolean(), can_retry: z.boolean(), can_resume: z.boolean(),
  created_at: z.string(), updated_at: z.string(), run: runSchema.nullable(),
})
export const artifactPageSchema = z.object({ list: z.array(artifactSchema), total: integer, page: integer, page_size: integer })
export const taskPageSchema = z.object({ list: z.array(taskSchema), total: integer, page: integer, page_size: integer })
export type Artifact = z.infer<typeof artifactSchema>
export type ArtifactDetail = z.infer<typeof detailSchema>
export type ArtifactVersion = z.infer<typeof versionSchema>
export type VersionSummary = z.infer<typeof versionSummarySchema>
export type StudyBody = z.infer<typeof bodySchema>
export type StudyBlock = z.infer<typeof blockSchema>
export type StudyRelation = z.infer<typeof relationSchema>
export type Evidence = z.infer<typeof evidenceSchema>
export type GenerationRun = z.infer<typeof runSchema>
export type ArtifactEditRun = z.infer<typeof editRunSchema>
export type ArtifactEditOperation = z.infer<typeof editOperationSchema>
export type ArtifactEditMode = ArtifactEditRun['mode']
export type ProductTask = z.infer<typeof taskSchema>
export const positionSchema = z.object({ revision: integer, task_id: z.number().int().positive(), artifact_id: z.string(), version_id: z.string(), block_id: z.string(), time_ms: integer, updated_at: z.string(), fallback: z.string().optional() })
export type LearningPosition = z.infer<typeof positionSchema>
export const blockContextSchema = z.object({ artifact_id: id, version_id: id, task_id: z.number().int().positive(), block: blockSchema })
export const answerPreviewSchema = z.object({ message_id: z.number().int().positive(), content: z.string(), after_block_id: id, mapped: z.array(z.object({ evidence_id: id, relation: z.enum(['supports', 'context', 'contradicts']), chat_citation_id: z.string().optional() })), unmapped: z.array(z.string()), version_id: id })
export type AnswerPreview = z.infer<typeof answerPreviewSchema>
export interface GenerationInput { kind: 'study'; scope: 'video'; source_ids: number[]; goal: string; artifact_id?: string; base_version?: number }
export const canvasNodeSchema = z.object({ position: z.object({ x: z.number().finite().min(-100000).max(100000), y: z.number().finite().min(-100000).max(100000) }), width: z.number().finite().min(120).max(600), height: z.number().finite().min(60).max(500), pinned: z.boolean(), hidden: z.boolean(), collapsed: z.boolean(), style: z.enum(['auto', 'section', 'concept', 'operation', 'command', 'note']) })
export const canvasLayoutSchema = z.object({ direction: z.enum(['RIGHT', 'DOWN']), algorithm: z.literal('elk-layered-v1'), nodes: z.record(z.string(), canvasNodeSchema), viewport: z.object({ x: z.number().finite().min(-100000).max(100000), y: z.number().finite().min(-100000).max(100000), zoom: z.number().finite().min(.1).max(3) }) })
export const canvasLayoutViewSchema = z.object({ content_version_id: id, view_id: z.literal('knowledge'), revision: integer, layout: canvasLayoutSchema })
export type CanvasLayout = z.infer<typeof canvasLayoutSchema>
export type CanvasLayoutView = z.infer<typeof canvasLayoutViewSchema>
export const canvasAIPlanSchema = z.object({ direction: z.enum(['RIGHT', 'DOWN']), scope: z.enum(['all', 'selected']), density: z.enum(['comfortable', 'compact']), summary: z.string().max(200) })
