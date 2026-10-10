// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { summaryExperienceApi, type SummaryGeneration } from '@/lib/summaryExperience'
import { useSummaryGeneration } from './useSummaryGeneration'

vi.mock('@/lib/summaryExperience',()=>({summaryExperienceApi:{generation:vi.fn(),events:vi.fn()}}))
afterEach(()=>{cleanup();vi.resetAllMocks()})
const snapshot=(task_id:number,generation_id:string,watermark=0)=>({task_id,generation_id,status:'completed',legacy:false,requested_mode:'text',resolved_mode:'text',text_state:'ready',visual_state:'skipped',result_state:'ready',stage:'completed',generated_version:1,content_digest:'digest',content_hash_kind:'summary-json-v2',activities:[],event_high_watermark:watermark}) as SummaryGeneration
test('switching tasks rejects a late snapshot and aborts all old reads',async()=>{
 let finish!:(v:SummaryGeneration)=>void
 vi.mocked(summaryExperienceApi.generation).mockImplementation(id=>id===1?new Promise(resolve=>{finish=resolve}):Promise.resolve(snapshot(2,'new')))
 const view=renderHook(({id})=>useSummaryGeneration(id),{initialProps:{id:1}})
 const oldSignal=vi.mocked(summaryExperienceApi.generation).mock.calls[0][1]
 view.rerender({id:2})
 await waitFor(()=>expect(view.result.current.generation?.task_id).toBe(2))
 expect(oldSignal?.aborted).toBe(true)
 await act(async()=>finish(snapshot(1,'old')))
 expect(view.result.current.generation?.generation_id).toBe('new')
})
test('ordered event replay follows each server cursor and trusts the snapshot on gaps',async()=>{
 vi.mocked(summaryExperienceApi.generation).mockResolvedValue(snapshot(1,'generation',7))
 vi.mocked(summaryExperienceApi.events).mockResolvedValueOnce({generation_id:'generation',events:[],high_watermark:7,next_after_seq:3,has_more:true,cursor_gap:false}).mockResolvedValueOnce({generation_id:'generation',events:[],high_watermark:7,next_after_seq:7,has_more:false,cursor_gap:true})
 const view=renderHook(()=>useSummaryGeneration(1))
 await waitFor(()=>expect(view.result.current.generation?.event_high_watermark).toBe(7))
 expect(vi.mocked(summaryExperienceApi.events).mock.calls.map(call=>call[2])).toEqual([0,3])
 expect(view.result.current.generation?.activities).toEqual([])
})
