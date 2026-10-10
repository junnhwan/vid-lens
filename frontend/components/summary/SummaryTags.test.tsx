// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { SummaryTags } from './SummaryTags'

const mocks = vi.hoisted(() => ({task:vi.fn(),patch:vi.fn(),decide:vi.fn()}))
vi.mock('@/lib/summaryTags', () => ({summaryTagsApi:mocks,tagError:(error:Error)=>error.message}))
vi.mock('@/components/tags/TagPicker', () => ({TagPicker:()=>null}))
vi.mock('@/components/tags/TagWordbook', () => ({TagWordbook:()=>null}))
const initial = {task_id:1,version:4,assignments:[{tag_id:'tag-1',origin:'auto',tag:{display_name:'Go'}}],suggestions:[{id:'suggestion',display_name:'稍后复习',reason:'个人意图待确认',status:'pending'}]}
beforeEach(() => { vi.resetAllMocks(); mocks.task.mockResolvedValue(initial) })
afterEach(cleanup)

test('manual retain and suggestion rejection use the latest authoritative tag version', async () => {
  mocks.patch.mockResolvedValue({...initial,version:5,assignments:[{...initial.assignments[0],origin:'manual'}]})
  mocks.decide.mockResolvedValue({...initial,version:6,suggestions:[{...initial.suggestions[0],status:'rejected'}]})
  render(<SummaryTags taskId={1} />)
  fireEvent.click(await screen.findByRole('button',{name:'保留'}))
  await waitFor(() => expect(mocks.patch).toHaveBeenCalledWith(1,{expected_version:4,keep_auto_ids:['tag-1']}))
  await waitFor(() => expect(screen.queryByRole('button',{name:'保留'})).toBeNull())
  fireEvent.click(screen.getByRole('button',{name:'拒绝'}))
  await waitFor(() => expect(mocks.decide).toHaveBeenCalledWith(1,'suggestion','reject',5))
  expect(await screen.findByRole('button',{name:'恢复建议'})).toBeTruthy()
})

test('a mutation response from the previous video cannot overwrite the new tag state', async () => {
  let resolve!: (state:typeof initial)=>void
  mocks.patch.mockReturnValue(new Promise(done => { resolve=done }))
  const view=render(<SummaryTags taskId={1} />)
  fireEvent.click(await screen.findByRole('button',{name:'保留'}))
  mocks.task.mockResolvedValue({task_id:2,version:0,assignments:[],suggestions:[]})
  view.rerender(<SummaryTags taskId={2} />)
  await screen.findByText('尚未添加标签')
  await act(async () => { resolve({...initial,version:5}); await Promise.resolve() })
  expect(screen.queryByText(/Go/)).toBeNull()
  expect(screen.getByText('尚未添加标签')).toBeTruthy()
})

test('post-publication pending tags are read again until the real classification completes', async () => {
  mocks.task.mockResolvedValueOnce({...initial,assignments:[],suggestions:[],classification:{status:'pending',enabled:true,generated_version:1}}).mockResolvedValue({...initial,classification:{status:'completed',enabled:true,generated_version:1}})
  render(<SummaryTags taskId={1} generationVersion={1} />)
  expect(await screen.findByText('正文已就绪，正在处理标签…')).toBeTruthy()
  expect(await screen.findByText('Go · 自动',{}, {timeout:3000})).toBeTruthy()
  expect(screen.queryByText('正文已就绪，正在处理标签…')).toBeNull()
  expect(mocks.task).toHaveBeenCalledTimes(2)
})
