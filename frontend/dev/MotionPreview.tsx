import { useState } from 'react'
import { ShellFrame } from '@/components/shell/ShellFrame'
import { TranscriptPanel } from '@/components/video/TranscriptPanel'
import { ThinkingProcess } from '@/components/chat/ThinkingProcess'
import { MarkdownAnswer } from '@/components/chat/MarkdownAnswer'
import { EvidenceDrawer } from '@/components/chat/EvidenceDrawer'
import type { CiteRef } from '@/components/Citation'
import type { ChatMsg } from '@/components/chat/chatUtils'
import type { TimelineAtom, VideoTask } from '@/lib/types'
import '@/components/chat/ChatWorkspace.css'

const rows = Array.from({ length: 16 }, (_, i) => ({
  id: `motion-row-${i}`, modality: 'transcript', start_ms: i * 15000, end_ms: (i + 1) * 15000, time_range_status: 'exact',
  content: `第 ${i + 1} 段：先理解视频里的概念，再结合上下文回看。手动滚动后，可以停下来阅读前面的段落，不会被下一句带走。`,
  paragraphs: [`第 ${i + 1} 段：先理解视频里的概念，再结合上下文回看。手动滚动后，可以停下来阅读前面的段落，不会被下一句带走。`], source_ids: [`motion-row-${i}`],
})) satisfies (TimelineAtom & { paragraphs: string[]; source_ids: string[] })[]
const citations: CiteRef[] = [0, 10].map((index, i) => ({ id: `C${i + 1}`, chunkIndex: index, score: 1, content: rows[index].content, anchorQuote: rows[index].content, displayContext: `${rows[index].content}\n${'完整上下文。'.repeat(150)}`, startMS: rows[index].start_ms, endMS: rows[index].end_ms, timeRangeStatus: 'exact', modality: 'transcript' }))
const task = { id: 0, has_transcription: true } as VideoTask

/** Isolated local state for browser feel-checks; no API, provider or media calls. */
export default function MotionPreview() {
  const [index, setIndex] = useState(0)
  const [streaming, setStreaming] = useState(true)
  const [citation, setCitation] = useState<CiteRef | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [active, setActive] = useState<number>()
  const message: ChatMsg = { role: 'assistant', content: '先理解概念 [C1]，再回到另一处来源 [C2]。', streaming, trace: [{ id: 'preview-retrieve', kind: 'retrieve', label: '查找来源', status: 'done', detail: '样例：找到 2 个片段' }, { id: 'preview-answer', kind: 'answer', label: '整理回答', status: streaming ? 'running' : 'done', detail: '仅展示交互状态，不调用模型' }] }
  const openCitation = (number: number) => { setActive(number); setCitation(citations[number - 1]); setDrawerOpen(true) }
  return <ShellFrame pathname="/chat" crumb={[{ label: '交互验收 · 样例' }]} user={{ name: '开发预览', detail: '样例数据 · 无后台调用' }} products preview>
    <div className="product-preview-banner">仅开发环境 · 字幕、引用和执行步骤均为样例。这里不播放视频，也不连接模型或后台任务。</div>
    <div className="page motion-preview">
      <h1>阅读与证据交互</h1><p className="muted">检查完成反馈、自由阅读、引用切换和抽屉关闭。</p>
      <div className="product-actions"><button className="btn" onClick={() => setIndex(value => (value + 1) % rows.length)}>下一句（样例）</button><button className="btn" onClick={() => setIndex(12)}>跳到第 13 句（样例）</button><button className="btn btn-primary" onClick={() => setStreaming(value => !value)}>{streaming ? '完成回答（样例）' : '重新生成（样例）'}</button></div>
      <div className="motion-preview-grid">
        <section><ThinkingProcess message={message} /><div className="answer"><MarkdownAnswer content={message.content} activeCite={active} onCite={openCitation} /></div><p className="muted">样例播放位置：第 {index + 1} 句</p></section>
        <section className="rail-pane on"><TranscriptPanel task={task} transcriptAtoms={rows} transcriptRows={rows} visualAtoms={[]} timelineMs={240000} playheadMs={rows[index].start_ms} headSnap={false} liveIndex={index} seek={ms => setIndex(Math.min(rows.length - 1, Math.floor(ms / 15000)))}>{null}</TranscriptPanel></section>
      </div>
    </div>
    {citation && <EvidenceDrawer open={drawerOpen} cite={citation} cites={citations} onSelect={next => { setCitation(next); setActive(Number(next.id.slice(1))) }} fallbackTitle="交互验收样例" canJump onJump={next => { setIndex(next.chunkIndex); setDrawerOpen(false) }} onClose={() => setDrawerOpen(false)} onExited={() => setCitation(null)} />}
  </ShellFrame>
}
