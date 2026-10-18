import { useRef, useState } from "react"
import { ApiError, sessionLost } from "../api/error"
import { citationURL } from "../contract/text"
import type {
  Citation,
  Frame
} from "../contract/types"
import { prefersReduced } from "../ui/motion"
import type { VisitorContext } from "./visitorContext"
import { errorText } from "./visitorContract"
export function useVisitorMedia(context: VisitorContext) {
  const workspace = context.workspace
  const { lost: markLost, announce: setLive } = context.messages
  function ownsRequest(gen: number, id: string) { return workspace.ownsRequest(gen, id, context.request()?.request.id) }

  const [historyOpen, setHistoryOpen] = useState(false)
  const [frameMap, setFrameMap] = useState<Map<string, Frame>>(() => new Map())
  const [lightbox, setLightbox] = useState<{
    open: boolean
    frames: Frame[]
    index: number
    origin: HTMLElement | null
  }>({ open: false, frames: [], index: 0, origin: null })
  const historyRef = useRef(false)
  const lightboxRef = useRef(false)
  const engagedRef = useRef(false)
  historyRef.current = historyOpen
  lightboxRef.current = lightbox.open
  engagedRef.current = historyOpen || lightbox.open
  function absorb(list: Frame[]) {
    if (!list.length) return
    setFrameMap((prev) => {
      let changed = false
      const next = new Map(prev)
      for (const frame of list) {
        const old = next.get(frame.id)
        if (
          !old ||
          old.availability !== frame.availability ||
          old.urls.full !== frame.urls.full ||
          old.urls.thumbnail !== frame.urls.thumbnail ||
          old.caption !== frame.caption
        ) {
          next.set(frame.id, frame)
          changed = true
        }
      }
      return changed ? next : prev
    })
  }
  function reset() { historyRef.current = false; lightboxRef.current = false; engagedRef.current = false; setHistoryOpen(false); setLightbox({ open: false, frames: [], index: 0, origin: null }); setFrameMap(new Map()) }
  function toggleHistory() { const next = !historyRef.current; historyRef.current = next; engagedRef.current = next || lightboxRef.current; setHistoryOpen(next); return next }
  return {
    absorb, reset, toggleHistory, historyOpen, engaged: () => engagedRef.current, frames: frameMap, view: {
      frames: frameMap,
      lightbox: {
        ...lightbox,
        frames: lightbox.frames.map((frame) => frameMap.get(frame.id) ?? frame),
      },
      openFrames: (frames: Frame[], index: number, origin: HTMLElement) => {
        lightboxRef.current = true
        engagedRef.current = true
        setLightbox({ open: true, frames, index, origin })
      },
      closeLightbox: () => {
        lightboxRef.current = false
        engagedRef.current = historyRef.current
        setLightbox((prev) => ({ ...prev, open: false }))
      },
      setLightboxIndex: (index: number) => setLightbox((prev) => ({ ...prev, index })),
      refreshFrame: (frame: Frame) =>
        void (async () => {
          const rt = context.runtime()
          const id = context.request()?.request.id
          if (!rt || !id) return
          const gen = workspace.epoch
          try {
            const next = await rt.api.frame(id, frame.id)
            if (!ownsRequest(gen, id)) return
            absorb([next])
          } catch (error) {
            if (!ownsRequest(gen, id)) return
            if (sessionLost(error)) markLost(error instanceof ApiError ? error.detail : "匿名会话已失效，请重新开始。")
          }
        })(),
      cite: (citation: Citation) =>
        void (async () => {
          if (citation.kind === "external_reference" && citation.reference_id) {
            const node = document.getElementById(`reference-${citation.reference_id}`)
            if (node instanceof HTMLDetailsElement) {
              node.open = true
              node.scrollIntoView({ block: "nearest", behavior: "auto" })
              node.querySelector("summary")?.focus()
            }
            return
          }
          if (citation.kind === "video_frame" && citation.frame_id) {
            let frame = frameMap.get(citation.frame_id) ?? null
            const rt = context.runtime()
            const id = context.request()?.request.id
            const gen = workspace.epoch
            if (!frame && rt && id) {
              try {
                frame = await rt.api.frame(id, citation.frame_id)
                if (!ownsRequest(gen, id)) return
                absorb([frame])
              } catch (error) {
                if (!ownsRequest(gen, id)) return
                setLive(errorText(error))
                return
              }
            }
            if (frame) {
              const origin = document.activeElement instanceof HTMLElement ? document.activeElement : document.body
              lightboxRef.current = true
              engagedRef.current = true
              setLightbox({ open: true, frames: [frame], index: 0, origin })
            }
            return
          }
          if (citation.kind === "video_text") {
            const url = citationURL(citation, context.article()?.article.source.canonical_url || context.request()?.source?.canonical_url || "")
            if (url) window.open(url, "_blank", "noopener,noreferrer")
            return
          }
          if (citation.block_id) {
            try {
              const node = document.querySelector(`[data-block-id="${CSS.escape(citation.block_id)}"]`)
              node?.scrollIntoView({
                behavior: prefersReduced() ? "auto" : "smooth",
                block: "center",
              })
              if (node instanceof HTMLElement && !prefersReduced()) {
                node.classList.remove("is-cited")
                void node.offsetWidth
                node.classList.add("is-cited")
                window.setTimeout(() => node.classList.remove("is-cited"), 700)
              }
            } catch {
              setLive(citation.label)
            }
            return
          }
          setLive(citation.quote || citation.label)
        })()
    }
  }
}
