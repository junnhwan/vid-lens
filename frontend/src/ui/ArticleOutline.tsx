import { useState } from "react"
import type { Article } from "../contract/types"

export function ArticleOutline({ sections, current, onJump }: { sections: Article["sections"]; current: string; onJump: (id: string) => void }) {
  const [open, setOpen] = useState(true)
  if (!sections.length) return null
  return <aside className="article-outline" aria-label="文章目录">
    <div className="outline-heading"><span>文章目录</span><button type="button" aria-expanded={open} aria-controls="article-outline-items" onClick={() => setOpen((value) => !value)}>{open ? "收起" : "展开"}</button></div>
    {open ? <nav id="article-outline-items" aria-label="章节导航"><a className="outline-top" href="#article-top" onClick={(event) => {event.preventDefault();onJump("article-top")}}>回到开头 <span aria-hidden="true">↑</span></a><ol>{sections.map((section, index) => <li key={section.id}><a href={`#${section.id}`} aria-current={current === section.id ? "location" : undefined} onClick={(event) => {event.preventDefault();onJump(section.id)}}><span className="outline-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span><span>{section.title}</span></a></li>)}</ol></nav> : null}
  </aside>
}
