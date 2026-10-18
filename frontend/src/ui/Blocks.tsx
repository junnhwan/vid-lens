import type { ReactNode } from "react"
import type { Block, Citation, Frame, Inline } from "../contract/types"
import { citationClock, citationStart, formatClock } from "../contract/text"
import { FrameThumb } from "./FrameThumb"

export function citationClusterLabel(cites: Citation[]): string {
  if (!cites.length) return ""
  if (cites.length === 1) return citationClock(cites[0])
  const starts = cites.map(citationStart).filter((value): value is number => value != null)
  if (starts.length === cites.length && starts.length > 1) {
    const min = Math.min(...starts)
    const max = Math.max(...starts)
    if (formatClock(min) === formatClock(max)) return formatClock(min)
    return `${formatClock(min)}–${formatClock(max)}`
  }
  return `${citationClock(cites[0])} +${cites.length - 1}`
}

function previewFrame(cites: Citation[], frames: Map<string, Frame> | undefined): Frame | undefined {
  for (const citation of cites) {
    const frame = citation.frame_id ? frames?.get(citation.frame_id) : undefined
    if (frame) return frame
  }
  return undefined
}

function CiteCluster({
  cites,
  frames,
  onCite,
}: {
  cites: Citation[]
  frames?: Map<string, Frame>
  onCite: (citation: Citation) => void
}) {
  if (!cites.length) return null
  const frame = previewFrame(cites, frames)
  if (cites.length === 1) {
    return (
      <span className="cite-cluster">
        <button type="button" className="time" data-citation="true" onClick={() => onCite(cites[0])}>
          {citationClock(cites[0])}
        </button>
        {frame ? (
          <span className="cite-preview" aria-hidden="true">
            <FrameThumb frame={frame} variant="thumbnail" />
          </span>
        ) : null}
      </span>
    )
  }
  return (
    <span className="cite-cluster">
      <span className="cite-range" aria-hidden="true">
        {citationClusterLabel(cites)}
      </span>
      <span className="cite-list">
        {frame ? (
          <span className="cite-preview" aria-hidden="true">
            <FrameThumb frame={frame} variant="thumbnail" />
          </span>
        ) : null}
        {cites.map((citation) => (
          <button type="button" className="time" data-citation="true" key={citation.id} onClick={() => onCite(citation)}>
            {citationClock(citation)}
          </button>
        ))}
      </span>
    </span>
  )
}

