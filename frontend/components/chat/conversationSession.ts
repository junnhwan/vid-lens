import type { ChatMsg } from './chatUtils.ts'
import {
  agentTraceReducer,
  emptyAgentTraceState,
  streamTraceReducer,
  type AgentSSEPayload,
  type AgentTraceState,
  type ChatTraceStep,
} from './traceTypes.ts'

export interface ConversationSessionState {
  messages: ChatMsg[]
  ragTrace: ChatTraceStep[]
  agentTrace: AgentTraceState
  streaming: boolean
}

export const emptyConversationSessionState = (): ConversationSessionState => ({
  messages: [],
  ragTrace: [],
  agentTrace: emptyAgentTraceState(),
  streaming: false,
})

type RAGEvent = Parameters<typeof streamTraceReducer>[1]
type RAGPayload = Parameters<typeof streamTraceReducer>[2]

export type ConversationSessionAction =
  | { type: 'load_messages'; messages: ChatMsg[] }
  | { type: 'append_messages'; messages: ChatMsg[] }
  | { type: 'reset' }
  | { type: 'rag_start'; question: string }
  | { type: 'agent_start'; question: string }
  | { type: 'answer_delta'; delta: string }
  | { type: 'patch_last'; patch: Partial<ChatMsg> }
  | { type: 'rag_event'; event: RAGEvent; payload?: RAGPayload }
  | { type: 'agent_event'; event: AgentSSEPayload }
  | { type: 'stream_done'; patch?: Partial<ChatMsg> }
  | { type: 'stream_error'; message: string }
  | { type: 'stream_cancelled' }
  | { type: 'toggle_citation'; messageIndex: number; citationId: string }

export function conversationSessionReducer(
  state: ConversationSessionState,
  action: ConversationSessionAction,
): ConversationSessionState {
  switch (action.type) {
    case 'load_messages':
      return { ...state, messages: action.messages }
    case 'append_messages':
      return { ...state, messages: [...state.messages, ...action.messages] }
    case 'reset':
      return emptyConversationSessionState()
    case 'rag_start': {
      const trace = streamTraceReducer([], 'start')
      return {
        ...state,
        streaming: true,
        ragTrace: trace,
        agentTrace: emptyAgentTraceState(),
        messages: [...state.messages,
          { role: 'user', content: action.question },
          { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true, trace },
        ],
      }
    }
    case 'agent_start':
      return {
        ...state,
        streaming: true,
        ragTrace: [],
        agentTrace: emptyAgentTraceState(),
        messages: [...state.messages,
          { role: 'user', content: action.question },
          { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true, trace: [], agentRun: true },
        ],
      }
    case 'answer_delta':
      return { ...state, messages: patchLastAssistant(state.messages, current => ({ ...current, content: current.content + action.delta, streaming: true })) }
    case 'patch_last':
      return { ...state, messages: patchLastAssistant(state.messages, current => ({ ...current, ...action.patch })) }
    case 'rag_event': {
      const trace = streamTraceReducer(state.ragTrace, action.event, action.payload)
      return { ...state, ragTrace: trace, messages: patchLastAssistant(state.messages, current => ({ ...current, trace })) }
    }
    case 'agent_event': {
      const trace = agentTraceReducer(state.agentTrace, action.event)
      return {
        ...state,
        agentTrace: trace,
        messages: patchLastAssistant(state.messages, current => ({ ...current, trace: trace.steps, agentRun: true })),
      }
    }
    case 'stream_done':
      return {
        ...state,
        streaming: false,
        messages: patchLastAssistant(state.messages, current => ({ ...current, streaming: false, ...(action.patch || {}) })),
      }
    case 'stream_error': {
      const ragTrace = state.ragTrace.length
        ? streamTraceReducer(state.ragTrace, 'error', { error: action.message }).map(step => (
            step.status === 'running'
              ? { ...step, status: 'error' as const, detail: action.message, error: action.message }
              : step
          ))
        : state.ragTrace
      return {
        ...state,
        streaming: false,
        ragTrace,
        messages: patchLastAssistant(state.messages, current => ({ ...current, streaming: false, error: action.message, ...(ragTrace.length ? { trace: ragTrace } : {}) })),
      }
    }
    case 'stream_cancelled':
      return { ...state, streaming: false, messages: patchLastAssistant(state.messages, current => ({ ...current, streaming: false })) }
    case 'toggle_citation': {
      const message = state.messages[action.messageIndex]
      if (!message) return state
      const open = message.openCiteIds || []
      const messages = [...state.messages]
      messages[action.messageIndex] = {
        ...message,
        openCiteIds: open.includes(action.citationId)
          ? open.filter(id => id !== action.citationId)
          : [...open, action.citationId],
      }
      return { ...state, messages }
    }
    default:
      return state
  }
}

function patchLastAssistant(
  messages: ChatMsg[],
  patch: (message: ChatMsg) => ChatMsg,
): ChatMsg[] {
  if (messages.length === 0) return messages
  const index = messages.length - 1
  const current = messages[index]
  if (!current || current.role !== 'assistant') return messages
  const next = [...messages]
  next[index] = patch(current)
  return next
}
