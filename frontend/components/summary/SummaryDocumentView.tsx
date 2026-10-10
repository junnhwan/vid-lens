import { useEffect, useMemo, useRef, useState } from 'react'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { HierarchyMap } from '@/components/HierarchyMap'
import { SummaryTags } from './SummaryTags'
import { Modal } from '@/components/ui/Modal'
import type { EffectiveSummaryView } from '@/lib/types'
import { orderedSummaryBlocks, summaryExperienceApi, uniqueUnicodeQuoteRange, type SummaryBlock, type SummaryContextRef, type SummaryFigure } from '@/lib/summaryExperience'

export const summaryBlockElementID = (id: string) => `summary-block-${encodeURIComponent(id)}`

function SummaryImage({ taskId, figure, onReference, onReplay, playable }: { taskId: number; figure: SummaryFigure; onReference: () => void; onReplay: () => void; playable: boolean }) {
  const [url, setURL] = useState(''), [error, setError] = useState(''), [attempt, setAttempt] = useState(0), [large, setLarge] = useState(false)
  useEffect(() => {
    const controller = new AbortController(); let localURL = ''
    setURL(''); setError('')
    void summaryExperienceApi.screenshot(taskId, figure.screenshot_ref, controller.signal).then(blob => { if (controller.signal.aborted) return; localURL = URL.createObjectURL(blob); setURL(localURL) }).catch(reason => { if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : '图片暂不可用') })
    return () => { controller.abort(); if (localURL) URL.revokeObjectURL(localURL) }
  }, [taskId, figure.screenshot_ref, attempt])
  return <figure className="summary-figure">
    {url && !error ? <button className="summary-image-button" onClick={() => setLarge(true)} aria-label={`放大图片：${figure.alt}`}><img src={url} alt={figure.alt} loading="lazy" onError={() => setError('图片加载失败')} /></button> : <div className="summary-image-placeholder" role="status">{error || '正在读取已保存画面…'}{error && <button className="btn btn-sm" onClick={() => setAttempt(value => value + 1)}>重试图片</button>}</div>}
    <figcaption><MarkdownAnswer content={figure.caption} /><p className="muted">{figure.supports}</p><div className="summary-figure-actions"><button className="btn btn-sm" onClick={onReference}>引用这张图</button><button className="btn btn-sm" disabled={!playable || figure.capture_ms == null} onClick={onReplay}>{figure.capture_ms == null ? '没有可定位时间' : `回放 ${Math.floor(figure.capture_ms / 60000)}:${String(Math.floor(figure.capture_ms / 1000) % 60).padStart(2, '0')}`}</button></div></figcaption>
    {large && <Modal title={figure.alt} width="min(1100px, 92vw)" onClose={() => setLarge(false)}><img src={url} alt={figure.alt} style={{ width: '100%', maxHeight: '75vh', objectFit: 'contain' }} /><p>{figure.caption}</p></Modal>}
  </figure>
}

