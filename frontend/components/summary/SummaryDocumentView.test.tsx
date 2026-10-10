// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { SummaryDocumentView } from './SummaryDocumentView'
import { summaryExperienceApi, type SummaryBlockContext, type SummaryDocument } from '@/lib/summaryExperience'
import type { EffectiveSummaryView } from '@/lib/types'

vi.mock('@/components/summary/SummaryTags',()=>({SummaryTags:()=>null}))
vi.mock('@/components/HierarchyMap',()=>({HierarchyMap:({nodes,onSelect}:{nodes:{id:string;title:string}[];onSelect:(id:string)=>void})=><div>{nodes.map(node=><button key={node.id} onClick={()=>onSelect(node.id)}>{node.title}</button>)}</div>}))
vi.mock('@/lib/summaryExperience',async original=>({...await original<typeof import('@/lib/summaryExperience')>(),summaryExperienceApi:{block:vi.fn(),screenshot:vi.fn()}}))
const figure={id:'figure',screenshot_ref:'opaque-frame',capture_ms:19040,caption:'**图中**步骤😀',alt:'操作截图',supports:'解释步骤'}
const doc:SummaryDocument={schema_version:'summary-v2',document_id:'doc',source_id:'source',source_digest:'source-digest',media_revision:'media',presentation_mode:'image_text',title:'部署',overview:'先检查健康状态',blocks:[{id:'health',parent_id:null,order:0,title:'健康检查',body_markdown:'检查 **健康** 状态😀',source_refs:[{source_id:'source',cue_ids:['cue'],start_ms:19040,end_ms:22000,timing_method:'subtitle'}],figures:[figure]}]}
const summary={content:'markdown',revision:0,document:doc,version_ref:{generated_version:2},content_digest:'document-digest'} as EffectiveSummaryView
const context={canonical_text:'检查 健康 状态😀\n图中步骤😀',block_digest:'block-digest',document_digest:'document-digest',figures:[{...figure,canonical_caption:'图中步骤😀'}]} as SummaryBlockContext
beforeEach(()=>{
 vi.mocked(summaryExperienceApi.block).mockResolvedValue(context)
 vi.mocked(summaryExperienceApi.screenshot).mockResolvedValue(new Blob(['image']))
 vi.stubGlobal('requestAnimationFrame',(run:()=>void)=>{run();return 1})
 vi.stubGlobal('cancelAnimationFrame',vi.fn())
 URL.createObjectURL=vi.fn(()=> 'blob:actual-authorized-image');URL.revokeObjectURL=vi.fn()
 HTMLElement.prototype.scrollIntoView=vi.fn()
})
afterEach(()=>{cleanup();vi.resetAllMocks();vi.unstubAllGlobals()})
function view(media='media'){const refs=vi.fn(),seek=vi.fn(),notice=vi.fn();const rendered=render(<SummaryDocumentView taskId={42} summary={summary} mediaRevision={media} playbackReady onSeek={seek} onReference={refs} onMessage={notice}/>);return{...rendered,refs,seek,notice}}
test('screenshot references use server visible caption, Unicode offsets and exact version',async()=>{
 const current=view()
 fireEvent.click(screen.getByRole('button',{name:'引用这张图'}))
 await waitFor(()=>expect(current.refs).toHaveBeenCalledTimes(1))
 expect(current.refs.mock.calls[0][0]).toMatchObject({kind:'summary_screenshot',quote:'图中步骤😀',text_start:10,text_end:15,screenshot_ref:'opaque-frame',version_ref:{generated_version:2}})
 expect(summaryExperienceApi.screenshot).toHaveBeenCalledWith(42,'opaque-frame',expect.any(AbortSignal))
 current.unmount();expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:actual-authorized-image')
})
test('media mismatch disables every timestamp while keeping text and images readable',()=>{
 const current=view('replaced-media')
 expect((screen.getByRole('button',{name:'回放此处'}) as HTMLButtonElement).disabled).toBe(true)
 expect((screen.getByRole('button',{name:'回放 0:19'}) as HTMLButtonElement).disabled).toBe(true)
 expect(screen.getByText(/先检查健康状态/)).toBeTruthy();expect(current.seek).not.toHaveBeenCalled()
})
test('a stale block response never becomes a question reference',async()=>{
 vi.mocked(summaryExperienceApi.block).mockResolvedValue({...context,document_digest:'new-generation'})
 const current=view();fireEvent.click(screen.getByRole('button',{name:'引用本章前 700 字'}))
 await waitFor(()=>expect(current.notice).toHaveBeenCalledWith('摘要版本已变化，请刷新后重新选段。'))
 expect(current.refs).not.toHaveBeenCalled()
})
test('leaving a task aborts pending quote reads and suppresses late notices',async()=>{
 let finish!:(value:SummaryBlockContext)=>void
 vi.mocked(summaryExperienceApi.block).mockImplementation(()=>new Promise(resolve=>{finish=resolve}))
 const current=view();fireEvent.click(screen.getByRole('button',{name:'引用本章前 700 字'}))
 const signal=vi.mocked(summaryExperienceApi.block).mock.calls[0][3];current.unmount()
 expect(signal?.aborted).toBe(true)
 await act(async()=>finish(context));expect(current.refs).not.toHaveBeenCalled();expect(current.notice).not.toHaveBeenCalled()
})
test('overview quotes resolve the reserved block against the same generated version',async()=>{
 vi.mocked(summaryExperienceApi.block).mockResolvedValue({...context,canonical_text:'先检查健康状态',figures:[]})
 const current=view();fireEvent.click(screen.getByRole('button',{name:'引用概览'}))
 await waitFor(()=>expect(current.refs).toHaveBeenCalledTimes(1))
 expect(summaryExperienceApi.block).toHaveBeenCalledWith(42,'summary-overview',{generated_version:2},expect.any(AbortSignal))
 expect(current.refs.mock.calls[0][0]).toMatchObject({kind:'summary_selection',block_id:'summary-overview',text_start:0,text_end:7,quote:'先检查健康状态'})
})