function marks(text: string, kinds: string[] | undefined): ReactNode {
  let node: ReactNode = kinds?.includes("code") ? text : text.replace(/\\"/g, '"')
  if (kinds?.includes("code")) node = <code>{node}</code>
  if (kinds?.includes("emphasis")) node = <em>{node}</em>
  if (kinds?.includes("bold")) node = <strong>{node}</strong>
  return node
}

export function Inlines({
  items,
  citations,
  onCite,
  frames,
}: {
  items: Inline[] | undefined
  citations: Record<string, Citation>
  onCite: (citation: Citation) => void
  frames?: Map<string, Frame>
}) {
  const body: ReactNode[] = []
  const cites: Citation[] = []
  for (const [index, item] of (items ?? []).entries()) {
    if (item.type === "text") {
      body.push(<span key={index}>{marks(item.text ?? "", item.marks)}</span>)
      continue
    }
    if (item.type === "link") {
      const url = item.url ?? ""
      if (!/^https?:\/\//i.test(url)) body.push(<span key={index}>{item.text}</span>)
      else
        body.push(
          <a key={index} href={url} target="_blank" rel="noopener noreferrer">
            {item.text}
          </a>,
        )
      continue
    }
    if (item.type === "citation") {
      const citation = item.citation_id ? citations[item.citation_id] : undefined
      if (citation) cites.push(citation)
    }
  }
  return (
    <>
      {body}
      <CiteCluster cites={cites} frames={frames} onCite={onCite} />
    </>
  )
}

function wrap(origin: string, children: ReactNode) {
  if (origin === "external_reference")
    return (
      <aside className="supplement external-supplement">
        <span className="supplement-kicker">网络补充</span>
        {children}
      </aside>
    )
  if (origin !== "background_explanation") return children
  return (
    <aside className="supplement">
      <span className="supplement-kicker">补充解释</span>
      {children}
    </aside>
  )
}

export function BlockView({
  block,
  sectionId,
  citations,
  frames,
  onOpenFrame,
  onAskFrame,
  onCite,
  onBroken,
  carry,
  readOnly,
}: {
  block: Block
  sectionId: string
  citations: Record<string, Citation>
  frames: Map<string, Frame>
  onOpenFrame: (frames: Frame[], index: number, origin: HTMLElement) => void
  onAskFrame: (frame: Frame) => void
  onCite: (citation: Citation) => void
  onBroken: (frame: Frame) => void
  carry?: boolean
  readOnly?: boolean
}) {
  const attrs = { "data-block-id": block.id, "data-section-id": sectionId }
  if (block.type === "paragraph") {
    return wrap(
      block.origin,
      <p {...attrs}>
        <Inlines items={block.content} citations={citations} onCite={onCite} frames={frames} />
      </p>,
    )
  }
  if (block.type === "quote") {
    return wrap(
      block.origin,
      <blockquote {...attrs}>
        <p>
          <Inlines items={block.content} citations={citations} onCite={onCite} frames={frames} />
        </p>
      </blockquote>,
    )
  }
  if (block.type === "list") {
    const Tag = block.ordered ? "ol" : "ul"
    return wrap(
      block.origin,
      <Tag {...attrs}>
        {(block.items ?? []).map((item) => (
          <li key={item.id}>
            <Inlines items={item.content} citations={citations} onCite={onCite} frames={frames} />
          </li>
        ))}
      </Tag>,
    )
  }
  if (block.type === "code") {
    return wrap(
      block.origin,
      <pre {...attrs}>
        <code>{block.text ?? ""}</code>
      </pre>,
    )
  }
  if (block.type === "table") {
    return wrap(
      block.origin,
      <table {...attrs}>
        <thead>
          <tr>
            {(block.columns ?? []).map((column, index) => (
              <th key={index}>{column}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {(block.rows ?? []).map((row, index) => (
            <tr key={index}>
              {row.cells.map((cell, cellIndex) => (
                <td key={cellIndex}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>,
    )
  }
  if (block.type === "figure") {
    const shown = (block.frame_ids ?? []).map((id) => frames.get(id)).filter((item): item is Frame => !!item)
    const layout = block.layout === "sequence" && shown.length > 1 ? "sequence" : "single"
    return (
      <figure className="figure" {...attrs}>
        <div className="shot-row" data-layout={layout}>
          {shown.map((frame, index) => (
            <button
              type="button"
              className="shot-btn"
              key={frame.id}
              onClick={(event) => onOpenFrame(shown, index, event.currentTarget.querySelector(".shot-host") || event.currentTarget)}
            >
              <span className="sr-only">放大 {formatClock(frame.time_ms)} 的画面</span>
              <span className="shot-host" data-carry-to={carry && index === 0 ? "true" : undefined}>
                <FrameThumb frame={frame} onBroken={onBroken} />
              </span>
              <span className="shot-badge" aria-hidden="true">
                {formatClock(frame.time_ms)}
              </span>
            </button>
          ))}
          {!shown.length ? <span className="frame-missing">这张画面现在不能显示</span> : null}
        </div>
        <figcaption>
          <span>{block.caption}</span>
          <span className="caption-actions">
            {shown.map((frame) => (
              <button
                type="button"
                className="time"
                key={frame.id}
                onClick={(event) => onOpenFrame(shown, shown.indexOf(frame), event.currentTarget)}
              >
                {formatClock(frame.time_ms)}
              </button>
            ))}
            {shown[0] && !readOnly ? (
              <button type="button" className="ask" onClick={() => onAskFrame(shown[0])}>
                问这张图
              </button>
            ) : null}
          </span>
        </figcaption>
      </figure>
    )
  }
  return (
    <p className="compat" {...attrs}>
      这一段使用了当前版本还不能显示的内容格式。
    </p>
  )
}
