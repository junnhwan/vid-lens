import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { AIProfile, VideoTask } from '@/lib/types'
import { summaryExperienceApi, type TextSourceRefreshRequest } from '@/lib/summaryExperience'
import { Modal } from '@/components/ui/Modal'

export function TextSourceRefreshDialog({ task, readOnly, onClose, onAccepted }: { task: VideoTask; readOnly: boolean; onClose: () => void; onAccepted: () => void }) {
  const [profiles,setProfiles]=useState<AIProfile[]>([]),[ready,setReady]=useState(false),[legacy,setLegacy]=useState(false),[error,setError]=useState('')
  const [policy,setPolicy]=useState<'prefer_platform'|'force_asr'>(task.source_type==='url'?'prefer_platform':'force_asr'),[profileID,setProfileID]=useState(''),[autoSummary,setAutoSummary]=useState(true),[sending,setSending]=useState(false)
  const intent=useRef<{key:string;body:TextSourceRefreshRequest}>(),active=useRef<AbortController>()
  const basis=JSON.stringify([task.id,task.active_text_source_id,policy,profileID,autoSummary])
  useEffect(()=>{
    const controller=new AbortController();setReady(false);setError('');setLegacy(false)
    void Promise.all([api.listProfiles(),summaryExperienceApi.generation(task.id,controller.signal)]).then(([rows,generation])=>{
      if(controller.signal.aborted)return
      setProfiles(rows);setLegacy(generation.legacy);setReady(true)
    }).catch(()=>{if(!controller.signal.aborted)setError('配置读取失败，请关闭后重新打开。')})
    return()=>controller.abort()
  },[task.id])
  useEffect(()=>{active.current?.abort();intent.current=undefined;setSending(false);return()=>active.current?.abort()},[basis])
  const submit=async()=>{
    if(readOnly||!ready||legacy||active.current&&!active.current.signal.aborted)return
    intent.current??={key:`source-refresh-${crypto.randomUUID()}`,body:{expected_source_id:task.active_text_source_id||'',text_source_policy:policy,auto_summary:autoSummary,...(profileID?{profile_id:Number(profileID)}:{})}}
    const controller=new AbortController();active.current=controller;setSending(true);setError('')
    try{
      await summaryExperienceApi.refreshSource(task.id,intent.current.body,intent.current.key,controller.signal)
      if(!controller.signal.aborted)onAccepted()
    }catch(reason){
      if(!controller.signal.aborted)setError(reason instanceof ApiError&&reason.status===409?'来源或任务状态已变化，请关闭并刷新视频后重试。':reason instanceof ApiError?reason.message:'请求暂未确认。可重试同一次刷新，避免重复处理。')
    }finally{if(active.current===controller){active.current=undefined;if(!controller.signal.aborted)setSending(false)}}
  }
  return <Modal title="刷新文字来源" onClose={sending?()=>{}:onClose} width={520} footer={<><button className="btn" disabled={sending} onClick={onClose}>返回</button><button className="btn btn-primary" disabled={readOnly||!ready||legacy||sending} onClick={()=>void submit()}>{sending?'正在提交…':'开始刷新'}</button></>}>
    <p>新文字成功就绪后再替换已有来源；现有摘要、用户修订和历史引用会保留。</p>
    {!ready&&!error&&<p role="status">正在读取处理配置…</p>}
    {legacy&&<p role="status">此视频使用较早的处理方式，请使用转写操作；刷新不会自动升级旧内容。</p>}
    {error&&<p role="alert">{error}</p>}
    <fieldset disabled={sending||!ready||readOnly||legacy}>
      <label>文字来源<select className="input" aria-label="刷新文字来源策略" value={policy} onChange={event=>setPolicy(event.target.value as typeof policy)}><option value="prefer_platform" disabled={task.source_type!=='url'}>优先平台字幕，不可用时转写</option><option value="force_asr">重新识别视频音频</option></select></label>
      <label>处理配置<select className="input" aria-label="刷新处理配置" value={profileID} onChange={event=>setProfileID(event.target.value)}><option value="">沿用此视频配置，读取最新设置</option>{profiles.map(profile=><option key={profile.id} value={profile.id}>{profile.name}</option>)}</select></label>
      <label><input type="checkbox" checked={autoSummary} onChange={event=>setAutoSummary(event.target.checked)}/>文字就绪后继续生成摘要</label>
    </fieldset>
    <p className="muted">转写或生成摘要可能消耗模型额度；配置缺失时会说明原因。可先到<a href="/settings" target="_blank" rel="noopener noreferrer">设置</a>补齐配置，再继续处理。</p>
  </Modal>
}
