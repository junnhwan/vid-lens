'use client'

import { useEffect, useState } from 'react'
import { api, ApiError } from '@/lib/api'
import type { RunDetail, SessionMemoryPolicy } from '@/lib/knowledge'
import styles from './KnowledgeWorkspace.module.css'

const STATUS: Record<string, string> = { pending:'等待中', running:'进行中', completed:'已完成', failed:'失败', cancelled:'已取消', budget_exhausted:'预算已用完' }
const REASONS: Record<string,string> = { answer_generated:'答案已生成', goal_satisfied:'研究已完成', budget_exhausted:'已达到本次研究预算', scope_changed:'知识库成员发生变化', request_cancelled:'请求已取消', execution_failed:'执行失败' }

export function RunDetails({ sessionId, runId, live }: { sessionId: number; runId: string; live: boolean }) {
  const [open,setOpen] = useState(false)
  const [data,setData] = useState<RunDetail | null>(null)
  const [error,setError] = useState('')
  const [revision,setRevision] = useState(0)
  useEffect(() => {
    if (!open) return
    let active=true; setData(null); setError('')
    api.getRunDetail(sessionId,runId).then(d => { if(active) setData(d) }).catch(e => { if(active) setError(e instanceof Error ? e.message : '加载失败') })
    return () => { active=false }
  },[sessionId,runId,open,live,revision])
  return <details className={styles.details} open={open} onToggle={e=>setOpen(e.currentTarget.open)}><summary>运行详情 · 耗时与调用用量</summary>
    {error ? <p className={styles.error}>{error}</p> : !data ? <p className={styles.note}>加载执行记录…</p> : <>
      <div className={styles.stats}>
        <div className={styles.stat}><small>执行状态</small><b style={{fontSize:16}}>{STATUS[data.status] || data.status}</b></div>
        <div className={styles.stat}><small>累计步骤耗时</small><b>{(data.duration_ms / 1000).toFixed(1)}s</b></div>
        <div className={styles.stat}><small>工具调用 / 上限</small><b>{data.tools_used} / {data.tools_limit}</b></div>
        <div className={styles.stat}><small>模型 / 检索调用</small><b>{data.model_calls} / {data.retrieval_calls}</b></div>
      </div>
      <p className={styles.note}>{data.token_source === 'unknown' ? 'Token 用量暂不可用' : `输入 ${data.prompt_tokens.toLocaleString()} · 输出 ${data.completion_tokens.toLocaleString()} tokens（${data.token_source === 'actual' ? '实际用量' : data.token_source === 'mixed' ? '混合统计' : '估算'}）`}</p>
      {data.stop_reason && <p className={styles.note}>{REASONS[data.stop_reason] || data.stop_reason}</p>}
      {data.steps.map(step=><div className={styles.step} key={step.id}><span>{step.label}</span><span>{step.status==='done' ? `${((step.duration_ms || 0)/1000).toFixed(1)}s` : step.status==='running' ? '进行中' : '未完成'}</span></div>)}
    </>}
    <button className="btn btn-sm btn-ghost" onClick={()=>setRevision(v=>v+1)}>刷新记录</button>
  </details>
}

export function SessionMemoryControl({ sessionId, disabled }: { sessionId: number; disabled: boolean }) {
  const [view,setView]=useState<SessionMemoryPolicy | null>(null)
  const [busy,setBusy]=useState(false)
  const [error,setError]=useState('')
  useEffect(()=>{ let active=true; setView(null);setError('');api.getSessionMemoryPolicy(sessionId).then(v=>{if(active)setView(v)}).catch(()=>{if(active)setError('记忆策略暂不可用')});return()=>{active=false} },[sessionId])
  const change=async(policy:SessionMemoryPolicy['policy'])=>{
    if(!view)return;setBusy(true);setError('')
    try {setView(await api.updateSessionMemoryPolicy(sessionId,policy,view.version))}
    catch(e){setError(e instanceof ApiError && e.status===409 ? '设置已在别处修改，请按最新状态重新选择' : '保存失败，请重试');try{setView(await api.getSessionMemoryPolicy(sessionId))}catch{setView(null)}}finally{setBusy(false)}
  }
  return <div className={styles.memory}><label>本次会话的长期记忆<br/><select aria-label="会话记忆策略" value={view?.policy || 'inherit'} disabled={!view || busy || disabled || !view.effective_memory_policy.capability_enabled} onChange={e=>void change(e.target.value as SessionMemoryPolicy['policy'])}>
    <option value="inherit">沿用我的默认设置</option><option value="enabled">在此会话中开启</option><option value="disabled">在此会话中关闭</option>
  </select></label><p>{error || (view ? !view.effective_memory_policy.capability_enabled ? '服务端尚未开启记忆能力' : view.effective_memory_policy.effective_enabled ? '记住回答偏好；视频事实仍以当前证据为准' : '不召回或新增长期记忆' : '加载记忆设置…')}</p></div>
}
