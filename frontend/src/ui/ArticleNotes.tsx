import type { ReactNode } from "react"
import type { Article, Limitation } from "../contract/types"

export function ArticleNotes({ limitations, sections, onJump, review, children }: {
  limitations: Limitation[]
  sections: Article["sections"]
  onJump: (id: string) => void
  review?: ReactNode
  children?: ReactNode
}) {
  const chapter = new Map(sections.map((section, index) => [section.id, { title: section.title, number: index + 1 }]))
  return <footer className="article-ending">
    <div className="reading-end-mark"><span>正文完</span></div>
    {limitations.length || review ? <section className="article-notes" aria-label="阅读附记">
      <h2 className="article-notes-title">阅读附记</h2>
      {limitations.length ? <aside className="article-limitations" aria-label="还需确认的内容">
        <div className="reading-section-head"><h3>还需确认的内容</h3><span className="reading-count">{limitations.length} 项</span></div>
        <ol className="reading-note-list">{limitations.map((item, index) => <li key={`${item.code}-${index}`}>
          <span className="reading-note-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
          <div><p>{item.message}</p>
            {(item.section_ids ?? []).filter((id) => chapter.has(id)).length ? <div className="reading-note-links">
              {[...new Set(item.section_ids)].filter((id) => chapter.has(id)).map((id) => <button type="button" className="reading-note-link" key={id} aria-label={`查看第 ${chapter.get(id)!.number} 章：${chapter.get(id)!.title}`} onClick={() => onJump(id)}>查看第 {chapter.get(id)!.number} 章 <span aria-hidden="true">↗</span></button>)}
            </div> : null}
          </div>
        </li>)}</ol>
      </aside> : null}
      {review}
    </section> : null}
    {children ? <div className="article-finish">{children}</div> : null}
  </footer>
}
