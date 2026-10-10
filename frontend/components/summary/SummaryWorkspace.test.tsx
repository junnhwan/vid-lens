// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { createRef } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { SummaryWorkspace } from './SummaryWorkspace'
import type { VideoTask } from '@/lib/types'
import type { VideoPlayerHandle } from '@/components/player/VideoPlayer'
import type { SummaryGeneration } from '@/lib/summaryExperience'

const counters=vi.hoisted(()=>({generation:{task_id:42,generation_id:'generation',legacy:false,requested_mode:'auto',resolved_mode:'text',visual_state:'skipped',stage:'completed',generated_version:2,content_digest:'digest',content_hash_kind:'document-v1',event_high_watermark:0,status:'completed',result_state:'ready',text_state:'ready',mindmap_enabled:true,activities:[]} as SummaryGeneration,playerMounts:0,chatMounts:0,summary:{version_ref:{generated_version:2} as {generated_version:number}|{revision_id:string},content_digest:'digest'}}))
vi.mock('@/components/shell/AppShell',()=>({useShell:()=>({user:{role:'USER'}})}))
vi.mock('@/lib/router',()=>({default:({href,children}:{href:string;children:React.ReactNode})=><a href={href}>{children}</a>}))
vi.mock('./useSummaryGeneration',()=>({useSummaryGeneration:()=>({generation:counters.generation,error:''})}))
vi.mock('./SummaryRevisionPanel',()=>({SummaryRevisionPanel:({renderContent}:{renderContent:(value:unknown)=>React.ReactNode})=><div>{renderContent(counters.summary)}</div>}))
vi.mock('./SummaryDocumentView',()=>({SummaryDocumentView:({onReference}:{onReference:(value:unknown)=>void})=><div><p id="summary-block-block">真实摘要入口</p><button onClick={()=>onReference({kind:'summary_selection',task_id:42,version_ref:{generated_version:2},document_digest:'digest',block_id:'block',block_digest:'block-digest',text_start:0,text_end:2,quote:'选段'})}>选段提问</button></div>}))
vi.mock('@/components/player/VideoPlayer',async()=>{const {forwardRef,useEffect,useState}=await import('react');return{VideoPlayer:forwardRef((_props,ref)=>{const [position,setPosition]=useState(19);useEffect(()=>{counters.playerMounts++},[]);return <button ref={ref as never} onClick={()=>setPosition(27)}>播放位置 {position}</button>})}})
vi.mock('@/components/chat/ChatWorkspace',async()=>{const {useEffect,useState}=await import('react');return{ChatWorkspace:({summaryContextRefs,onReturnSummaryContext}:{summaryContextRefs:{quote:string}[];onReturnSummaryContext:(index:number)=>void})=>{const[draft,setDraft]=useState('');useEffect(()=>{counters.chatMounts++},[]);return <div><label>问题草稿<input value={draft} onChange={event=>setDraft(event.target.value)}/></label>{summaryContextRefs.map((ref,index)=><p key={index}>{ref.quote}<button onClick={()=>onReturnSummaryContext(index)}>返回原段</button></p>)}</div>}}})
beforeEach(()=>{counters.generation={task_id:42,generation_id:'generation',legacy:false,requested_mode:'auto',resolved_mode:'text',visual_state:'skipped',stage:'completed',generated_version:2,content_digest:'digest',content_hash_kind:'document-v1',event_high_watermark:0,status:'completed',result_state:'ready',text_state:'ready',mindmap_enabled:true,activities:[]} as SummaryGeneration;counters.playerMounts=0;counters.chatMounts=0;counters.summary={version_ref:{generated_version:2},content_digest:'digest'};vi.stubGlobal('requestAnimationFrame',(run:()=>void)=>{run();return 1})})
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
test('reading is the default; conversation, playback and focus preserve drafts and reading position',()=>{
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const {container}=render(<SummaryWorkspace task={task} readOnly playbackUrl="/actual-media" playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed />)
 expect(container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(true)
 expect(counters.chatMounts).toBe(0)
 const reader=container.querySelector('.summary-reader') as HTMLElement;reader.scrollTop=143;fireEvent.scroll(reader)
 fireEvent.click(screen.getByRole('button',{name:'回放'}))
 fireEvent.click(screen.getByRole('button',{name:'播放位置 19'}))
 fireEvent.click(screen.getByRole('button',{name:'返回摘要'}))
 fireEvent.click(screen.getByRole('button',{name:'选段提问'}))
 expect(container.querySelector('.summary-workspace')?.classList.contains('focus-chat')).toBe(true)
 fireEvent.change(screen.getByRole('textbox',{name:'问题草稿'}),{target:{value:'这段有什么前提？'}})
 fireEvent.click(screen.getByRole('button',{name:'返回摘要'}))
 expect(reader.scrollTop).toBe(143)
 expect(container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'回放'}))
 expect(screen.getByRole('button',{name:'播放位置 27'})).toBeTruthy()
 fireEvent.keyDown(window,{key:'Escape'})
 fireEvent.click(screen.getByRole('tab',{name:'问答 · 1'}))
 expect((screen.getByRole('textbox',{name:'问题草稿'}) as HTMLInputElement).value).toBe('这段有什么前提？')
 expect(screen.getByText('选段')).toBeTruthy()
 expect(counters.playerMounts).toBe(1);expect(counters.chatMounts).toBe(1)
})
test('details open separately without interrupting the reader, and Escape returns to the same article',()=>{
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const props={task,readOnly:true,playbackUrl:'/actual-media',playerRef:createRef<VideoPlayerHandle>(),onPlayhead:vi.fn(),onDuration:vi.fn(),onSeek:vi.fn(),refreshPlaybackUrl:vi.fn(),onChanged:vi.fn(),onGenerate:vi.fn(),onTechnical:vi.fn(),busy:false,indexed:true}
 const view=render(<SummaryWorkspace {...props}/>)
 const reader=view.container.querySelector('.summary-reader') as HTMLElement;reader.scrollTop=260;fireEvent.scroll(reader)
 fireEvent.click(screen.getByRole('button',{name:/摘要已就绪/}))
 expect(screen.getByRole('dialog',{name:'摘要生成详情'})).toBeTruthy()
 fireEvent.keyDown(window,{key:'Escape'})
 expect(screen.queryByRole('dialog')).toBeNull()
 expect(reader.scrollTop).toBe(260)
 fireEvent.click(screen.getByRole('button',{name:'选段提问'}))
 fireEvent.change(screen.getByRole('textbox',{name:'问题草稿'}),{target:{value:'窄屏草稿'}})
 fireEvent.keyDown(window,{key:'Escape'})
 expect(view.container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(true)
 expect(reader.scrollTop).toBe(260)
 fireEvent.click(screen.getByRole('tab',{name:'问答 · 1'}))
 expect((screen.getByRole('textbox',{name:'问题草稿'}) as HTMLInputElement).value).toBe('窄屏草稿')
})

test.each([
 {version_ref:{generated_version:3},content_digest:'digest'},
 {version_ref:{generated_version:2},content_digest:'new-digest'},
 {version_ref:{revision_id:'user-revision'},content_digest:'digest'},
])('returning an old quote never jumps into a changed effective summary: %j',next=>{
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const props={task,readOnly:true,playbackUrl:'/actual-media',playerRef:createRef<VideoPlayerHandle>(),onPlayhead:vi.fn(),onDuration:vi.fn(),onSeek:vi.fn(),refreshPlaybackUrl:vi.fn(),onChanged:vi.fn(),onGenerate:vi.fn(),onTechnical:vi.fn(),busy:false,indexed:true}
 const view=render(<SummaryWorkspace {...props}/>)
 const scroll=vi.fn();(view.container.querySelector('#summary-block-block') as HTMLElement).scrollIntoView=scroll
 fireEvent.click(screen.getByRole('button',{name:'选段提问'}))
 fireEvent.change(screen.getByRole('textbox',{name:'问题草稿'}),{target:{value:'保留问题'}})
 fireEvent.click(screen.getByRole('button',{name:'返回原段'}))
 expect(scroll).toHaveBeenCalledOnce()
 scroll.mockClear();counters.summary=next;view.rerender(<SummaryWorkspace {...props}/>);fireEvent.click(screen.getByRole('tab',{name:'问答 · 1'}))
 fireEvent.click(screen.getByRole('button',{name:'返回原段'}))
 expect(scroll).not.toHaveBeenCalled()
 expect(screen.getByRole('status').textContent).toContain('较早的摘要版本')
 expect((screen.getByRole('textbox',{name:'问题草稿'}) as HTMLInputElement).value).toBe('保留问题')
 expect(screen.getByText('选段')).toBeTruthy()
})

test('reading starts with content; completed processing and secondary actions stay in closed menus',()=>{
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const view=render(<SummaryWorkspace task={task} readOnly playbackUrl="/actual-media" playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed />)
 expect(screen.queryByRole('dialog')).toBeNull()
 expect(view.container.querySelector('.summary-reader .summary-activities')).toBeNull()
 expect(view.container.querySelector('.summary-reader .summary-visual-retry')).toBeNull()
 expect((view.container.querySelector('.summary-more') as HTMLDetailsElement).open).toBe(false)
 fireEvent.click(screen.getByLabelText('更多操作'))
 fireEvent.click(screen.getByRole('button',{name:'视频与来源'}))
 expect((view.container.querySelector('.summary-more') as HTMLDetailsElement).open).toBe(false)
 expect(screen.getByText('真实摘要入口')).toBeTruthy()
})


test('real platform subtitle source and a failed text run are described accurately',()=>{
 counters.generation={...counters.generation,status:'failed',result_state:'pending',text_state:'failed',visual_state:'failed',source:{id:'source',kind:'platform_subtitle',digest:'digest',language:'ai-zh',quality:'usable'}}
 const task={id:42,filename:'lesson.mp4',has_summary:false,source_type:'url'} as VideoTask
 render(<SummaryWorkspace task={task} readOnly playbackUrl={null} playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed={false} />)
 expect(screen.getByText('平台字幕 · 链接导入')).toBeTruthy();fireEvent.click(screen.getByRole('button',{name:/摘要未完成/}))
 expect(screen.getByText('平台字幕 · ai-zh')).toBeTruthy()
 expect(screen.getAllByText('摘要未完成 · 详情').length).toBeGreaterThan(0)
 expect(screen.queryByText('配图未完成 · 详情')).toBeNull()
 expect(screen.getByRole('dialog',{name:'摘要生成详情'})).toBeTruthy()
})


test.each([1,2])('a real import in task status %i cannot start a duplicate summary before generation exists',status=>{
 counters.generation={...counters.generation,status:'not_started',result_state:'pending',text_state:'pending',visual_state:'pending',activities:[]}
 const task={id:42,status,stage:'downloading',filename:'lesson.mp4',has_summary:false,source_type:'url'} as VideoTask
 render(<SummaryWorkspace task={task} readOnly={false} playbackUrl={null} playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed={false} />)
 expect(screen.getByRole('heading',{name:'正在处理视频'})).toBeTruthy()
 expect(screen.queryByRole('button',{name:'生成摘要'})).toBeNull()
 expect(screen.getByRole('button',{name:/查看进度/})).toBeTruthy()
 expect(screen.getByRole('status').textContent).toContain('下载')
})

test('an explicit replay deep link opens the player even though normal reading starts alone',()=>{
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const view=render(<SummaryWorkspace task={task} readOnly playbackUrl="/actual-media" playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed initialTimeMS={19040} />)
 expect(view.container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(false)
 expect(screen.getByRole('button',{name:'播放位置 19'})).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'返回摘要'}))
 expect(view.container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(true)
})
