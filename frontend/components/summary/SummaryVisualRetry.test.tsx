// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { SummaryVisualRetry } from './SummaryVisualRetry'
import type { SummaryGeneration } from '@/lib/summaryExperience'

afterEach(() => { cleanup(); vi.unstubAllGlobals() })
beforeEach(() => { vi.stubGlobal('localStorage', { getItem: () => null }) })
const generation = { generation_id:'attempt', result_generation_id:'published', generated_version:4, content_digest:'user-head', generated_content_digest:'original-digest', generated_source_id:'source', generated_source_digest:'source-digest', visual_retry_available:true } as SummaryGeneration
const response = () => ({ ok:true,status:202,json:async()=>({code:202,data:{task_id:7,generation_id:'new-attempt',parent_generation_id:'published',operation:'visual_retry',accepted:true}}) })

test('explicit visual retry freezes generated base and reuses one key after an uncertain network response',async()=>{
 const fetcher=vi.fn().mockRejectedValueOnce(new TypeError('network')).mockResolvedValueOnce(response())
 vi.stubGlobal('fetch',fetcher)
 const accepted=vi.fn(), props={taskId:7,generation,readOnly:false,onAccepted:accepted}
 const view=render(<SummaryVisualRetry {...props}/>);view.rerender(<SummaryVisualRetry {...props}/>)
 expect(fetcher).not.toHaveBeenCalled()
 fireEvent.click(screen.getByRole('button',{name:'只重试配图'}))
 await screen.findByRole('status')
 fireEvent.click(screen.getByRole('button',{name:'只重试配图'}))
 await waitFor(()=>expect(accepted).toHaveBeenCalledTimes(1))
 const first=fetcher.mock.calls[0], second=fetcher.mock.calls[1]
 expect(first[0]).toBe('/api/v1/media/task/7/summary/visual-retry')
 expect(first[1].headers['Idempotency-Key']).toBe(second[1].headers['Idempotency-Key'])
 expect(first[1].body).toBe(second[1].body)
 expect(JSON.parse(first[1].body)).toEqual({expected_generation_id:'published',expected_generated_version:4,expected_content_digest:'original-digest',expected_source_id:'source',expected_source_digest:'source-digest',authorize_new_visual_budget:true})
 expect((screen.getByRole('button',{name:'补图请求已受理'}) as HTMLButtonElement).disabled).toBe(true)
 view.rerender(<SummaryVisualRetry {...props} generation={{...generation,generation_id:'new-attempt',visual_retry_available:false}}/> )
 expect(screen.queryByRole('button')).toBeNull()
 view.rerender(<SummaryVisualRetry {...props} generation={{...generation,generation_id:'new-attempt'}}/> )
 expect((screen.getByRole('button',{name:'只重试配图'}) as HTMLButtonElement).disabled).toBe(false)
})

test('task replacement aborts submission and discards late receipt',async()=>{
 let resolve!:(value:unknown)=>void
 const fetcher=vi.fn((_url:string,_options:RequestInit)=>new Promise(done=>{resolve=done}));vi.stubGlobal('fetch',fetcher)
 const accepted=vi.fn(),view=render(<SummaryVisualRetry taskId={7} generation={generation} readOnly={false} onAccepted={accepted}/>)
 fireEvent.click(screen.getByRole('button',{name:'只重试配图'}))
 fireEvent.click(screen.getByRole('button',{name:'正在提交补图请求…'}))
 expect(fetcher).toHaveBeenCalledTimes(1)
 const signal=fetcher.mock.calls[0][1].signal as AbortSignal
 view.rerender(<SummaryVisualRetry taskId={8} generation={{...generation,generation_id:'other-task'}} readOnly={false} onAccepted={accepted}/>)
 expect(signal.aborted).toBe(true)
 resolve(response())
 await waitFor(()=>expect((screen.getByRole('button',{name:'只重试配图'}) as HTMLButtonElement).disabled).toBe(false))
 expect(accepted).not.toHaveBeenCalled()
})

test('read-only or incomplete generated identity never admits a visual attempt',()=>{
 const props={taskId:7,generation,readOnly:true,onAccepted:vi.fn()}
 const view=render(<SummaryVisualRetry {...props}/>)
 expect(screen.queryByRole('button')).toBeNull()
 view.rerender(<SummaryVisualRetry {...props} readOnly={false} generation={{...generation,generated_source_digest:undefined}}/> )
 expect(screen.queryByRole('button')).toBeNull()
})
