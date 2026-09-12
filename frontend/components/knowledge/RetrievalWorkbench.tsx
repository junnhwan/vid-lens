'use client'

import { useEffect, useRef, useState } from 'react'
import Link from 'next/link'
import { api } from '@/lib/api'
import type { KnowledgeBase } from '@/lib/types'
import type { RetrievalTestResult } from '@/lib/knowledge'
import { replayLink } from '@/lib/knowledge'
import { formatTimeRange } from '@/components/Citation'
import { Icon } from '@/components/ui/Icon'
import { useCrumb } from '@/components/shell/AppShell'
import styles from './KnowledgeWorkspace.module.css'

const STAGES = [{id:'vector',label:'语义召回',note:'向量相似度'}, {id:'keyword',label:'关键词召回',note:'BM25 精确术语'}, {id:'fusion',label:'融合候选',note:'RRF 合并排序'}, {id:'final',label:'最终证据',note:'重排与模态选择'}]

export function RetrievalWorkbench({ kbId }: { kbId:number }) {
  const [kb,setKB]=useState<KnowledgeBase | null>(null)
  const [loadError,setLoadError]=useState('')
  const [question,setQuestion]=useState('')
  const [mode,setMode]=useState('hybrid')
  const [topK,setTopK]=useState(5)
  const [stage,setStage]=useState('final')
  const [result,setResult]=useState<RetrievalTestResult | null>(null)
  const [busy,setBusy]=useState(false)
  const [error,setError]=useState('')
  const requestRef=useRef(0)
  useCrumb([{label:'知识库',href:'/kb'},{label:kb?.name || '知识库',href:`/kb/${kbId}`},{label:'检索测试台'}])
  useEffect(()=>{let active=true;setKB(null);setResult(null);setLoadError('');api.getKB(kbId).then(v=>{if(active)setKB(v)}).catch(e=>{if(active)setLoadError(e instanceof Error?e.message:'加载失败')});return()=>{active=false;requestRef.current++}},[kbId])
  const run=async()=>{
    if(!question.trim() || busy)return
    const rid=++requestRef.current;setBusy(true);setError('');setResult(null)
    try {const next=await api.testKnowledgeRetrieval(kbId,question.trim(),mode,topK);if(rid===requestRef.current)setResult(next)}
    catch(e){if(rid===requestRef.current)setError(e instanceof Error?e.message:'检索失败')}
    finally{if(rid===requestRef.current)setBusy(false)}
  }
  const rows=result?.trace.stages?.filter(s=>s.name===stage).flatMap(s=>s.citations || []) || []
  const counts=new Map<number,number>()
  for(const c of result?.citations || [])counts.set(c.task_id,(counts.get(c.task_id)||0)+1)
  if(loadError)return <div className={styles.lab}><p className={styles.error}>{loadError}</p><Link href="/kb">返回知识库</Link></div>
  return <div className={styles.lab}>
    <header className={styles.labHero}><div><span className={styles.eyebrow}>Retrieval / Observatory</span><h1>让每一次检索，都有迹可循。</h1><p className={styles.note}>{kb?.name || '加载知识库…'} · {kb?.member_count ?? '—'} 个来源视频。输入一个问题，观察证据如何被找到、融合与筛选。</p></div><Link className="btn btn-ghost" href={`/chat/kb/${kbId}`}>返回研究工作区 ↗</Link></header>
    <form className={styles.query} onSubmit={e=>{e.preventDefault();void run()}}>
      <label htmlFor="retrieval-question" className={styles.eyebrow}>Test query</label>
      <textarea id="retrieval-question" value={question} maxLength={1000} disabled={busy} onChange={e=>setQuestion(e.target.value)} placeholder="输入需要定位的术语、观点或问题…" />
      <div className={styles.queryBottom}><div className={styles.modes}>
        {[['hybrid','混合检索'],['vector','仅语义'],['keyword','仅关键词']].map(([value,label])=><button key={value} type="button" aria-pressed={mode===value} disabled={busy} onClick={()=>setMode(value)}>{label}</button>)}
        <label className={styles.note}>返回 <select aria-label="返回证据数量" value={topK} disabled={busy} onChange={e=>setTopK(Number(e.target.value))} style={{background:'var(--bg-2)',padding:5}}>{[3,5,10].map(n=><option key={n} value={n}>{n}</option>)}</select> 条</label>
      </div><button className="btn btn-primary" disabled={busy || !question.trim() || !kb}><Icon name="search" size="sm" />{busy?'检索中…':'运行检索'}</button></div>
    </form>
    <p className={styles.note} style={{marginTop:10}}>使用原始问题测试，不生成最终答案。运行可能产生向量化或模型重排用量；问题不会加入聊天历史。</p>
    <div role="status" aria-live="polite">{error && <p className={styles.error}>{error}</p>}{busy && <p className={`${styles.note} ${styles.busy}`} style={{marginTop:20}}>正在查询当前授权资料，收集各阶段结果…</p>}</div>
    <nav className={styles.pipeline} aria-label="检索阶段">{STAGES.map(s=>{
      const total=result?.trace.stages?.filter(x=>x.name===s.id).reduce((n,x)=>n+(x.citations?.length||0),0)
      return <button key={s.id} aria-pressed={stage===s.id} onClick={()=>setStage(s.id)}><span>{s.label}</span><strong>{result?total ?? 0:'—'}</strong><span className={styles.note}>{s.note}</span></button>
    })}</nav>
    {result ? <div className={styles.resultGrid}><section>
      <div className={styles.meta}><span>{STAGES.find(s=>s.id===stage)?.label} · {rows.length} 条</span><span>{result.trace.duration_ms} ms</span><span>{result.mode==='hybrid'?'混合检索':result.mode==='keyword'?'仅关键词':'仅语义'}</span></div>
      {rows.map((cite,i)=><article className={styles.result} key={`${cite.evidence_id}-${i}`}>
        <h3><span><span style={{color:'var(--acc)',fontFamily:'var(--font-mono)',marginRight:12}}>{String(i+1).padStart(2,'0')}</span>{cite.video_title || `视频 ${cite.task_id}`}</span><Link className="btn btn-sm btn-ghost" href={replayLink(cite.task_id,cite.start_ms,cite.time_range_status)}><Icon name="play" size="sm" />{cite.time_range_status==='unknown'?'打开视频':'回放'}</Link></h3>
        <div className={styles.meta}><span>{cite.time_range_status === 'unknown' ? '时间未知' : formatTimeRange(cite.start_ms,cite.end_ms)}</span><span>{cite.modality==='transcript'?'转写':cite.modality==='visual_ocr'?'画面文字':cite.modality==='visual_caption'?'画面描述':'来源模态未知'}</span></div>
        <p>{cite.anchor_quote || cite.content}</p>
        <div className={styles.meta}><span>语义排名 {cite.vector_rank || '—'}</span><span>关键词排名 {cite.keyword_rank || '—'}</span><span>RRF {cite.rrf_score?.toFixed(4) || '—'}</span><span>最终排名 {cite.final_rank || '—'}</span></div>
      </article>)}
      {rows.length===0 && <div className={styles.empty}>{stage==='keyword' && result.mode==='vector' || stage==='vector' && result.mode==='keyword' ? '本次测试未启用此召回通道。' : '这一阶段没有找到匹配片段。试试资料中的具体术语，或切换检索方式。'}</div>}
    </section><aside><span className={styles.eyebrow}>Source coverage</span><div className={styles.stats}><div className={styles.stat}><small>最终覆盖视频</small><b>{counts.size} / {result.task_ids.length}</b></div><div className={styles.stat}><small>最终证据</small><b>{result.citations.length}</b></div></div>
      {(kb?.videos || []).map(v=><div className={styles.step} key={v.task_id}><span>{v.title || `视频 ${v.task_id}`}</span><span style={{color:counts.has(v.task_id)?'var(--acc)':'var(--tx-4)',whiteSpace:'nowrap'}}>{counts.get(v.task_id)||0} 条</span></div>)}
      <p className={styles.note} style={{marginTop:20}}>覆盖数量表示本次命中的来源，不代表未命中视频没有相关内容。</p>
      {!!result.trace.fallbacks?.length && <p className={styles.error}>发生降级：{result.trace.fallbacks.join('、')}</p>}
      <details className={styles.details}><summary>实际检索问题</summary><p className={styles.note}>{result.trace.original_query}</p></details>
    </aside></div> : !busy && <div className={styles.empty}><Icon name="search" size="lg" /><p>从一个具体问题开始。</p><p>可以用同一个问题分别运行三种检索方式，对比术语命中与语义覆盖。</p></div>}
  </div>
}
