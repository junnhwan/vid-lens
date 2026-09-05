'use client'

import { useCallback, useEffect, useReducer, useRef, useState } from 'react'

import type { CiteRef } from '@/components/Citation'
import { parseMessages, type ChatMsg } from '@/components/chat/chatUtils'
import {
  conversationSessionReducer,
  emptyConversationSessionState,
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
    abortRef.current?.abort()
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
          const messages = await api.getMessages(selected.id)
          if (active) dispatch({ type: 'load_messages', messages: parseHistory(messages) })
        } catch { /* keep an empty, usable session */ }
      }
      if (active) setSessionReady(true)
    }
    void init()
    return () => { active = false }
  }, [loadSessions, parseHistory])

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
    setSession(selected)
    dispatch({ type: 'load_messages', messages: [] })
    replaceSessionInURL(sessionId)
    try {
      const messages = await api.getMessages(sessionId)
      dispatch({ type: 'load_messages', messages: parseHistory(messages) })
    } catch { /* keep selected session usable */ }
  }, [sessions, session?.id, replaceSessionInURL, parseHistory])

  const newSession = useCallback(() => {
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
    abortRef.current = controller
    const agentMode = mode === 'agent'
    dispatch({ type: agentMode ? 'agent_start' : 'rag_start', question })

    try {
      if (agentMode) {
        await streamAgent(sessionId, question, { top_k: topK, mode: 'agent' }, {
          onRunStart: data => dispatch({ type: 'agent_event', event: { type: 'run_start', data } }),
          onStepStart: data => dispatch({ type: 'agent_event', event: { type: 'step_start', data } }),
          onStepDone: data => dispatch({ type: 'agent_event', event: { type: 'step_done', data } }),
          onStepError: data => dispatch({ type: 'agent_event', event: { type: 'step_error', data } }),
          onToolCall: data => dispatch({ type: 'agent_event', event: { type: 'tool_call', data } }),
          onToolResult: data => dispatch({ type: 'agent_event', event: { type: 'tool_result', data } }),
          onRetrieveHits: data => dispatch({ type: 'agent_event', event: { type: 'retrieve_hits', data } }),
          onAnswer: delta => dispatch({ type: 'answer_delta', delta }),
          onCitations: citations => dispatch({ type: 'patch_last', patch: { cites: mapCitations(citations) } }),
          onDone: () => {
            dispatch({ type: 'agent_event', event: { type: 'done' } })
            dispatch({ type: 'stream_done' })
          },
          onError: error => {
            dispatch({ type: 'agent_event', event: { type: 'error', data: { message: error.message, step_id: error.step_id } } })
            dispatch({ type: 'stream_error', message: error.message })
          },
        }, controller.signal)
      } else {
        let answerStarted = false
        await streamAsk(sessionId, question, topK, mode, {
          onAnswer: delta => {
            dispatch({ type: 'answer_delta', delta })
            if (!answerStarted) {
              answerStarted = true
              dispatch({ type: 'rag_event', event: 'answer' })
            }
          },
          onCitations: citations => {
            dispatch({ type: 'patch_last', patch: { cites: mapCitations(citations) } })
            const sources = [...new Set(citations.map(item => item.video_title || item.source).filter(Boolean))] as string[]
            dispatch({ type: 'rag_event', event: 'citations', payload: { hits: citations.length, sources } })
          },
          onDone: done => {
            dispatch({ type: 'rag_event', event: 'done' })
            dispatch({ type: 'stream_done', patch: { ...(done.answer ? { content: done.answer } : {}), degraded: done.degraded } })
          },
          onError: error => dispatch({ type: 'stream_error', message: error.message }),
        }, controller.signal)
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') {
        dispatch({ type: 'stream_cancelled' })
      } else {
        dispatch({ type: 'stream_error', message: error instanceof ApiError ? error.message : agentMode ? 'Agent 流式请求失败' : '流式请求失败' })
      }
    } finally {
      if (abortRef.current === controller) abortRef.current = null
      dispatch({ type: 'stream_cancelled' })
    }
  }, [state.streaming, canSend, onBlocked, onBeforeSend, session?.id, createSession, mode, topK, mapCitations])

  const stop = useCallback(() => {
    abortRef.current?.abort()
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
