import type { ComponentProps } from "react"
import type { Answer, Block } from "../contract/types"
import { answerKicker } from "../app/labels"
import { BlockView, Inlines } from "./Blocks"

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
  const normalize = (text: string) => text.replace(/\\"/g,'"').replace(/[\s\p{P}]/gu,"")
  const first = answer.blocks[0]
  const firstText = first?.type === "paragraph" ? (first.content ?? []).filter((inline) => inline.type === "text").map((inline) => inline.text ?? "").join("") : ""
  const repeatedLead = !!answer.summary && normalize(answer.summary) === normalize(firstText)
  return <div className="turn-answer">
    {answer.summary && !repeatedLead ? <p className="answer-lead">{answer.summary.replace(/\\"/g,'"')}</p> : null}
    {answerKicker(answer.kind) ? <p className="answer-kicker">{answerKicker(answer.kind)}</p> : null}
    <div className="answer-content">{answerGroups(answer).map((part, index) => <section className="answer-part" aria-label={part.title} key={`${part.title ?? "body"}-${index}`}>
      {part.title ? <h4>{part.title}</h4> : null}
      {part.blocks.map((block) => <BlockView key={block.id} block={block} sectionId={block.id} citations={answer.citations ?? {}} {...props} />)}
    </section>)}</div>
    {answer.limitations?.length ? <details className="answer-limits"><summary>证据与限制 <span>{answer.limitations.length}</span></summary><ul>{answer.limitations.map((item,index) => <li key={`${item.code}-${index}`}>{item.message} <Inlines items={(item.citation_ids ?? []).map((id) => ({type:"citation",citation_id:id}))} citations={answer.citations ?? {}} onCite={props.onCite} /></li>)}</ul></details> : null}
  </div>
}
