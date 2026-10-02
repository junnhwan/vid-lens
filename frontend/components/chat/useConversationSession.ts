import { useCallback, useEffect, useReducer, useRef, useState } from 'react'

import type { CiteRef } from '@/components/Citation'
import { citeFromAPI } from '@/components/Citation'
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
  const [historyLoading, setHistoryLoading] = useState(false)
  const [historyError, setHistoryError] = useState('')
  const [sending, setSending] = useState(false)
  const [state, dispatch] = useReducer(conversationSessionReducer, undefined, emptyConversationSessionState)
  const abortRef = useRef<AbortController | null>(null)
  const loadVersion = useRef(0)
  const requestVersion = useRef(0)
  const historyLoadingRef = useRef(false)
  const sendingRef = useRef(false)
  const flushRef = useRef<(() => void) | null>(null)
  const loadHistory = useCallback(async (sid: number) => {
    const [messages, runs] = await Promise.all([api.getMessages(sid), api.getRunHistory(sid).catch(() => [])])
    return mergeRunHistory(parseHistory(messages), runs)
  }, [parseHistory])

  const sessionFilter = useCallback(() => (
    scopeType === 'knowledge_base'
      ? { knowledge_base_id: targetId }
      : scopeType === 'video_library' ? { scope_type: 'video_library' as ChatScopeType }
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
    ++requestVersion.current
    abortRef.current?.abort()
    abortRef.current = null
    setSession(null)
    setSessionReady(false)
    setHistoryError('')
    setHistoryLoading(false)
    historyLoadingRef.current = false
    sendingRef.current = false
    setSending(false)
    dispatch({ type: 'reset' })
    const init = async () => {
      const list = await loadSessions()
      if (!active) return
      const sidParam = new URLSearchParams(location.search).get('session')
      const sid = sidParam ? Number(sidParam) : 0
      const selected = sid > 0 ? list.find(item => item.id === sid) || null : null
      setSession(selected)
      if (selected) {
        historyLoadingRef.current = true
        setHistoryLoading(true)
        try {
          const messages = await loadHistory(selected.id)
          if (active && version === loadVersion.current) dispatch({ type: 'load_messages', messages })
        } catch {
          if (active && version === loadVersion.current) setHistoryError('历史消息读取失败，请重试')
        } finally {
          if (active && version === loadVersion.current) { historyLoadingRef.current = false; setHistoryLoading(false) }
        }
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
    ++requestVersion.current
    abortRef.current?.abort()
    abortRef.current = null
    sendingRef.current = false
    setSending(false)
    historyLoadingRef.current = true
    setHistoryLoading(true)
    setHistoryError('')
    dispatch({ type: 'reset' })
    setSession(selected)
    dispatch({ type: 'load_messages', messages: [] })
    replaceSessionInURL(sessionId)
    try {
      const messages = await loadHistory(sessionId)
      if (version === loadVersion.current) dispatch({ type: 'load_messages', messages })
    } catch {
      if (version === loadVersion.current) setHistoryError('历史消息读取失败，请重试')
    } finally {
      if (version === loadVersion.current) { historyLoadingRef.current = false; setHistoryLoading(false) }
    }
  }, [sessions, session?.id, replaceSessionInURL, loadHistory])

  const retryHistory = useCallback(async () => {
    if (!session || historyLoadingRef.current) return
    const version = ++loadVersion.current
    historyLoadingRef.current = true
    setHistoryLoading(true)
    setHistoryError('')
    try {
      const messages = await loadHistory(session.id)
      if (version === loadVersion.current) dispatch({ type: 'load_messages', messages })
    } catch {
      if (version === loadVersion.current) setHistoryError('历史消息读取失败，请重试')
    } finally {
      if (version === loadVersion.current) { historyLoadingRef.current = false; setHistoryLoading(false) }
    }
  }, [session, loadHistory])

  const newSession = useCallback(() => {
    ++loadVersion.current
    ++requestVersion.current
    abortRef.current?.abort()
    abortRef.current = null
    sendingRef.current = false
    setSending(false)
    historyLoadingRef.current = false
    setHistoryLoading(false)
    setHistoryError('')
    setSession(null)
    dispatch({ type: 'reset' })
    replaceSessionInURL()
  }, [replaceSessionInURL])

  const createSession = useCallback(async (version: number) => {
    const created = await api.createSession(scopeType === 'knowledge_base'
      ? { knowledge_base_id: targetId, scope_type: 'knowledge_base' }
      : scopeType === 'video_library' ? { scope_type: 'video_library' }
      : { task_id: targetId, scope_type: 'video' })
    if (version !== requestVersion.current) return null
    setSession(created)
    replaceSessionInURL(created.id)
    void loadSessions()
    return created.id
  }, [scopeType, targetId, replaceSessionInURL, loadSessions])

  const send = useCallback(async (question: string) => {
    if (!sessionReady || historyLoadingRef.current || historyError || sendingRef.current || state.streaming) return
    if (!canSend) {
      onBlocked?.()
      return
    }
    onBeforeSend?.()
    const version = requestVersion.current
    sendingRef.current = true
    setSending(true)
    let sessionId = session?.id
    if (!sessionId) {
      try {
        sessionId = await createSession(version) ?? undefined
        if (!sessionId) return
      } catch (error) {
        if (version !== requestVersion.current) return
        dispatch({
          type: 'append_messages',
          messages: [
            { role: 'user', content: question },
            { role: 'assistant', content: '', error: error instanceof ApiError ? error.message : '创建会话失败' },
          ],
        })
        sendingRef.current = false
        setSending(false)
        return
      }
    }

    if (version !== requestVersion.current) return

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
              patch: { messageId: done.message_id, ...(done.answer !== undefined ? { content: done.answer } : {}), degraded: done.degraded, modelName: done.model, profileId: done.profile_id, ...(done.execution_duration_ms !== undefined ? { executionDurationMs: done.execution_duration_ms } : {}), ...(done.run_id ? { agentRunId: done.run_id } : {}) },
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
            update({ type: 'stream_done', patch: { messageId: done.message_id, ...(done.answer !== undefined ? { content: done.answer } : {}), degraded: done.degraded, degradationReason: done.degradation_reason, diagnosticId: done.diagnostic_id, modelName: done.model, profileId: done.profile_id, ...(done.execution_duration_ms !== undefined ? { executionDurationMs: done.execution_duration_ms } : {}) } })
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
      if (version === requestVersion.current) { sendingRef.current = false; setSending(false) }
    }
  }, [sessionReady, historyError, state.streaming, canSend, onBlocked, onBeforeSend, session?.id, createSession, mode, topK, mapCitations, parseHistory])

  const stop = useCallback(() => {
    ++requestVersion.current
    sendingRef.current = false
    setSending(false)
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
    historyLoading,
    historyError,
    retryHistory,
    sending,
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
  return citations.map(citeFromAPI)
}
