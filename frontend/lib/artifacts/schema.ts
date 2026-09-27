import { z } from 'zod'

// Wire authority: docs/architecture/artifact-api-contract.md (v1).
const id = z.string().min(1)
const integer = z.number().int().nonnegative()
export const blockSchema = z.object({
  block_id: id, parent_id: id.nullable(), type: z.enum(['section', 'concept', 'example', 'note']),
  title: z.string().min(1).max(200), content: z.string().max(8000),
  claim_origin: z.enum(['source', 'synthesis', 'user']),
  evidence_refs: z.array(z.object({ evidence_id: id, relation: z.enum(['supports', 'context', 'contradicts']) })),
})
export const bodySchema = z.object({
  schema_version: z.literal(1), kind: z.literal('study'), title: z.string().min(1).max(200),
  blocks: z.array(blockSchema).min(1).max(200), warnings: z.array(z.string()),
}).superRefine((body, ctx) => {
  const depths = new Map<string, number>()
  for (const [i, block] of body.blocks.entries()) {
    const depth = block.parent_id === null ? 1 : (depths.get(block.parent_id) ?? 99) + 1
    if (depths.has(block.block_id) || depth > 8) ctx.addIssue({ code: 'custom', path: ['blocks', i], message: '块 ID 重复、父节点顺序无效或层级超过 8 层' })
    if (block.claim_origin === 'source' && !block.evidence_refs.length) ctx.addIssue({ code: 'custom', path: ['blocks', i], message: '来源结论缺少引用' })
    depths.set(block.block_id, depth)
  }
  if (new TextEncoder().encode(JSON.stringify(body)).length > 512 * 1024) ctx.addIssue({ code: 'custom', message: '正文超过 512 KiB' })
})
const artifactMetadataSchema = z.object({
  id, kind: z.literal('study'), title: z.string(), head_version: integer,
  current_version_id: id.nullable(), created_at: z.string(), updated_at: z.string(),
})
export const versionSchema = z.object({
  id, artifact_id: id, version: integer, base_version: integer,
  origin: z.enum(['generated', 'user']), run_id: z.string().nullable(), manifest_id: id,
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
  result: z.object({ artifact_id: id, version_id: id, quality: z.string(), is_candidate: z.boolean() }).nullable(),
  error_code: z.string().nullable(), created_at: z.string(), started_at: z.string().nullable(), finished_at: z.string().nullable(), last_seq: integer,
  usage: z.object({ llm_calls: integer, prompt_tokens: integer, completion_tokens: integer, token_source: z.enum(['unknown', 'estimated', 'actual', 'mixed']) }),
})
export const artifactSchema = artifactMetadataSchema.extend({ latest_run: runSchema.nullable() })
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
export type Evidence = z.infer<typeof evidenceSchema>
export type GenerationRun = z.infer<typeof runSchema>
export type ProductTask = z.infer<typeof taskSchema>
export interface GenerationInput { kind: 'study'; scope: 'video'; source_ids: number[]; goal: string; artifact_id?: string; base_version?: number }
