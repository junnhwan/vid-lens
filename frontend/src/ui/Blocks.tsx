import type { ReactNode } from "react"
import type { Block, Citation, Frame, Inline } from "../contract/types"
import { citationClock, formatClock } from "../contract/text"
import { FrameThumb } from "./FrameThumb"

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
}: {
  items: Inline[] | undefined
  citations: Record<string, Citation>
  onCite: (citation: Citation) => void
}) {
  return (
    <>
      {(items ?? []).map((item, index) => {
        if (item.type === "text") return <span key={index}>{marks(item.text ?? "", item.marks)}</span>
        if (item.type === "link") {
          const url = item.url ?? ""
          if (!/^https?:\/\//i.test(url)) return <span key={index}>{item.text}</span>
          return (
            <a key={index} href={url} target="_blank" rel="noopener noreferrer">
              {item.text}
            </a>
          )
        }
        if (item.type === "citation") {
          const citation = item.citation_id ? citations[item.citation_id] : undefined
          if (!citation) return null
          return (
            <button type="button" className="time" data-citation="true" key={index} onClick={() => onCite(citation)}>
              {citationClock(citation)}
            </button>
          )
        }
        return null
      })}
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
        <Inlines items={block.content} citations={citations} onCite={onCite} />
      </p>,
    )
  }
  if (block.type === "quote") {
    return wrap(
      block.origin,
      <blockquote {...attrs}>
        <p>
          <Inlines items={block.content} citations={citations} onCite={onCite} />
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
            <Inlines items={item.content} citations={citations} onCite={onCite} />
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
