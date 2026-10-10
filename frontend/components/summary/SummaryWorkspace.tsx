import { useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import { VideoPlayer, type VideoPlayerHandle } from '@/components/player/VideoPlayer'
import { ChatWorkspace } from '@/components/chat/ChatWorkspace'
import { SummaryRevisionPanel } from './SummaryRevisionPanel'
import { SummaryReadView } from './SummaryReadView'
import { SummaryActivityList } from './SummaryActivityList'
import { SummaryVisualRetry } from './SummaryVisualRetry'
import { useSummaryGeneration } from './useSummaryGeneration'
import { taskTitle } from '@/lib/format'
import { summaryReferenceMatches, type SummaryContextRef } from '@/lib/summaryExperience'
import type { EffectiveSummaryView,VideoTask } from '@/lib/types'
import './SummaryWorkspace.css'
import { useShell } from '@/components/shell/AppShell'
import { useSummarySessionView, useSummaryTaskView } from './useSummaryViewState'
import Link from '@/lib/router'

interface Props { task: VideoTask; readOnly: boolean; playbackUrl: string | null; playerRef: RefObject<VideoPlayerHandle>; onPlayhead: (ms: number, playing: boolean) => void; onDuration: (seconds: number) => void; onSeek: (ms: number) => void; refreshPlaybackUrl: () => Promise<string | null>; onChanged: () => Promise<void> | void; onGenerate: () => void; onTechnical: () => void; busy: boolean; initialTimeMS?: number; indexed: boolean }

export function SummaryWorkspace({ task,readOnly,playbackUrl,playerRef,onPlayhead,onDuration,onSeek,refreshPlaybackUrl,onChanged,onGenerate,onTechnical,busy,initialTimeMS,indexed }:Props) {
  const [pane,setPane]=useState<'playback'|'chat'>('playback'),[chatSeen,setChatSeen]=useState(false),[focus,setFocus]=useState<'reading'|'chat'|null>(null),[collapsed,setCollapsed]=useState(() => window.matchMedia?.('(max-width: 1150px)').matches ?? false)
  const { user } = useShell()
  const [view] = useSummaryTaskView(user?.id, task.id)
  const [sessionView, patchSessionView] = useSummarySessionView(user?.id, task.id, view.sessionID)
  const [localContexts, setLocalContexts] = useState<SummaryContextRef[]>([]),[message,setMessage]=useState(''),[editBlocks,setEditBlocks]=useState<string[]>([])
  const contexts = user?.id ? sessionView.contexts : localContexts
  const setContexts = (next: SummaryContextRef[] | ((previous: SummaryContextRef[]) => SummaryContextRef[])) => {
    const value = typeof next === 'function' ? next(contexts) : next
    if (user?.id) patchSessionView({ contexts: value }); else setLocalContexts(value)
  }
  const [editExpectation,setEditExpectation]=useState<Pick<EffectiveSummaryView,'content_digest'|'version_ref'>|undefined>()
  const reader=useRef<HTMLElement>(null),position=useRef(0)
  const [summaryTitle,setSummaryTitle]=useState('')
  const committedSummary=useRef<{summary:EffectiveSummaryView;taskID:number;ownerID:number|undefined}|null>(null)
  const onSummaryCommitted=useCallback((summary:EffectiveSummaryView|null,taskID:number)=>{if(summary)setSummaryTitle(summary.document?.title||'');committedSummary.current=summary?{summary,taskID,ownerID:user?.id}:null},[user?.id])
  const returnToReference=(index:number)=>{
    const ref=contexts[index]
    if(!ref)return
    const matches=()=>{const current=committedSummary.current;return !!current&&current.taskID===task.id&&current.ownerID===user?.id&&summaryReferenceMatches(ref,task.id,current.summary)}
    const stale=()=>setMessage('这段引用来自较早的摘要版本；已保留原引用快照，请从当前正文重新选段。')
    if(!matches()){stale();return}
    toggleFocus(null)
    requestAnimationFrame(()=>{if(!matches()){stale();return}reader.current?.querySelector<HTMLElement>(`[id="summary-block-${encodeURIComponent(ref.block_id)}"]`)?.scrollIntoView({block:'center'})})
  }
  useEffect(() => { setLocalContexts([]); setSummaryTitle(''); setEditBlocks([]); setMessage(''); setPane('playback'); setChatSeen(false); position.current = 0 }, [user?.id, task.id])
  const [visualAttempt,setVisualAttempt]=useState(0),[editingRequest,setEditingRequest]=useState(0)
  const {generation,error}=useSummaryGeneration(task.id,`${task.updated_at}:${task.status}:${visualAttempt}`)
  const title=task.title||summaryTitle||taskTitle(task)
  const openChat=()=>{setChatSeen(true);setPane('chat');setCollapsed(false)}
  const toggleFocus=(next:'reading'|'chat'|null)=>{position.current=reader.current?.scrollTop||position.current;setFocus(next);if(next==='chat')openChat();requestAnimationFrame(()=>{if(reader.current)reader.current.scrollTop=position.current})}
  useEffect(()=>{const escape=(event:KeyboardEvent)=>{if(event.key==='Escape'&&focus&&!document.querySelector('[role=dialog]')){event.preventDefault();toggleFocus(null)}};window.addEventListener('keydown',escape);return()=>window.removeEventListener('keydown',escape)},[focus])
  const addContext=(ref:SummaryContextRef)=>{
    const key=JSON.stringify(ref)
    if(contexts.some(item=>JSON.stringify(item)===key)){openChat();return}
    if(contexts.length>=3||contexts.reduce((total,item)=>total+Array.from(item.quote).length,0)+Array.from(ref.quote).length>3000){setMessage('一次问题最多引用 3 段、合计 3000 字；请先移除部分引用。');return}
    setContexts(previous=>[...previous,ref]);openChat()
  }
  const seek=(ms:number)=>{setPane('playback');setCollapsed(false);if(focus==='chat')toggleFocus(null);onSeek(ms)}
  const editBlock=(id:string)=>{setEditBlocks([id]);requestAnimationFrame(()=>document.querySelector<HTMLElement>('.sumrev-compose')?.scrollIntoView({block:'center'}))}
  const ready=task.has_summary||generation?.result_state==='ready'||generation?.text_state==='ready'
  const running=['pending','queued','running','retry_waiting'].includes(generation?.status||'')
  return <div className={`summary-workspace${focus?` focus-${focus}`:''}${collapsed?' side-collapsed':''}`}>
    <header className="summary-workspace-heading"><div className="summary-heading-title"><h1>{title}</h1><p className="muted">{generation?.source?.kind==='subtitle'?'平台字幕':'音频转写'} · 视频摘要</p></div><div className="summary-workspace-tools">
      <details className="summary-generation-details"><summary>{running?'正在生成':generation?.visual_state==='failed'||generation?.visual_state==='skipped'&&generation?.fallback_reason?'配图未完成 · 详情':'生成详情'}</summary><div className="summary-details-content"><SummaryActivityList generation={generation} error={error} /><SummaryVisualRetry taskId={task.id} generation={generation} readOnly={readOnly} onAccepted={()=>{setVisualAttempt(value=>value+1);void onChanged()}} /></div></details>
      <button className="btn btn-sm" onClick={()=>toggleFocus(focus?null:'reading')}>{focus?'恢复布局':'放大阅读'}</button><button className="btn btn-sm" onClick={()=>setCollapsed(value=>!value)}>{collapsed?'展开侧栏':'收起侧栏'}</button>
      <details className="summary-more" onClick={event=>{if((event.target as HTMLElement).closest('button,a'))event.currentTarget.open=false}} onKeyDown={event=>{if(event.key==='Escape'){event.stopPropagation();event.currentTarget.open=false;event.currentTarget.querySelector<HTMLElement>('summary')?.focus()}}}><summary>更多</summary><div className="summary-details-content"><button className="btn btn-sm" onClick={()=>toggleFocus(focus==='chat'?null:'chat')}>放大问答</button><Link className="btn btn-sm" href={`/chat/v/${task.id}${view.sessionID ? `?session=${view.sessionID}` : ''}`}>独立问答</Link>{ready&&!readOnly&&<><button className="btn btn-sm" onClick={()=>{setEditingRequest(value=>value+1);requestAnimationFrame(()=>reader.current?.querySelector<HTMLElement>('.sumrev-compose')?.scrollIntoView({block:'center'}))}}>修改摘要</button><button className="btn btn-sm" disabled={busy||running} onClick={onGenerate}>重新生成原稿</button></>}<button className="btn btn-sm" onClick={onTechnical}>视频与来源</button></div></details>
    </div></header>
    {running&&<p className="summary-current-activity" role="status">{generation?.activities?.filter(row=>row.state==='running').at(-1)?.title||'正在处理视频，已保存的内容仍可阅读。'}</p>}
    <div className="summary-workspace-grid">
      <article ref={reader} className="summary-reader" tabIndex={0} aria-label="视频摘要" onScroll={()=>{position.current=reader.current?.scrollTop||0}}>
        {message&&<p className="summary-notice" role="status">{message}<button className="btn btn-sm" aria-label="关闭提示" onClick={()=>setMessage('')}>×</button></p>}
        {!ready&&<section className="summary-empty"><h2>{running?'正在整理视频摘要':['failed','dead'].includes(generation?.status||'')?'摘要尚未生成':'开始阅读这段视频'}</h2><p>{running?'处理会在后台继续。文字就绪后会先开放阅读。':'导入的视频已保存，可生成摘要后继续提问。'}</p>{!readOnly&&!running&&<button className="btn btn-primary" disabled={busy} onClick={onGenerate}>生成摘要</button>}</section>}
        {ready&&<><SummaryRevisionPanel compactEditing editingRequest={editingRequest} taskId={task.id} readOnly={readOnly} onChanged={onChanged} publishedDigest={generation?.content_digest} selectedBlockIDs={editBlocks} selectedExpectation={editExpectation} onClearSelection={()=>{setEditBlocks([]);setEditExpectation(undefined)}} renderContent={summary=><SummaryReadView readerRef={reader} onSummaryCommitted={onSummaryCommitted} taskId={task.id} summary={summary} mindmapEnabled={generation?.mindmap_enabled!==false} readOnly={readOnly} mediaRevision={task.file_md5} playbackReady={!!playbackUrl} onSeek={seek} onReference={addContext} onMessage={setMessage} onEditBlock={readOnly?undefined:id=>{setEditExpectation({content_digest:summary.content_digest,version_ref:summary.version_ref});editBlock(id)}} />} /></>}
      </article>
      <aside className="summary-side" aria-label="回放与问答">
        <div className="summary-side-tabs" role="tablist" aria-label="辅助视图"><button role="tab" aria-selected={pane==='playback'} onClick={()=>setPane('playback')}>回放</button><button role="tab" aria-selected={pane==='chat'} onClick={openChat}>问答{contexts.length?` · ${contexts.length} 段引用`:''}</button></div>
        <div className="summary-playback" hidden={pane!=='playback'}><VideoPlayer ref={playerRef} src={playbackUrl} title={title} initialTimeMs={initialTimeMS} onPlayhead={onPlayhead} onDuration={onDuration} onNeedRefresh={refreshPlaybackUrl} fallbackText="媒体暂未就绪；摘要正文与图注仍可阅读" /><p className="muted">点击章节或图片的回放按钮，回到对应画面。</p><details><summary>来源信息</summary><p>{task.source_type==='url'?'链接导入':'本地上传'}</p>{task.source_url&&<a href={task.source_url} target="_blank" rel="noopener noreferrer">打开原视频来源</a>}<p>{task.filename}</p></details></div>
        <div className="summary-chat" hidden={pane!=='chat'}>{chatSeen&&<ChatWorkspace embedded sharedSummaryState scopeType="video" targetId={task.id} scopeName={title} playbackUrl={null} refreshPlaybackUrl={refreshPlaybackUrl} suggestions={[]} videoHasTranscript={task.has_transcription||!!task.active_text_source_id||task.has_summary} videoRetrievable={indexed} videoVisualMode={task.visual_mode} summaryContextRefs={contexts} onReturnSummaryContext={returnToReference} onRemoveSummaryContext={index=>setContexts(previous=>previous.filter((_,i)=>i!==index))} onContextSent={()=>setContexts([])} />}</div>
      </aside>
    </div>
  </div>
}
