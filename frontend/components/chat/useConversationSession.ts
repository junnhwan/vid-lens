'use client'

import { useCallback, useEffect, useReducer, useRef, useState } from 'react'

import type { CiteRef } from '@/components/Citation'
import { parseMessages, type ChatMsg } from '@/components/chat/chatUtils'
import { mergeRunHistory } from './conversationHistory'
import { recoverConversationRun } from '@/lib/conversationRecovery'
import { budgetProgress } from '@/lib/budgetNotice'
import {
  conversationSessionReducer,
  emptyConversationSessionState,
  type ConversationSessionAction,
} from '@/components/chat/conversationSession'
import { api, ApiError, streamAgent, streamAsk } from '@/lib/api'
import type { ChatMessage, ChatScopeType, ChatSession, Citation, VideoChatMode } from '@/lib/types'

interface ConversationSessionOptions {
  scopeType: ChatScopeType
  targetId: number
  basePath: string
  mode: VideoChatMode
  topK: number
  parseHistory?: (messages: ChatMessage[]) => ChatMsg[]
  mapCitations?: (citations: Citation[]) => CiteRef[]
  canSend?: boolean
  onBlocked?: () => void
  onBeforeSend?: () => void
}

export function useConversationSession(options: ConversationSessionOptions) {
  const {
    scopeType, targetId, basePath, mode, topK,
    parseHistory = parseMessages,
    mapCitations = defaultCitationMapper,
    canSend = true,
    onBlocked,
    onBeforeSend,
  } = options
  const [session, setSession] = useState<ChatSession | null>(null)
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [sessionReady, setSessionReady] = useState(false)
  const [state, dispatch] = useReducer(conversationSessionReducer, undefined, emptyConversationSessionState)
  const abortRef = useRef<AbortController | null>(null)
  const loadVersion = useRef(0)
  const flushRef = useRef<(() => void) | null>(null)
  const loadHistory = useCallback(async (sid: number) => {
    const [messages, runs] = await Promise.all([api.getMessages(sid), api.getRunHistory(sid).catch(() => [])])
    return mergeRunHistory(parseHistory(messages), runs)
  }, [parseHistory])

  const sessionFilter = useCallback(() => (
    scopeType === 'knowledge_base'
      ? { knowledge_base_id: targetId }
      : { task_id: targetId }
  ), [scopeType, targetId])

  const loadSessions = useCallback(async () => {
    try {
      const list = await api.listSessions(sessionFilter())
      setSessions(list)
      return list
    } catch {
      return []
    }
  }, [sessionFilter])

  useEffect(() => {
    let active = true
    const version = ++loadVersion.current
    abortRef.current?.abort()
    abortRef.current = null
    setSession(null)
    setSessionReady(false)
    dispatch({ type: 'reset' })
    const init = async () => {
      const list = await loadSessions()
      if (!active) return
      const sidParam = new URLSearchParams(location.search).get('session')
      const sid = sidParam ? Number(sidParam) : 0
      const selected = sid > 0 ? list.find(item => item.id === sid) || null : null
      setSession(selected)
      if (selected) {
        try {
          const messages = await loadHistory(selected.id)
          if (active && version === loadVersion.current) dispatch({ type: 'load_messages', messages })
        } catch { /* keep an empty, usable session */ }
      }
      if (active) setSessionReady(true)
    }
    void init()
    return () => { active = false; abortRef.current?.abort(); abortRef.current = null }
  }, [loadSessions, loadHistory])

  const replaceSessionInURL = useCallback((sessionId?: number) => {
    const url = new URLSearchParams(location.search)
    if (sessionId) url.set('session', String(sessionId))
    else url.delete('session')
    const query = url.toString()
    history.replaceState(null, '', query ? `${basePath}?${query}` : basePath)
  }, [basePath])

  const switchSession = useCallback(async (sessionId: number) => {
    const selected = sessions.find(item => item.id === sessionId)
    if (!selected || selected.id === session?.id) return
    const version = ++loadVersion.current
    abortRef.current?.abort()
    abortRef.current = null
    dispatch({ type: 'reset' })
    setSession(selected)
    dispatch({ type: 'load_messages', messages: [] })
    replaceSessionInURL(sessionId)
    try {
      const messages = await loadHistory(sessionId)
      if (version === loadVersion.current) dispatch({ type: 'load_messages', messages })
    } catch { /* keep selected session usable */ }
  }, [sessions, session?.id, replaceSessionInURL, loadHistory])

  const newSession = useCallback(() => {
    ++loadVersion.current
    abortRef.current?.abort()
    abortRef.current = null
    setSession(null)
    dispatch({ type: 'reset' })
    replaceSessionInURL()
  }, [replaceSessionInURL])

  const createSession = useCallback(async () => {
    const created = await api.createSession(scopeType === 'knowledge_base'
      ? { knowledge_base_id: targetId, scope_type: 'knowledge_base' }
      : { task_id: targetId, scope_type: 'video' })
    setSession(created)
    replaceSessionInURL(created.id)
    void loadSessions()
    return created.id
  }, [scopeType, targetId, replaceSessionInURL, loadSessions])

  const send = useCallback(async (question: string) => {
    if (state.streaming) return
    if (!canSend) {
      onBlocked?.()
      return
    }
    onBeforeSend?.()
    let sessionId = session?.id
    if (!sessionId) {
      try {
        sessionId = await createSession()
      } catch (error) {
        dispatch({
          type: 'append_messages',
          messages: [
            { role: 'user', content: question },
            { role: 'assistant', content: '', error: error instanceof ApiError ? error.message : '创建会话失败' },
          ],
        })
        return
      }
    }

    const controller = new AbortController()
    let runId: string | undefined
    let streamFailed = false
    abortRef.current = controller
    const deliver = (action: ConversationSessionAction) => {
      if (abortRef.current === controller && !controller.signal.aborted) dispatch(action)
    }
    let pendingAnswer = ''
    const pendingReasoning = new Map<string, import('@/lib/conversationStream').ReasoningEvent>()
    let batchTimer: ReturnType<typeof setTimeout> | undefined
    const flush = () => {
      clearTimeout(batchTimer)
      batchTimer = undefined
      for (const event of pendingReasoning.values()) deliver({ type: 'reasoning', event })
      pendingReasoning.clear()
      if (pendingAnswer) deliver({ type: 'answer_delta', delta: pendingAnswer })
      pendingAnswer = ''
    }
    const update = (action: ConversationSessionAction) => {
      if (action.type === 'answer_delta') pendingAnswer += action.delta
      else if (action.type === 'reasoning') {
        const old = pendingReasoning.get(action.event.call_id)
        pendingReasoning.set(action.event.call_id, { ...action.event, delta: (old?.delta ?? '') + action.event.delta })
      } else { flush(); deliver(action); return }
      batchTimer ??= setTimeout(flush, 32)
    }
    flushRef.current = flush
    const processHandlers = {
      onProgress: (event: import('@/lib/conversationStream').ProgressEvent) => update({ type: 'progress', event }),
      onReasoning: (event: import('@/lib/conversationStream').ReasoningEvent) => update({ type: 'reasoning', event }),
      onAnswerReset: () => update({ type: 'patch_last', patch: { content: '' } }),
    }
    dispatch(
      mode === 'chat'
        ? { type: 'rag_start', question }
        : { type: 'agent_start', question, mode },
    )

    try {
      if (mode === 'agent') {
        await streamAgent(sessionId, question, { top_k: topK, mode: 'agent' }, {
          ...processHandlers,
          onRunStart: data => { runId = data.run_id; update({ type: 'agent_event', event: { type: 'run_start', data } }) },
          onStepStart: data => update({ type: 'agent_event', event: { type: 'step_start', data } }),
          onStepDone: data => update({ type: 'agent_event', event: { type: 'step_done', data } }),
          onStepError: data => update({ type: 'agent_event', event: { type: 'step_error', data } }),
          onToolCall: data => update({ type: 'agent_event', event: { type: 'tool_call', data } }),
          onToolResult: data => update({ type: 'agent_event', event: { type: 'tool_result', data } }),
          onRetrieveHits: data => update({ type: 'agent_event', event: { type: 'retrieve_hits', data } }),
          onAnswer: delta => update({ type: 'answer_delta', delta }),
          onCitations: citations => update({ type: 'patch_last', patch: { cites: mapCitations(citations) } }),
          onDone: done => {
            const budget = budgetProgress(done.stop_reason, done.budget_notice)
            if (budget) update({ type: 'progress', event: budget })
            update({ type: 'agent_event', event: { type: 'done' } })
            // done is authoritative; budget-limited runs may deliver evidence
            // excerpts instead of a model-generated answer.
            update({
              type: 'stream_done',
              patch: { messageId: done.message_id, ...(done.answer !== undefined ? { content: done.answer } : {}), degraded: done.degraded, ...(done.run_id ? { agentRunId: done.run_id } : {}) },
            })
          },
          onError: error => {
            streamFailed = true
            update({ type: 'agent_event', event: { type: 'error', data: { message: error.message, step_id: error.step_id } } })
            update({ type: 'stream_error', message: error.message })
          },
        }, controller.signal)
      } else {
        await streamAsk(sessionId, question, topK, mode, {
          ...processHandlers,
          onAnswer: delta => {
            update({ type: 'answer_delta', delta })
          },
          onCitations: citations => {
            update({ type: 'patch_last', patch: { cites: mapCitations(citations) } })
          },
          onDone: done => {
            update({ type: 'stream_done', patch: { messageId: done.message_id, ...(done.answer !== undefined ? { content: done.answer } : {}), degraded: done.degraded } })
          },
          onError: error => update({ type: 'stream_error', message: error.message }),
        }, controller.signal)
      }
    } catch (error) {
      streamFailed = true
      if (error instanceof DOMException && error.name === 'AbortError') {
        update({ type: 'stream_cancelled' })
      } else {
        const fallback = mode === 'agent' ? 'Agent 流式请求失败' : '流式请求失败'
        update({ type: 'stream_error', message: error instanceof ApiError ? error.message : fallback })
      }
    } finally {
      flush()
      if (streamFailed && runId && !controller.signal.aborted && abortRef.current === controller) {
        const recovered = await recoverConversationRun(runId, {
          detail: () => api.getRunDetail(sessionId, runId!),
          messages: async () => parseHistory(await api.getMessages(sessionId)),
          runId: message => message.agentRunId,
        }, controller.signal)
        if (recovered.message) update({ type: 'stream_done', patch: recovered.message })
        else update({ type: 'stream_error', message: recovered.notice || '运行状态待确认' })
      }
      if (abortRef.current === controller) {
        dispatch({ type: 'stream_cancelled' })
        abortRef.current = null
        flushRef.current = null
      }
    }
  }, [state.streaming, canSend, onBlocked, onBeforeSend, session?.id, createSession, mode, topK, mapCitations, parseHistory])

  const stop = useCallback(() => {
    flushRef.current?.()
    flushRef.current = null
    abortRef.current?.abort()
    abortRef.current = null
    dispatch({ type: 'stream_cancelled' })
  }, [])

  const toggleCite = useCallback((messageIndex: number, citationId: string) => {
    dispatch({ type: 'toggle_citation', messageIndex, citationId })
  }, [])

  return {
    session,
    sessions,
    sessionReady,
    messages: state.messages,
    ragTrace: state.ragTrace,
    agentTrace: state.agentTrace,
    streaming: state.streaming,
    loadSessions,
    switchSession,
    newSession,
    send,
    stop,
    toggleCite,
  }
}

function defaultCitationMapper(citations: Citation[]): CiteRef[] {
  return citations.map((citation, index) => ({
    id: `C${index + 1}`,
    taskId: citation.task_id,
    chunkIndex: citation.chunk_index,
    score: citation.score,
    content: citation.content,
    anchorQuote: citation.anchor_quote || citation.content,
    displayContext: citation.display_context || citation.content,
    modality: citation.modality,
    startMS: citation.start_ms,
    endMS: citation.end_ms,
    timeRangeStatus: citation.time_range_status,
    contextStartMS: citation.context_start_ms,
    contextEndMS: citation.context_end_ms,
    displayContextTruncated: citation.display_context_truncated,
    sourceRefs: citation.source_refs,
    source: citation.source,
    videoTitle: citation.video_title,
    finalRank: citation.final_rank,
  }))
}
