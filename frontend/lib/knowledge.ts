import type { Citation } from './types'

export interface RetrievalTestResult {
  task_ids: number[]
  mode: string
  citations: Citation[]
  trace: { duration_ms: number; original_query?: string; rewritten_queries?: string[]; fallbacks?: string[]; stages?: { name: string; query?: string; citations: Citation[] }[] }
}

export interface RunDetail {
  id: string; status: string; stop_reason: string; duration_ms: number
  tools_used: number; tools_limit: number; model_calls: number; retrieval_calls: number
  prompt_tokens: number; completion_tokens: number; token_source: string
  steps: { id: string; label: string; status: string; duration_ms?: number }[]
}

export interface SessionMemoryPolicy {
  session_id: number; policy: 'inherit' | 'enabled' | 'disabled'; version: number
  effective_memory_policy: { effective_enabled: boolean; capability_enabled: boolean; reason: string }
}

export function replayLink(taskId: number, startMS?: number, timeStatus?: string): string {
  return `/video/${taskId}${(timeStatus === 'exact' || timeStatus === 'coarse') && Number.isFinite(startMS) && (startMS ?? -1) >= 0 ? `?t=${Math.floor(startMS!)}` : ''}`
}
