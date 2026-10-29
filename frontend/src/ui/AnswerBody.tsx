import { useEffect, useRef, useState, type ComponentProps } from "react"
import type { Answer, Block, Citation } from "../contract/types"
import { answerKicker } from "../app/labels"
import { BlockView, Inlines } from "./Blocks"
import { citationClock, formatClock } from "../contract/text"
import { FrameThumb } from "./FrameThumb"

export function answerGroups(answer: Answer): {title?: string; blocks: Block[]}[] {
  const byID = new Map(answer.blocks.map((block) => [block.id, block]))
  const assigned = new Map<string, number>()
  const parts = (answer.parts ?? []).map((part, index) => ({ title: part.title, blocks: part.block_ids.filter((id) => {
    if (!byID.has(id) || assigned.has(id)) return false
    assigned.set(id,index);return true
  }).map((id) => byID.get(id)!) }))
  const seen = new Set<number>()
  const groups: {title?: string; blocks: Block[]}[] = []
  for (const block of answer.blocks) {
    const index = assigned.get(block.id)
    if (index === undefined) groups.push({blocks:[block]})
    else if (!seen.has(index)) {seen.add(index);groups.push(parts[index])}
  }
  return groups
}

type Props = Omit<ComponentProps<typeof BlockView>, "block" | "sectionId" | "citations"> & {answer: Answer}
export function AnswerBody({ answer, ...props }: Props) {
  const [evidenceOpen, setEvidenceOpen] = useState(false)
  const [sourceID, setSourceID] = useState<string | null>(null)
  const evidenceRef = useRef<HTMLDetailsElement>(null)
  useEffect(() => { if (sourceID) evidenceRef.current?.scrollIntoView({ block: "nearest" }) }, [sourceID])
  const evidence = answer.evidence
  const sources = evidence?.text_sources ?? []
  const source = sources.find(item => item.citation_id === sourceID) ?? sources[0]
  const citation = source ? answer.citations[source.citation_id] : undefined
  const viewed = (evidence?.viewed_frame_ids ?? []).flatMap(id => {
    const frame = props.frames?.get(id) ?? answer.frames?.find(frame => frame.id === id)
    return frame ? [frame] : []
  })
  const onCite = (citation: Citation) => {
    if (sources.some(source => source.citation_id === citation.id)) {
      setSourceID(citation.id); setEvidenceOpen(true)
    } else props.onCite(citation)
  }
  const normalize = (text: string) => text.replace(/\\"/g,'"').replace(/[\s\p{P}]/gu,"")
  const first = answer.blocks[0]
  const firstText = first?.type === "paragraph" ? (first.content ?? []).filter((inline) => inline.type === "text").map((inline) => inline.text ?? "").join("") : ""
  const repeatedLead = !!answer.summary && normalize(answer.summary) === normalize(firstText)
  return <div className="turn-answer">
    {evidence ? <details ref={evidenceRef} className="answer-evidence" open={evidenceOpen} onToggle={event => setEvidenceOpen(event.currentTarget.open)}>
      <summary>{sources.length ? `依据 ${sources.length} 处视频原文` : "查看回答依据"}<span> · {evidence.viewed_frame_ids.length ? `本轮已查看 ${evidence.viewed_frame_ids.length} 张画面` : "本轮未查看画面"}</span></summary>
      <div className="answer-evidence-body">
        {sources.length ? <>
          <p className="evidence-note">各标记对应独立原文片段，字幕时间沿用来源记录。旁白对画面的介绍与实际看图分开记录。</p>
          <div className="source-choices" role="group" aria-label="视频原文片段">{sources.map(item => {
            const cite = answer.citations[item.citation_id]
            return cite ? <button type="button" key={item.citation_id} aria-pressed={source === item} onClick={() => setSourceID(item.citation_id)}>{citationClock(cite)}</button> : null
          })}</div>
          <blockquote className="source-original">{source?.text}</blockquote>
          {citation ? <button type="button" className="disclosure" onClick={() => props.onCite(citation)}>打开原视频核对 ↗</button> : null}
        </> : null}
        {viewed.length ? <div className="answer-viewed-frames">{viewed.map((frame, index) => <button type="button" key={frame.id} onClick={event => props.onOpenFrame(viewed, index, event.currentTarget)}><FrameThumb frame={frame} variant="thumbnail" /><span>{formatClock(frame.time_ms)}</span></button>)}</div> : null}
        {evidence.viewed_frame_ids.length > viewed.length ? <p className="evidence-note">部分已查看画面的预览暂不可用。</p> : null}
      </div>
    </details> : null}
    {answer.summary && !repeatedLead ? <p className="answer-lead">{answer.summary.replace(/\\"/g,'"')}</p> : null}
    {answerKicker(answer.kind) ? <p className="answer-kicker">{answerKicker(answer.kind)}</p> : null}
    <div className="answer-content">{answerGroups(answer).map((part, index) => <section className="answer-part" aria-label={part.title} key={`${part.title ?? "body"}-${index}`}>
      {part.title ? <h4>{part.title}</h4> : null}
      {part.blocks.map((block) => <BlockView key={block.id} block={block} sectionId={block.id} citations={answer.citations ?? {}} {...props} onCite={onCite} />)}
    </section>)}</div>
    {answer.limitations?.length ? <details className="answer-limits"><summary>证据与限制 <span>{answer.limitations.length}</span></summary><ul>{answer.limitations.map((item,index) => <li key={`${item.code}-${index}`}>{item.message} <Inlines items={(item.citation_ids ?? []).map((id) => ({type:"citation",citation_id:id}))} citations={answer.citations ?? {}} onCite={onCite} /></li>)}</ul></details> : null}
  </div>
}
