import { useRef, useState } from "react"
import type { Frame } from "../contract/types"
import { formatClock } from "../contract/text"
import { FrameThumb } from "./FrameThumb"
import { ReadingDialog } from "./ReadingDialog"
import type { ArticleImageGroup } from "./articleImages"

export function ArticleGallery({ groups, active = true, onOpenFrame, onLocate, onBroken }: {
  groups: ArticleImageGroup[]
  active?: boolean
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onLocate: (sectionId: string, blockId: string) => void
  onBroken: (frame: Frame) => void
}) {
  const [open, setOpen] = useState(false)
  const [visited, setVisited] = useState(false)
  const pending = useRef<{ sectionId: string; blockId: string } | null>(null)
  const images = groups.flatMap((group) => group.images)
  const frames = images.flatMap((image) => image.frame ? [image.frame] : [])
  if (!images.length) return null
  return <>
    <button type="button" className="disclosure gallery-trigger" aria-haspopup="dialog" onClick={() => { setVisited(true); setOpen(true) }}>插图 <span>{images.length}</span></button>
    <ReadingDialog open={active && open} title={`文章插图 · ${images.length}`} onClose={() => setOpen(false)}
      onAfterClose={() => {
        const location = pending.current
        pending.current = null
        if (location) onLocate(location.sectionId, location.blockId)
      }} className="article-gallery">
      {visited ? <div className="gallery-grid">{groups.flatMap((group) => group.images.map((image) => <figure key={image.frameId}>
          <h3 className="gallery-chapter" title={group.sectionTitle}><span>{String(group.chapter).padStart(2, "0")}</span><span className="gallery-chapter-title">{group.sectionTitle}</span></h3>
          <button type="button" className="gallery-image" aria-label={`放大：${image.caption}`}
            disabled={!image.frame || image.frame.availability !== "ready" || !(image.frame.urls.full || image.frame.urls.thumbnail)}
            onClick={(event) => {
              if (!image.frame) return
              setOpen(false)
              onOpenFrame(frames, frames.indexOf(image.frame), event.currentTarget)
            }}>
            <FrameThumb frame={image.frame} variant="thumbnail" onBroken={onBroken} />
            {image.frame ? <time>{formatClock(image.frame.time_ms)}</time> : null}
          </button>
          <figcaption><p className="gallery-caption" title={image.caption}>{image.caption}</p>
            <button type="button" className="text-button" onClick={() => { pending.current = image; setOpen(false) }}>定位正文</button>
          </figcaption>
        </figure>))}</div> : null}
    </ReadingDialog>
  </>
}