export function SummaryDocumentView({ taskId, summary, mediaRevision, playbackReady, onSeek, onReference, onMessage,onEditBlock,readOnly=false,mindmapEnabled=true }: { taskId: number; summary: EffectiveSummaryView; mediaRevision: string; playbackReady: boolean; onSeek: (ms: number) => void; onReference: (ref: SummaryContextRef) => void; onMessage: (text: string) => void; onEditBlock?:(block:string)=>void;readOnly?:boolean;mindmapEnabled?:boolean }) {
  const doc = summary.document
  const blocks = useMemo(() => doc ? orderedSummaryBlocks(doc) : [], [doc])
  const nodes = useMemo(() => blocks.map(block => ({ id: block.id, parent_id: block.parent_id, title: block.title, references: block.source_refs.length })), [blocks])
  const [selected, setSelected] = useState<string | null>(null), [fullMap, setFullMap] = useState(false), [refBusy, setRefBusy] = useState(false)
  const referenceRequest = useRef<AbortController | null>(null)
  useEffect(() => { setRefBusy(false); return () => referenceRequest.current?.abort() }, [taskId,summary.content_digest])
  useEffect(() => { if (selected && !blocks.some(block => block.id === selected)) setSelected(null) }, [blocks, selected])
  const select = (id: string) => { setSelected(id); document.getElementById(summaryBlockElementID(id))?.scrollIntoView({ block: 'start', behavior: 'instant' }) }
  const playable = !!doc && playbackReady && doc.media_revision === mediaRevision
  const reference = async (block: SummaryBlock, quote?: string, figure?: SummaryFigure) => {
    if (!summary.version_ref || refBusy) return
    setRefBusy(true)
    const controller = new AbortController(); referenceRequest.current = controller
    try {
      const current = await summaryExperienceApi.block(taskId, block.id, summary.version_ref,controller.signal)
      if(controller.signal.aborted)return
      const exact = quote || (figure ? current.figures.find(item=>item.screenshot_ref===figure.screenshot_ref)?.canonical_caption : '') || Array.from(current.canonical_text).slice(0, 700).join('')
      const range = uniqueUnicodeQuoteRange(current.canonical_text, exact)
      if (!range) { onMessage('这段文字在当前章节中重复或已变化，请缩小选段后重试。'); return }
      if (current.document_digest !== summary.content_digest) { onMessage('摘要版本已变化，请刷新后重新选段。'); return }
      onReference({ kind: figure ? 'summary_screenshot' : 'summary_selection', task_id: taskId, version_ref: summary.version_ref, document_digest: current.document_digest, block_id: block.id, block_digest: current.block_digest, ...range, quote: exact, ...(figure ? { screenshot_ref: figure.screenshot_ref } : {}) })
      onMessage('已加入问题引用。问题与引用会分别发送。')
    } catch (reason) { if(!controller.signal.aborted)onMessage(reason instanceof Error ? reason.message : '引用读取失败，请重试') } finally { if(!controller.signal.aborted)setRefBusy(false) }
  }
  if (!doc) return <div><SummaryTags compact taskId={taskId} readOnly={readOnly} generationVersion={summary.version_ref&&'generated_version' in summary.version_ref?summary.version_ref.generated_version:undefined} /><div className="summary-body" onMouseUp={event=>{const selection=window.getSelection();if(!selection||selection.isCollapsed||!selection.anchorNode||!selection.focusNode||!event.currentTarget.contains(selection.anchorNode)||!event.currentTarget.contains(selection.focusNode))return;const quote=selection.toString().trim();if(quote)void reference({id:"summary-overview",parent_id:null,order:0,title:"摘要",body_markdown:summary.content,source_refs:[],figures:[]},quote)}}><MarkdownAnswer content={summary.content} domainTags /></div></div>
  return <div className="summary-document">
    {doc.overview && <section className="summary-overview" aria-label="摘要概览"><header><p className="product-eyebrow">概览</p><details className="summary-chapter-actions"><summary aria-label="概览操作">•••</summary><div><button className="btn btn-sm" disabled={refBusy} onClick={()=>void reference({id:'summary-overview',parent_id:null,order:0,title:'概览',body_markdown:doc.overview,source_refs:[],figures:[]})}>引用概览</button>{onEditBlock&&<button className="btn btn-sm" onClick={()=>onEditBlock('summary-overview')}>修改概览</button>}</div></details></header><div className="summary-body" onMouseUp={event=>{const selection=window.getSelection();if(!selection||selection.isCollapsed||!selection.anchorNode||!selection.focusNode||!event.currentTarget.contains(selection.anchorNode)||!event.currentTarget.contains(selection.focusNode))return;const quote=selection.toString().trim();if(quote)void reference({id:'summary-overview',parent_id:null,order:0,title:'概览',body_markdown:doc.overview,source_refs:[],figures:[]},quote)}}><MarkdownAnswer content={doc.overview} domainTags /></div></section>}
    <SummaryTags compact taskId={taskId} readOnly={readOnly} generationVersion={summary.version_ref&&'generated_version' in summary.version_ref?summary.version_ref.generated_version:undefined} />
    {mindmapEnabled&&<section className="summary-map-preview" aria-label="摘要导图预览"><header><span>思维导图</span><button className="btn btn-sm btn-ghost" onClick={() => setFullMap(true)}>展开导图</button></header><HierarchyMap compact title={doc.title} nodes={nodes} heading="摘要结构" label="摘要概念导图预览" selectedBlock={selected} onSelect={select} /></section>}
    {!playable && <p className="muted" role="status">{playbackReady ? '这份摘要属于另一媒体版本；当前不能按旧时间回放。' : '媒体播放源暂未就绪；正文与图注仍可阅读。'}</p>}
    {blocks.map(block => <section key={block.id} id={summaryBlockElementID(block.id)} className={`summary-chapter${block.parent_id ? ' child' : ''}`}>
      <header><h2>{block.title}</h2><details className="summary-chapter-actions"><summary aria-label={`章节操作：${block.title}`}>•••</summary><div><button className="btn btn-sm" disabled={refBusy} onClick={() => void reference(block)}>引用本章前 700 字</button>{onEditBlock && <button className="btn btn-sm" onClick={()=>onEditBlock(block.id)}>修改本章</button>}{block.source_refs[0]?.start_ms != null && <button className="btn btn-sm" disabled={!playable} onClick={() => onSeek(block.source_refs[0].start_ms!)}>回放此处</button>}</div></details></header>
      <div className="summary-body" onMouseUp={event => { const selection = window.getSelection(); if (!selection || selection.isCollapsed || !selection.anchorNode || !selection.focusNode || !event.currentTarget.contains(selection.anchorNode) || !event.currentTarget.contains(selection.focusNode)) return; const quote = selection.toString().trim(); if (quote) void reference(block, quote) }}><MarkdownAnswer content={block.body_markdown} domainTags /></div>
      {block.figures.map(figure => <SummaryImage key={figure.id} taskId={taskId} figure={figure} playable={playable} onReference={() => void reference(block, undefined, figure)} onReplay={() => figure.capture_ms != null && onSeek(figure.capture_ms)} />)}
    </section>)}
    {fullMap && <Modal title="摘要导图 · 与当前正文同一版本" width="min(1100px, 92vw)" onClose={() => setFullMap(false)}><HierarchyMap title={doc.title} nodes={nodes} heading="摘要结构" label="摘要概念导图" selectedBlock={selected} onSelect={id => { setFullMap(false); select(id) }} /></Modal>}
  </div>
}
