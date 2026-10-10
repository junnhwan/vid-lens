import { ApiError, req } from './api'
import type { PaginatedTasks } from './types'

export interface UserTag { id: string; display_name: string; aliases: string[]; version: number; creation_origin: string; protected_by_user: boolean; video_count: number }
export interface TagSuggestion { id: string; display_name: string; reason: string; status: 'pending' | 'accepted' | 'rejected' | 'stale' }
export interface TaskTagState { classification?:{status:'pending'|'completed'|'failed'|'cancelled';enabled:boolean;error_code?:string;generated_version:number}; task_id: number; version: number; assignments: { tag_id: string; origin: 'manual' | 'auto'; tag: UserTag }[]; suggestions: TagSuggestion[] }
export interface TagPage { list: UserTag[]; total: number; page: number; page_size: number }
export interface TagPatch { expected_version: number; add_ids?: string[]; remove_ids?: string[]; keep_auto_ids?: string[] }
export type TagMatch = 'all' | 'any'
const tagPath = (id: string) => `/tags/${encodeURIComponent(id)}`
export const summaryTagsApi = {
  list: (page = 1, search = '', signal?: AbortSignal, sort: 'name' | 'usage' = 'name') => req<TagPage>(`/tags?${new URLSearchParams({ page: String(page), page_size: '50', search, sort })}`, 'GET', undefined, undefined, signal),
  create: (name: string) => req<UserTag>('/tags', 'POST', { name }),
  rename: (tag: UserTag, name: string) => req<UserTag>(tagPath(tag.id), 'PATCH', { name, expected_version: tag.version }),
  aliases: (tag: UserTag, aliases: string[]) => req<UserTag>(`${tagPath(tag.id)}/aliases`, 'PUT', { aliases, expected_version: tag.version }),
  merge: (source: UserTag, target: UserTag, key: string) => req<UserTag>(`${tagPath(source.id)}/merge`, 'POST', { target_id: target.id, source_version: source.version, target_version: target.version }, { 'Idempotency-Key': key }),
  task: (taskID: number, signal?: AbortSignal) => req<TaskTagState>(`/media/task/${taskID}/tags`, 'GET', undefined, undefined, signal),
  patch: (taskID: number, input: TagPatch) => req<TaskTagState>(`/media/task/${taskID}/tags`, 'PATCH', input),
  decide: (taskID: number, suggestionID: string, decision: 'accept' | 'reject' | 'restore', version: number) => req<TaskTagState>(`/media/task/${taskID}/tag-suggestions/${encodeURIComponent(suggestionID)}/decision`, 'POST', { decision, expected_version: version }),
  listTasks: (page: number, size: number, keyword: string, activity: string, ids: string[], match: TagMatch, signal?: AbortSignal) => req<PaginatedTasks>(`/media/list?${new URLSearchParams({ page: String(page), page_size: String(size), keyword, activity, tag_ids: ids.join(','), tag_match: match })}`, 'GET', undefined, undefined, signal),
}
export function tagError(error: unknown): string {
  if (error instanceof ApiError && error.code === 'tag_name_conflict') return '该名称或别名已被另一个标签使用。'
  if (error instanceof ApiError && error.status === 409) return '标签已发生变化，请刷新后重试。'
  return error instanceof Error ? error.message : '标签操作失败'
}
