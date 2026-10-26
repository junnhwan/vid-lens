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
  it("groups scattered citations at the end of a paragraph",()=>{
    const cited: Answer={...answer,blocks:[{id:"c",type:"paragraph",origin:"video",content:[{type:"text",text:"先检查权限。"},{type:"citation",citation_id:"a"},{type:"text",text:"再看执行边界。"},{type:"citation",citation_id:"b"}]}],parts:undefined,citations:{a:{id:"a",kind:"video_text",label:"00:10",text_origin:"platform_subtitles",range:{start_ms:10000,end_ms:12000}},b:{id:"b",kind:"video_text",label:"00:40",text_origin:"platform_subtitles",range:{start_ms:40000,end_ms:42000}}}}
    const html=renderToStaticMarkup(<AnswerBody answer={cited} {...callbacks} />)
    expect(html.indexOf("再看执行边界。")).toBeLessThan(html.search(/00:10.00:40/))
    expect(html).toContain(">00:10<")
    expect(html).toContain(">00:40<")
  })
  it("provides independent chapter anchors and the current reading location",()=>{
    const html=renderToStaticMarkup(<ArticleOutline current="b" sections={[{id:"a",title:"原理",blocks:[]},{id:"b",title:"实践",blocks:[]}]} onJump={()=>undefined} />)
    expect(html).toContain('aria-label="文章目录"')
    expect(html).toContain("文章目录")
    expect(html).toContain("02/02")
    expect(html).toContain("outline-list")
    expect(html).toContain("实践")
    expect(html).toContain('href="#a"')
    expect(html).toContain('href="#b" aria-current="location"')
  })
})
