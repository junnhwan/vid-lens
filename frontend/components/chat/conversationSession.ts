import type { ChatMsg } from './chatUtils.ts'
import {
  agentTraceReducer,
  emptyAgentTraceState,
  streamTraceReducer,
  progressTrace,
  finishTrace,
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
  | { type: 'agent_start'; question: string; mode?: 'agent' }
  | { type: 'answer_delta'; delta: string }
  | { type: 'progress'; event: import('../../lib/conversationStream.ts').ProgressEvent }
  | { type: 'reasoning'; event: import('../../lib/conversationStream.ts').ReasoningEvent }
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
      const trace: ChatTraceStep[] = []
      return {
        ...state,
        streaming: true,
        ragTrace: trace,
        agentTrace: emptyAgentTraceState(),
        messages: [...state.messages,
          { role: 'user', content: action.question },
          { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true, trace: [], processStartedAt: Date.now() },
        ],
      }
    }
    case 'agent_start': {
      const mode = action.mode ?? 'agent'
      return {
        ...state,
        streaming: true,
        ragTrace: [],
        agentTrace: { ...emptyAgentTraceState(), mode },
        messages: [...state.messages,
          { role: 'user', content: action.question },
          { role: 'assistant', content: '', cites: [], openCiteIds: [], streaming: true, trace: [], agentRun: true, agentMode: mode, processStartedAt: Date.now() },
        ],
      }
    }
    case 'answer_delta':
      return { ...state, messages: patchLastAssistant(state.messages, current => ({ ...current, content: current.content + action.delta, streaming: true })) }
    case 'reasoning':
      return { ...state, messages: patchLastAssistant(state.messages, current => ({ ...current, reasoning: { ...current.reasoning, [action.event.call_id]: ((current.reasoning?.[action.event.call_id] ?? '') + action.event.delta).slice(0, 64000) } })) }
    case 'progress': {
      const agent = !!state.messages.at(-1)?.agentRun
      const steps = progressTrace(agent ? state.agentTrace.steps : (state.messages.at(-1)?.trace ?? []), action.event)
      return { ...state, ...(agent ? { agentTrace: { ...state.agentTrace, steps } } : { ragTrace: steps }), messages: patchLastAssistant(state.messages, current => ({ ...current, trace: steps, traceSource: agent ? 'agent' : 'server' })) }
    }
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
        messages: patchLastAssistant(state.messages, current => ({ ...current, streaming: false, processFinishedAt: Date.now(), trace: finishTrace(current.trace ?? [], 'done'), ...(action.patch || {}) })),
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
        agentTrace: { ...state.agentTrace, finished: true, steps: finishTrace(state.agentTrace.steps, 'error') },
        messages: patchLastAssistant(state.messages, current => ({ ...current, streaming: false, processFinishedAt: Date.now(), error: action.message, trace: finishTrace(current.trace ?? [], 'error') })),
      }
    }
    case 'stream_cancelled':
      if (!state.streaming) return state
      return { ...state, streaming: false, ragTrace: finishTrace(state.ragTrace, 'cancelled'), agentTrace: { ...state.agentTrace, finished: true, steps: finishTrace(state.agentTrace.steps, 'cancelled') }, messages: patchLastAssistant(state.messages, current => ({ ...current, streaming: false, cancelled: true, processFinishedAt: Date.now(), trace: finishTrace(current.trace ?? [], 'cancelled') })) }
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
