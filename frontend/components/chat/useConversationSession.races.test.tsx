// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { useConversationSession } from './useConversationSession'
import type { SummaryContextRef } from '@/lib/summaryExperience'

const mocks = vi.hoisted(() => ({
  listSessions: vi.fn(), getMessages: vi.fn(), getRunHistory: vi.fn(), createSession: vi.fn(),
  streamAsk: vi.fn(),
}))
vi.mock('@/lib/api', () => ({
  api: { listSessions: mocks.listSessions, getMessages: mocks.getMessages, getRunHistory: mocks.getRunHistory, createSession: mocks.createSession },
  streamAsk: mocks.streamAsk,
  streamAgent: vi.fn(),
  ApiError: class ApiError extends Error {},
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}
const options = { scopeType: 'video' as const, targetId: 63, basePath: '/chat/v/63', mode: 'chat' as const, topK: 5 }
const historyMessages = [
  { id: 10, role: 'user', content: 'Old question', created_at: '2026-09-29T01:00:00Z' },
  { id: 11, role: 'assistant', content: 'Old answer', created_at: '2026-09-29T01:00:01Z' },
]
const sessions = [{ id: 1, task_id: 63, scope_type: 'video', title: 'Old session' }]

beforeEach(() => {
  vi.resetAllMocks()
  window.history.replaceState({}, '', '/chat/v/63')
  mocks.listSessions.mockResolvedValue(sessions)
  mocks.getMessages.mockResolvedValue(historyMessages)
  mocks.getRunHistory.mockResolvedValue([])
  mocks.createSession.mockResolvedValue({ id: 41, task_id: 63, scope_type: 'video' })
  mocks.streamAsk.mockImplementation((_sid, _question, _topK, _mode, _handlers, signal: AbortSignal) =>
    new Promise<void>((_resolve, reject) => signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })))
})
afterEach(() => cleanup())

test('switching sessions blocks send until history has loaded', async () => {
  const history = deferred<typeof historyMessages>()
  mocks.getMessages.mockReturnValue(history.promise)
  const hook = renderHook(() => useConversationSession(options))
  await waitFor(() => expect(hook.result.current.sessionReady).toBe(true))
  act(() => { void hook.result.current.switchSession(1) })
  await waitFor(() => expect(hook.result.current.session?.id).toBe(1))
  expect(hook.result.current.historyLoading).toBe(true)
  act(() => { void hook.result.current.send('New question') })
  expect(mocks.streamAsk).not.toHaveBeenCalled()
  await act(async () => { history.resolve(historyMessages); await history.promise })
  expect(hook.result.current.messages.map(message => message.content)).toEqual(['Old question', 'Old answer'])
  act(() => { void hook.result.current.send('New question') })
  await waitFor(() => expect(mocks.streamAsk).toHaveBeenCalledTimes(1))
  expect(hook.result.current.messages.map(message => message.content)).toContain('New question')
  act(() => hook.result.current.stop())
})

test('a session creation response cannot reopen a question after new session', async () => {
  const creation = deferred<{ id: number; task_id: number; scope_type: string }>()
  mocks.createSession.mockReturnValue(creation.promise)
  const hook = renderHook(() => useConversationSession(options))
  await waitFor(() => expect(hook.result.current.sessionReady).toBe(true))
  act(() => { void hook.result.current.send('Question left behind') })
  expect(hook.result.current.sending).toBe(true)
  act(() => hook.result.current.newSession())
  await act(async () => { creation.resolve({ id: 41, task_id: 63, scope_type: 'video' }); await creation.promise })
  expect(hook.result.current.session).toBeNull()
  expect(window.location.search).toBe('')
  expect(hook.result.current.messages).toEqual([])
  expect(mocks.streamAsk).not.toHaveBeenCalled()
})

test('two sends during session creation create and stream only once', async () => {
  const creation = deferred<{ id: number; task_id: number; scope_type: string }>()
  mocks.createSession.mockReturnValue(creation.promise)
  const hook = renderHook(() => useConversationSession(options))
  await waitFor(() => expect(hook.result.current.sessionReady).toBe(true))
  act(() => { void hook.result.current.send('Suggested question'); void hook.result.current.send('Suggested question') })
  expect(mocks.createSession).toHaveBeenCalledTimes(1)
  expect(hook.result.current.sending).toBe(true)
  await act(async () => { creation.resolve({ id: 41, task_id: 63, scope_type: 'video' }); await creation.promise })
  await waitFor(() => expect(mocks.streamAsk).toHaveBeenCalledTimes(1))
  expect(hook.result.current.messages.filter(message => message.role === 'user')).toHaveLength(1)
  act(() => hook.result.current.stop())
})

