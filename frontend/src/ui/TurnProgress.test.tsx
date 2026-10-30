import { createElement, type ComponentProps } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { ActivityStep, Answer, RunStatus, TurnView } from "../contract/types"
import { turnProgress, TurnProgress } from "./TurnProgress"
import { QaPanel } from "./QaPanel"

const stamp = "2026-10-30T01:13:50Z"
const now = Date.parse(stamp)
const current: ActivityStep = { id:"call", run_id:"run",kind:"model_response",model_call:5,segment_no:0,first_seq:1,last_seq:2,phase:"validate",title:"模型调用5",purpose:"正在独立复核事实、引用和实际配图",status:"running",source_range:null,frame_ids:[],frames:[],outcome:null,retry_of_step_id:null,started_at:stamp,finished_at:null,model_progress:{state:"thinking",observed_at:stamp,elapsed_ms:220000,call_limit_ms:600000} }
function view(status: RunStatus = "running"): TurnView {
  return { server_now:stamp,schema_version:1,turn:{id:"turn",conversation_id:"conversation",request_id:"request",article_id:"article",snapshot_id:"snapshot",turn_no:1,revision:1,status,question:"问题",context:{frame_id:null,passage:null},retry_of_id:null,answer:null,created_at:"2026-10-30T01:12:37Z",updated_at:stamp},execution:{id:"run",kind:"answer.generate",status,phase:"validate",retry_at:null,wait_reason:null},activity:{current_step_id:"call",recent_steps:[current],has_more:false,history_url:"",history_status:"available",history_expires_at:null},failure:null,actions:{can_cancel:status==="running",can_retry:false,can_submit_next:false},stream:{cursor:"cursor",revision:1,run_id:"run",seq:2} }
}
let renderer: ReactTestRenderer | undefined
afterEach(async()=>{ await act(async()=>renderer?.unmount());renderer=undefined;vi.unstubAllGlobals();vi.restoreAllMocks() })

