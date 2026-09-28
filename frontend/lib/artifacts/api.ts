import { z } from 'zod'
import { ApiError, req } from '@/lib/api'
import { artifactPageSchema, detailSchema, evidenceSchema, runSchema, sourceSchema, taskPageSchema, versionSchema, versionSummarySchema, positionSchema, blockContextSchema, answerPreviewSchema, editRunSchema, editOperationSchema, type StudyBody, type GenerationInput, type ArtifactEditMode } from './schema'
import { errorLabels } from './view'

export class ContractError extends Error { constructor() { super('收到的成果数据格式不受支持，请刷新或稍后重试。') } }
async function read<T>(schema: z.ZodType<T>, response: Promise<unknown>): Promise<T> {
  const parsed = schema.safeParse(await response)
  if (!parsed.success) throw new ContractError()
  return parsed.data
}
const pathId = encodeURIComponent
export const artifactApi = {
  position: () => read(positionSchema.nullable(), req('/learning-position', 'GET')),
  savePosition: (input: { expected_revision: number; task_id: number; artifact_id: string; version_id: string; block_id: string; time_ms: number }) => read(positionSchema, req('/learning-position', 'PATCH', input)),
  blockContext: (id: string, versionId: string, blockId: string) => read(blockContextSchema, req(`/artifacts/${pathId(id)}/blocks/${pathId(blockId)}/context?version_id=${pathId(versionId)}`, 'GET')),
  answerPreview: (id: string, messageId: number, blockId: string, head: number) => read(answerPreviewSchema, req(`/artifacts/${pathId(id)}/answer-preview`, 'POST', { message_id: messageId, after_block_id: blockId, expected_head_version: head })),
  importAnswer: (id: string, messageId: number, blockId: string, head: number, key: string, personal: boolean) => read(detailSchema, req(`/artifacts/${pathId(id)}/answer-import`, 'POST', { message_id: messageId, after_block_id: blockId, expected_head_version: head, personal_without_sources: personal }, { 'Idempotency-Key': key })),
  list: (page = 1, sourceId?: number, signal?: AbortSignal) => read(artifactPageSchema, req(`/artifacts?page=${page}&page_size=20${sourceId ? `&source_id=${sourceId}` : ''}`, 'GET', undefined, undefined, signal)),
  get: (id: string, signal?: AbortSignal) => read(detailSchema, req(`/artifacts/${pathId(id)}`, 'GET', undefined, undefined, signal)),
  versions: (id: string, signal?: AbortSignal) => read(z.object({ list: z.array(versionSummarySchema) }), req(`/artifacts/${pathId(id)}/versions`, 'GET', undefined, undefined, signal)),
  version: (id: string, versionId: string, signal?: AbortSignal) => read(versionSchema, req(`/artifacts/${pathId(id)}/versions/${pathId(versionId)}`, 'GET', undefined, undefined, signal)),
  save: (id: string, head: number, body: StudyBody) => read(detailSchema, req(`/artifacts/${pathId(id)}`, 'PATCH', { expected_head_version: head, body })),
  adopt: (id: string, head: number, versionId: string) => read(detailSchema, req(`/artifacts/${pathId(id)}/adopt`, 'POST', { expected_head_version: head, version_id: versionId })),
  source: (id: number, signal?: AbortSignal) => read(sourceSchema, req(`/sources/video/${id}`, 'GET', undefined, undefined, signal)),
  evidence: (manifestId: string, evidenceId: string, signal?: AbortSignal) => read(evidenceSchema, req(`/sources/${pathId(manifestId)}/evidence/${pathId(evidenceId)}`, 'GET', undefined, undefined, signal)),
  generate: (input: GenerationInput, key: string) => read(runSchema, req('/artifact-runs', 'POST', input, { 'Idempotency-Key': key })),
  run: (id: string, signal?: AbortSignal) => read(runSchema, req(`/artifact-runs/${pathId(id)}`, 'GET', undefined, undefined, signal)),
  cancel: (id: string) => read(runSchema, req(`/artifact-runs/${pathId(id)}/cancel`, 'POST', {})),
  retry: (id: string, key: string) => read(runSchema, req(`/artifact-runs/${pathId(id)}/retry`, 'POST', {}, { 'Idempotency-Key': key })),
  resume: (id: string) => read(runSchema, req(`/artifact-runs/${pathId(id)}/resume`, 'POST', {})),
  submitEdit: (id: string, input: { instruction: string; expected_head_version: number; selected_block_ids: string[]; mode: ArtifactEditMode }, key: string) => read(editRunSchema, req(`/artifacts/${pathId(id)}/edit-runs`, 'POST', input, { 'Idempotency-Key': key })),
  editRun: (id: string, signal?: AbortSignal) => read(editRunSchema, req(`/artifact-edit-runs/${pathId(id)}`, 'GET', undefined, undefined, signal)),
  cancelEdit: (id: string) => read(editRunSchema, req(`/artifact-edit-runs/${pathId(id)}/cancel`, 'POST', {})),
  editOperation: (id: string, signal?: AbortSignal) => read(editOperationSchema, req(`/artifact-edit-operations/${pathId(id)}`, 'GET', undefined, undefined, signal)),
  applyEdit: (id: string, head: number, key: string) => read(editOperationSchema, req(`/artifact-edit-operations/${pathId(id)}/apply`, 'POST', { expected_head_version: head }, { 'Idempotency-Key': key })),
  undoEdit: (id: string, head: number, key: string) => read(editOperationSchema, req(`/artifact-edit-operations/${pathId(id)}/undo`, 'POST', { expected_head_version: head }, { 'Idempotency-Key': key })),
  tasks: (page = 1, signal?: AbortSignal) => read(taskPageSchema, req(`/tasks?page=${page}&page_size=20`, 'GET', undefined, undefined, signal)),
}
export function artifactError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code && errorLabels[error.code]) return errorLabels[error.code]
    if (error.status === 404) return '内容不存在，或你没有访问权限。'
    if (error.status === 403) return '当前账号没有执行此操作的权限。'
    if (error.status === 410) return errorLabels.source_deleted
    if (error.status === 429) return '请求过于频繁或额度不足，请稍后重试。'
    return error.message
  }
  if (error instanceof ContractError) return error.message
  return '暂时无法连接服务，请检查网络后重试。'
}
