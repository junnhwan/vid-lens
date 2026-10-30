import { useEffect, useRef, useState } from "react"
import type { SuggestedQuestionsView } from "../contract/types"

// Reading metadata never starts model work. Poll only an already admitted job,
// and fence late responses when the reader switches to another video.
export function useSuggestedQuestions(resource:string|undefined, initial:SuggestedQuestionsView|undefined, load:()=>Promise<SuggestedQuestionsView>) {
  const loader=useRef(load);loader.current=load
  const [saved,setSaved]=useState<{resource:string;view:SuggestedQuestionsView}|null>(null)
  useEffect(()=>{
    let disposed=false,timer:ReturnType<typeof setTimeout>|undefined
    let reads=0,failures=0
    if (!resource || !initial || !["queued","running"].includes(initial.status)) return
    const id=resource
    async function poll() {
      if (disposed || reads>=90) return
      if (typeof document!=="undefined" && document.hidden) {timer=setTimeout(poll,3000);return}
      reads++
      try {
        const view=await loader.current()
        if (disposed) return
        setSaved({resource:id,view});failures=0
        if (["queued","running"].includes(view.status)) timer=setTimeout(poll,2000)
      } catch {
        if (disposed) return
        if (++failures<4) timer=setTimeout(poll,4000)
        else setSaved({resource:id,view:{status:"incomplete",items:[]}})
      }
    }
    timer=setTimeout(poll,2000)
    return ()=>{disposed=true;clearTimeout(timer)}
  },[resource,initial?.status])
  return !initial || !["queued","running"].includes(initial.status) ? initial : saved && saved.resource===resource ? saved.view : initial
}