describe("question progress",()=>{
  it("offers saved questions as editable starts without automatically sending",async()=>{
    vi.stubGlobal("window",{setInterval,clearInterval,matchMedia:()=>({matches:true})})
    const onSuggest=vi.fn(),onSend=vi.fn()
    await act(async()=>{renderer=create(createElement(QaPanel,{...panelProps(view()),turns:[],prompts:["什么时候应重开上下文窗口？","研究和规划阶段应分别保留哪些信息？"],promptSource:"generated",promptStatus:"completed",onSuggest,onSend}))})
    const group=renderer!.root.findByProps({"aria-label":"本视频推荐问题"})
    await act(async()=>group.findAllByType("button")[0].props.onClick())
    expect(onSuggest).toHaveBeenCalledWith("什么时候应重开上下文窗口？")
    expect(onSend).not.toHaveBeenCalled()
    expect(JSON.stringify(renderer?.toJSON())).toContain("打开会话不会重复生成")
  })
  it("shows a provisional answer while review is running and replaces it with the final result",async()=>{
    vi.stubGlobal("window",{setInterval,clearInterval,matchMedia:()=>({matches:true})})
    const preview:Answer={kind:"limited",summary:"先看已有依据",blocks:[{id:"p",type:"paragraph",origin:"video",content:[{type:"text",text:"原文支持的参考解释"}]}],citations:{},frames:[],limitations:[],evidence:{review:"pending",review_reason:"独立核验正在进行",text_sources:[],viewed_frame_ids:[]}}
    const props=panelProps({...view(),answer_preview:preview})
    await act(async()=>{renderer=create(createElement(QaPanel,props))})
    expect(JSON.stringify(renderer?.toJSON())).toContain("原文支持的参考解释")
    expect(JSON.stringify(renderer?.toJSON())).toContain("参考解读 · 正在独立核验")
    expect(JSON.stringify(renderer?.toJSON())).toContain("参考解读已显示")
    expect(renderer!.root.findAllByType("button").find(button=>button.props.className==="send stop-send")?.props.hidden).toBe(false)
    const final:Answer={...preview,summary:"正式的有限结论",evidence:{...preview.evidence!,review:"partial",review_reason:"只保留支持的部分",uncertainties:[{claim:"未证实的全片排名",reason:"仅查看了局部"}]}}
    await act(async()=>{renderer!.update(createElement(QaPanel,{...props,turns:[{...view("completed"),turn:{...view("completed").turn,answer:final}}]}))})
    expect(JSON.stringify(renderer?.toJSON())).not.toContain("参考解读 · 正在独立核验")
    expect(JSON.stringify(renderer?.toJSON())).toContain("部分内容已核验")
    expect(JSON.stringify(renderer?.toJSON())).toContain("未证实的全片排名")
  })
  it("retains a stopped preview without claiming review is still running",async()=>{
    vi.stubGlobal("window",{setInterval,clearInterval,matchMedia:()=>({matches:true})})
    const preview:Answer={kind:"limited",summary:"已保存参考解读",blocks:[],citations:{},frames:[],limitations:[],evidence:{review:"pending",review_reason:"独立核验正在进行",text_sources:[],viewed_frame_ids:[]}}
    await act(async()=>{renderer=create(createElement(QaPanel,panelProps({...view("canceled"),answer_preview:preview})))})
    const text=JSON.stringify(renderer?.toJSON())
    expect(text).toContain("已保存参考解读")
    expect(text).toContain("后续核验已停止")
    expect(text).not.toContain("参考解读 · 正在独立核验")
  })
  it("shows actual review thinking and stops claiming fresh activity when the receipt ages",()=>{
    expect(turnProgress(view(),current,now).title).toBe("正在核对回答依据 · 模型思考中")
    const quiet = turnProgress(view(),current,now+30000)
    expect(quiet.title).toContain("等待后续响应")
    expect(quiet.detail).toContain("30 秒前")
    expect(quiet.title).not.toContain("思考中")
  })
  it("distinguishes waiting, responding, queueing and stopping",()=>{
    expect(turnProgress(view(),{...current,model_progress:{...current.model_progress!,state:"waiting"}},now).title).toContain("等待模型响应")
    expect(turnProgress(view(),{...current,model_progress:{...current.model_progress!,state:"responding"}},now).title).toContain("正在接收响应")
    expect(turnProgress(view("queued"),current,now).title).toBe("正在等待处理")
    expect(turnProgress(view("cancel_requested"),current,now).title).toBe("正在停止回答")
  })
  it("anchors receipt freshness to server time even if the browser clock is far ahead",async()=>{
    vi.stubGlobal("window",{setInterval,clearInterval,matchMedia:()=>({matches:true})})
    vi.spyOn(Date,"now").mockReturnValue(now+3600000)
    await act(async()=>{renderer=create(createElement(TurnProgress,{view:view(),current}))})
    expect(JSON.stringify(renderer?.toJSON())).toContain("模型思考中")
    expect(JSON.stringify(renderer?.toJSON())).toContain("本轮已等待")
  })
  it("keeps stop visible, disables duplicate clicks and shows the pending cancellation immediately",async()=>{
    vi.stubGlobal("window",{setInterval,clearInterval,matchMedia:()=>({matches:true})})
    let done!:()=>void
    const onStop=vi.fn(()=>new Promise<void>(resolve=>{done=resolve}))
    const props:ComponentProps<typeof QaPanel>={open:true,turns:[view()],stepsFor:()=>[current],frames:new Map(),draft:"下一条草稿",frameChip:null,passageChip:null,prompts:[],busy:false,error:"",expanded:{},focusToken:0,onDraft:()=>{},onSend:()=>{},onStop,onRetry:()=>{},onClose:()=>{},onToggle:()=>{},onSuggest:()=>{},onRemoveFrame:()=>{},onRemovePassage:()=>{},onOpenFrame:()=>{},onCite:()=>{},onBroken:()=>{},externalAllowed:false,externalAvailable:false,searchAvailable:false,referenceLinks:"",onExternal:()=>{},onReferenceLinks:()=>{},modelUnavailable:false}
    await act(async()=>{renderer=create(createElement(QaPanel,props))})
    const stop=()=>renderer!.root.findAllByType("button").find(button=>button.props.className==="send stop-send")!
    await act(async()=>{stop().props.onClick();stop().props.onClick()})
    expect(onStop).toHaveBeenCalledTimes(1)
    expect(stop().props.disabled).toBe(true)
    expect(JSON.stringify(renderer?.toJSON())).toContain("正在停止回答")
    expect(JSON.stringify(renderer?.toJSON())).toContain("下一条草稿")
    await act(async()=>{renderer!.update(createElement(QaPanel,{...props,turns:[view("cancel_requested")]}));done()})
    expect(stop().props.hidden).toBe(false)
    expect(stop().props.disabled).toBe(true)
    await act(async()=>renderer!.update(createElement(QaPanel,{...props,turns:[view("canceled")]})))
    expect(stop().props.hidden).toBe(true)
  })
})

function panelProps(turn:TurnView):ComponentProps<typeof QaPanel> {
  return {open:true,turns:[turn],stepsFor:()=>[current],frames:new Map(),draft:"",frameChip:null,passageChip:null,prompts:[],busy:false,error:"",expanded:{},focusToken:0,onDraft:()=>{},onSend:()=>{},onStop:async()=>{},onRetry:()=>{},onClose:()=>{},onToggle:()=>{},onSuggest:()=>{},onRemoveFrame:()=>{},onRemovePassage:()=>{},onOpenFrame:()=>{},onCite:()=>{},onBroken:()=>{},externalAllowed:false,externalAvailable:false,searchAvailable:false,referenceLinks:"",onExternal:()=>{},onReferenceLinks:()=>{},modelUnavailable:false}
}
