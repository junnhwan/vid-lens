import { createElement } from "react"
import { act, create, type ReactTestRenderer } from "react-test-renderer"
import { afterEach, expect, it, vi } from "vitest"
import type { SuggestedQuestionsView } from "../contract/types"
import { useSuggestedQuestions } from "./useSuggestedQuestions"

let renderer:ReactTestRenderer|undefined
afterEach(async()=>{await act(async()=>renderer?.unmount());renderer=undefined;vi.useRealTimers();vi.unstubAllGlobals()})
function Harness({id,initial,load}:{id:string;initial:SuggestedQuestionsView;load:()=>Promise<SuggestedQuestionsView>}){
  const view=useSuggestedQuestions(id,initial,load)
  return createElement("output",null,JSON.stringify(view))
}
it("polls only existing pending work and stops when stored questions are ready",async()=>{
  vi.useFakeTimers()
  const ready:SuggestedQuestionsView={status:"completed",items:[{question:"为什么要区分研究与实现阶段？"}]}
  const load=vi.fn().mockResolvedValue(ready)
  await act(async()=>{renderer=create(createElement(Harness,{id:"video",initial:{status:"queued",items:[]},load}))})
  await act(async()=>{vi.advanceTimersByTime(2000)})
  expect(JSON.stringify(renderer?.toJSON())).toContain("为什么要区分")
  await act(async()=>{vi.advanceTimersByTime(20000)})
  expect(load).toHaveBeenCalledTimes(1)
})
it("ignores late results from another video and never polls legacy empty metadata",async()=>{
  vi.useFakeTimers()
  let finish!:(value:SuggestedQuestionsView)=>void
  const load=vi.fn(()=>new Promise<SuggestedQuestionsView>(resolve=>{finish=resolve}))
  await act(async()=>{renderer=create(createElement(Harness,{id:"old",initial:{status:"running",items:[]},load}))})
  await act(async()=>{vi.advanceTimersByTime(2000)})
  await act(async()=>{renderer!.update(createElement(Harness,{id:"new",initial:{status:"unavailable",items:[]},load}));finish({status:"completed",items:[{question:"旧视频的问题不得出现"}]})})
  expect(JSON.stringify(renderer?.toJSON())).not.toContain("旧视频的问题")
  await act(async()=>{vi.advanceTimersByTime(20000)})
  expect(load).toHaveBeenCalledTimes(1)
})
it("uses new terminal metadata instead of a saved pending response",async()=>{
  vi.useFakeTimers()
  const load=vi.fn().mockResolvedValue({status:"running",items:[]})
  await act(async()=>{renderer=create(createElement(Harness,{id:"video",initial:{status:"queued",items:[]},load}))})
  await act(async()=>{vi.advanceTimersByTime(2000)})
  await act(async()=>{renderer!.update(createElement(Harness,{id:"video",initial:{status:"incomplete",items:[]},load}))})
  expect(JSON.stringify(renderer?.toJSON())).toContain("incomplete")
  await act(async()=>{vi.advanceTimersByTime(20000)})
  expect(load).toHaveBeenCalledTimes(1)
})
it("pauses reads in a hidden tab without consuming its read budget",async()=>{
  vi.useFakeTimers()
  const doc={hidden:true};vi.stubGlobal("document",doc)
  const load=vi.fn().mockResolvedValue({status:"completed",items:[]})
  await act(async()=>{renderer=create(createElement(Harness,{id:"video",initial:{status:"running",items:[]},load}))})
  await act(async()=>{vi.advanceTimersByTime(300000)})
  expect(load).not.toHaveBeenCalled()
  doc.hidden=false
  await act(async()=>{vi.advanceTimersByTime(3000)})
  expect(load).toHaveBeenCalledTimes(1)
})