test('failed history read shows error, blocks send, and can retry', async () => {
  mocks.getMessages.mockRejectedValueOnce(new Error('temporary read failure')).mockResolvedValueOnce(historyMessages)
  window.history.replaceState({}, '', '/chat/v/63?session=1')
  const hook = renderHook(() => useConversationSession(options))
  await waitFor(() => expect(hook.result.current.sessionReady).toBe(true))
  expect(hook.result.current.historyError).toBeTruthy()
  act(() => { void hook.result.current.send('Question while history is unknown') })
  expect(mocks.streamAsk).not.toHaveBeenCalled()
  await act(async () => { await hook.result.current.retryHistory() })
  expect(hook.result.current.historyError).toBe('')
  expect(hook.result.current.messages.map(message => message.content)).toEqual(['Old question', 'Old answer'])
})

const selectedRef: SummaryContextRef = { kind:'summary_selection', task_id:63, version_ref:{generated_version:1}, document_digest:'body', block_id:'chapter', block_digest:'block', text_start:0, text_end:3, quote:'旧摘要' }

test('context selection is frozen across session creation and cleared only after saved done', async () => {
  const creation = deferred<{id:number;task_id:number;scope_type:string}>()
  mocks.createSession.mockReturnValue(creation.promise)
  const onContextSent = vi.fn()
  let handlers: {onDone:(done:{message_id:number})=>void} | undefined
  mocks.streamAsk.mockImplementation((_sid, _question, _topK, _mode, h) => { handlers = h; return Promise.resolve() })
  const hook = renderHook(({refs}) => useConversationSession({...options, basePath:'/video/63', contextRefs:refs, onContextSent}), {initialProps:{refs:[selectedRef]}})
  await waitFor(() => expect(hook.result.current.sessionReady).toBe(true))
  act(() => { void hook.result.current.send('解释这一段') })
  hook.rerender({refs:[{...selectedRef, quote:'刚改选段'}]})
  await act(async () => { creation.resolve({id:41,task_id:63,scope_type:'video'}); await creation.promise })
  await waitFor(() => expect(mocks.streamAsk).toHaveBeenCalled())
  expect(mocks.streamAsk.mock.calls[0][6]).toEqual([selectedRef])
  expect(window.location.pathname).toBe('/video/63')
  expect(onContextSent).not.toHaveBeenCalled()
  // A stale callback after the stream has closed must not clear newer refs.
  act(() => handlers?.onDone({message_id:99}))
  expect(onContextSent).not.toHaveBeenCalled()
})

test('selection remains after failed stream; successful persisted done acknowledges once', async () => {
  const onContextSent = vi.fn()
  mocks.streamAsk.mockImplementation(async (_sid,_question,_topK,_mode, handlers) => { handlers.onError({message:'选段版本已变化'}) })
  const hook = renderHook(() => useConversationSession({...options,contextRefs:[selectedRef],onContextSent}))
  await waitFor(() => expect(hook.result.current.sessionReady).toBe(true))
  await act(async () => { await hook.result.current.send('解释') })
  expect(onContextSent).not.toHaveBeenCalled()
  expect(hook.result.current.messages.at(-1)?.error).toBe('选段版本已变化')
  mocks.streamAsk.mockImplementation(async (_sid,_question,_topK,_mode, handlers) => { handlers.onDone({message_id:9,answer:'解释完成'}); handlers.onDone({message_id:9,answer:'解释完成'}) })
  await act(async () => { await hook.result.current.send('重试解释') })
  expect(onContextSent).toHaveBeenCalledTimes(1)
})

test('a new selection added during a running answer survives the previous done', async () => {
  const onContextSent = vi.fn()
  const stream = deferred<void>()
  let done!: (event:{message_id:number})=>void
  mocks.streamAsk.mockImplementation((_sid,_question,_topK,_mode,handlers) => { done=handlers.onDone; return stream.promise })
  const hook=renderHook(({refs})=>useConversationSession({...options,contextRefs:refs,onContextSent}),{initialProps:{refs:[selectedRef]}})
  await waitFor(() => expect(hook.result.current.sessionReady).toBe(true))
  act(() => { void hook.result.current.send('旧选段解释') })
  await waitFor(() => expect(mocks.streamAsk).toHaveBeenCalled())
  hook.rerender({refs:[{...selectedRef,quote:'新选段'}]})
  await act(async () => { done({message_id:100}); stream.resolve(); await stream.promise })
  expect(onContextSent).not.toHaveBeenCalled()
})
