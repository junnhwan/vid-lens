// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { createRef } from 'react'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { SummaryWorkspace } from './SummaryWorkspace'
import type { VideoTask } from '@/lib/types'
import type { VideoPlayerHandle } from '@/components/player/VideoPlayer'

const counters=vi.hoisted(()=>({playerMounts:0,chatMounts:0,summary:{version_ref:{generated_version:2} as {generated_version:number}|{revision_id:string},content_digest:'digest'}}))
vi.mock('@/components/shell/AppShell',()=>({useShell:()=>({user:{role:'USER'}})}))
vi.mock('@/lib/router',()=>({default:({href,children}:{href:string;children:React.ReactNode})=><a href={href}>{children}</a>}))
vi.mock('./useSummaryGeneration',()=>({useSummaryGeneration:()=>({generation:{status:'completed',result_state:'ready',text_state:'ready',mindmap_enabled:true,activities:[]},error:''})}))
vi.mock('./SummaryRevisionPanel',()=>({SummaryRevisionPanel:({renderContent}:{renderContent:(value:unknown)=>React.ReactNode})=><div>{renderContent(counters.summary)}</div>}))
vi.mock('./SummaryDocumentView',()=>({SummaryDocumentView:({onReference}:{onReference:(value:unknown)=>void})=><div><p id="summary-block-block">真实摘要入口</p><button onClick={()=>onReference({kind:'summary_selection',task_id:42,version_ref:{generated_version:2},document_digest:'digest',block_id:'block',block_digest:'block-digest',text_start:0,text_end:2,quote:'选段'})}>选段提问</button></div>}))
vi.mock('@/components/player/VideoPlayer',async()=>{const {forwardRef,useEffect,useState}=await import('react');return{VideoPlayer:forwardRef((_props,ref)=>{const [position,setPosition]=useState(19);useEffect(()=>{counters.playerMounts++},[]);return <button ref={ref as never} onClick={()=>setPosition(27)}>播放位置 {position}</button>})}})
vi.mock('@/components/chat/ChatWorkspace',async()=>{const {useEffect,useState}=await import('react');return{ChatWorkspace:({summaryContextRefs,onReturnSummaryContext}:{summaryContextRefs:{quote:string}[];onReturnSummaryContext:(index:number)=>void})=>{const[draft,setDraft]=useState('');useEffect(()=>{counters.chatMounts++},[]);return <div><label>问题草稿<input value={draft} onChange={event=>setDraft(event.target.value)}/></label>{summaryContextRefs.map((ref,index)=><p key={index}>{ref.quote}<button onClick={()=>onReturnSummaryContext(index)}>返回原段</button></p>)}</div>}}})
beforeEach(()=>{counters.playerMounts=0;counters.chatMounts=0;counters.summary={version_ref:{generated_version:2},content_digest:'digest'};vi.stubGlobal('requestAnimationFrame',(run:()=>void)=>{run();return 1})})
afterEach(()=>{cleanup();vi.unstubAllGlobals()})
test('focus and side tabs preserve drafts, references, playback and reader scroll',()=>{
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const {container}=render(<SummaryWorkspace task={task} readOnly playbackUrl="/actual-media" playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed />)
 const reader=container.querySelector('.summary-reader') as HTMLElement;reader.scrollTop=143;fireEvent.scroll(reader)
 fireEvent.click(screen.getByRole('button',{name:'播放位置 19'}))
 fireEvent.click(screen.getByRole('button',{name:'选段提问'}))
 fireEvent.change(screen.getByRole('textbox',{name:'问题草稿'}),{target:{value:'这段有什么前提？'}})
 fireEvent.click(screen.getByText('更多'));fireEvent.click(screen.getByRole('button',{name:'放大问答'}))
 expect(container.querySelector('.summary-workspace')?.classList.contains('focus-chat')).toBe(true)
 fireEvent.click(screen.getByRole('button',{name:'恢复布局'}))
 expect(reader.scrollTop).toBe(143)
 expect((screen.getByRole('textbox',{name:'问题草稿'}) as HTMLInputElement).value).toBe('这段有什么前提？')
 expect(screen.getByText('选段')).toBeTruthy()
 fireEvent.click(screen.getByRole('tab',{name:'回放'}))
 expect(screen.getByRole('button',{name:'播放位置 27'})).toBeTruthy()
 fireEvent.click(screen.getByRole('button',{name:'放大阅读'}))
 fireEvent.keyDown(window,{key:'Escape'})
 fireEvent.click(screen.getByRole('tab',{name:'问答 · 1 段引用'}))
 expect((screen.getByRole('textbox',{name:'问题草稿'}) as HTMLInputElement).value).toBe('这段有什么前提？')
 expect(counters.playerMounts).toBe(1);expect(counters.chatMounts).toBe(1)
})
test('narrow desktop starts with the auxiliary area collapsed; opening references preserves reader state',()=>{
 vi.stubGlobal('matchMedia',vi.fn(()=>({matches:true,addEventListener:vi.fn(),removeEventListener:vi.fn()})))
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const props={task,readOnly:true,playbackUrl:'/actual-media',playerRef:createRef<VideoPlayerHandle>(),onPlayhead:vi.fn(),onDuration:vi.fn(),onSeek:vi.fn(),refreshPlaybackUrl:vi.fn(),onChanged:vi.fn(),onGenerate:vi.fn(),onTechnical:vi.fn(),busy:false,indexed:true}
 const view=render(<SummaryWorkspace {...props}/>)
 expect(view.container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(true)
 const reader=view.container.querySelector('.summary-reader') as HTMLElement;reader.scrollTop=260;fireEvent.scroll(reader)
 fireEvent.click(screen.getByRole('button',{name:'选段提问'}))
 expect(view.container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(false)
 fireEvent.change(screen.getByRole('textbox',{name:'问题草稿'}),{target:{value:'窄屏草稿'}})
 fireEvent.click(screen.getByRole('button',{name:'收起侧栏'}))
 view.rerender(<SummaryWorkspace {...props}/>)
 expect(view.container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(true)
 fireEvent.click(screen.getByText('更多'));fireEvent.click(screen.getByRole('button',{name:'放大问答'}))
 expect(view.container.querySelector('.summary-workspace')?.classList.contains('side-collapsed')).toBe(false)
 expect(reader.scrollTop).toBe(260)
 expect((screen.getByRole('textbox',{name:'问题草稿'}) as HTMLInputElement).value).toBe('窄屏草稿')
 expect(screen.getByText('选段')).toBeTruthy()
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
 scroll.mockClear();counters.summary=next;view.rerender(<SummaryWorkspace {...props}/>)
 fireEvent.click(screen.getByRole('button',{name:'返回原段'}))
 expect(scroll).not.toHaveBeenCalled()
 expect(screen.getByRole('status').textContent).toContain('较早的摘要版本')
 expect((screen.getByRole('textbox',{name:'问题草稿'}) as HTMLInputElement).value).toBe('保留问题')
 expect(screen.getByText('选段')).toBeTruthy()
})

test('reading starts with content; completed processing and secondary actions stay in closed menus',()=>{
 const task={id:42,filename:'lesson.mp4',has_summary:true,file_md5:'media',source_type:'upload'} as VideoTask
 const view=render(<SummaryWorkspace task={task} readOnly playbackUrl="/actual-media" playerRef={createRef<VideoPlayerHandle>()} onPlayhead={vi.fn()} onDuration={vi.fn()} onSeek={vi.fn()} refreshPlaybackUrl={vi.fn()} onChanged={vi.fn()} onGenerate={vi.fn()} onTechnical={vi.fn()} busy={false} indexed />)
 const details=view.container.querySelector('.summary-generation-details') as HTMLDetailsElement
 expect(details.open).toBe(false)
 expect(view.container.querySelector('.summary-reader .summary-activities')).toBeNull()
 expect(view.container.querySelector('.summary-reader .summary-visual-retry')).toBeNull()
 expect((view.container.querySelector('.summary-more') as HTMLDetailsElement).open).toBe(false)
 fireEvent.click(screen.getByText('更多'))
 fireEvent.click(screen.getByRole('button',{name:'视频与来源'}))
 expect((view.container.querySelector('.summary-more') as HTMLDetailsElement).open).toBe(false)
 expect(screen.getByText('真实摘要入口')).toBeTruthy()
})
