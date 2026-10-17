import { renderToStaticMarkup } from "react-dom/server"
import { describe, expect, it } from "vitest"
import type { Answer } from "../contract/types"
import { AnswerBody, answerGroups } from "./AnswerBody"
import { ArticleOutline } from "./ArticleOutline"

const answer: Answer = {kind:"grounded",summary:"隔离限制影响范围，快照减少重建成本。",blocks:[{id:"why",type:"paragraph",origin:"video",content:[{type:"text",text:"权限检查与执行边界解决不同的问题。"}]},{id:"how",type:"paragraph",origin:"video",content:[{type:"text",text:"快照能复用已经准备好的环境状态。"}]},{id:"bound",type:"paragraph",origin:"video",content:[{type:"text",text:"还需考虑状态的一致性。"}]}],parts:[{title:"隔离保护什么",block_ids:["why"]},{title:"快照节省哪一步",block_ids:["how"]}],citations:{},frames:[],limitations:[]}
const callbacks={frames:new Map(),onOpenFrame:()=>undefined,onAskFrame:()=>undefined,onCite:()=>undefined,onBroken:()=>undefined}
describe("answer composition and navigation",()=>{
  it("uses question-specific subtopics without losing ungrouped or legacy blocks",()=>{
    const groups=answerGroups(answer)
    expect(groups.flatMap((group)=>group.blocks.map((block)=>block.id))).toEqual(["why","how","bound"])
    const html=renderToStaticMarkup(<AnswerBody answer={answer} {...callbacks} />)
    expect(html).toContain("隔离保护什么")
    expect(html).toContain("快照节省哪一步")
    expect(html.match(/还需考虑状态的一致性/g)).toHaveLength(1)
    expect(answerGroups({...answer,parts:undefined}).flatMap((group)=>group.blocks)).toEqual(answer.blocks)
  })
  it("renders a literal repeated summary once and keeps its cited paragraph",()=>{
    const repeated={...answer,blocks:[{...answer.blocks[0],content:[{type:"text",text:answer.summary}]}],parts:undefined}
    const html=renderToStaticMarkup(<AnswerBody answer={repeated} {...callbacks} />)
    expect(html.match(/隔离限制影响范围/g)).toHaveLength(1)
  })
  it("provides independent chapter anchors and the current reading location",()=>{
    const html=renderToStaticMarkup(<ArticleOutline current="b" sections={[{id:"a",title:"原理",blocks:[]},{id:"b",title:"实践",blocks:[]}]} onJump={()=>undefined} />)
    expect(html).toContain('aria-label="文章目录"')
    expect(html).toContain('href="#a"')
    expect(html).toContain('href="#b" aria-current="location"')
  })
})
