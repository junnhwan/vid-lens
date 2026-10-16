import { useEffect, useState, type ReactNode } from "react"
import { SiteFooter, SiteHeader } from "../ui/SiteHeader"

const pages = [
  ["index", "项目是什么"],
  ["start", "开始使用"],
  ["models", "关注点与模型"],
  ["evidence", "图文、出处与问答"],
  ["export", "下载与离线阅读"],
  ["privacy", "隐私与保留"],
  ["develop", "部署与开发"],
]
const sources = import.meta.glob("../../../docs/site/*.md", {
  query: "?raw",
  import: "default",
  eager: true,
}) as Record<string, string>
function slug(s: string) {
  return s.replace(/[^\p{L}\p{N}]+/gu, "-").replace(/^-|-$/g, "")
}
function inline(s: string): ReactNode[] {
  return s.split(/(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g).map((p, i) => {
    if (p.startsWith("`")) return <code key={i}>{p.slice(1, -1)}</code>
    if (p.startsWith("**")) return <strong key={i}>{p.slice(2, -2)}</strong>
    const m = p.match(/^\[([^\]]+)\]\(([^)]+)\)$/)
    if (m && /^(https?:\/\/|\/docs(?:\/|$)|\/example$)/.test(m[2]))
      return (
        <a key={i} href={m[2]}>
          {m[1]}
        </a>
      )
    return p
  })
}
function Code({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className="doc-code">
      <button
        type="button"
        onClick={() => {
          void navigator.clipboard
            .writeText(text)
            .then(() => setCopied(true))
            .catch(() => setCopied(false))
        }}
      >
        {copied ? "已复制" : "复制"}
      </button>
      <pre>
        <code>{text}</code>
      </pre>
    </div>
  )
}
function markdown(raw: string) {
  const lines = raw.split("\n"),
    nodes: ReactNode[] = []
  for (let i = 0; i < lines.length; i++) {
    const l = lines[i]
    if (!l.trim()) continue
    if (l.startsWith("```")) {
      const code: string[] = []
      while (++i < lines.length && !lines[i].startsWith("```")) code.push(lines[i])
      nodes.push(<Code key={i} text={code.join("\n")} />)
      continue
    }
    const h = l.match(/^(#{1,3}) (.*)$/)
    if (h) {
      const Tag = `h${h[1].length}` as "h1" | "h2" | "h3"
      nodes.push(
        <Tag key={i} id={slug(h[2])}>
          {h[2]}
        </Tag>,
      )
      continue
    }
    if (/^[-*] /.test(l)) {
      const items: string[] = []
      while (i < lines.length && /^[-*] /.test(lines[i])) items.push(lines[i++].slice(2))
      i--
      nodes.push(
        <ul key={i}>
          {items.map((s, j) => (
            <li key={j}>{inline(s)}</li>
          ))}
        </ul>,
      )
      continue
    }
    if (/^> /.test(l)) {
      nodes.push(
        <aside className="doc-note" key={i}>
          {inline(l.slice(2))}
        </aside>,
      )
      continue
    }
    const para = [l]
    while (i + 1 < lines.length && lines[i + 1].trim() && !/^(#|[-*] |>|```)/.test(lines[i + 1])) para.push(lines[++i])
    nodes.push(<p key={i}>{inline(para.join(" "))}</p>)
  }
  return nodes
}
export function Docs() {
  const id = location.pathname.split("/")[2] || "index"
  const item = pages.find((p) => p[0] === id)
  const raw = sources[`../../../docs/site/${id}.md`]
  const headings =
    raw
      ?.split("\n")
      .filter((l) => l.startsWith("## "))
      .map((l) => l.slice(3)) || []
  useEffect(() => {
    document.title = `${item?.[1] || "文档未找到"} · VidLens 文档`
  }, [id])
  return (
    <>
      <a className="skip" href="#doc-main">
        跳到正文
      </a>
      <SiteHeader />
      <div className="docs-layout">
        <aside className="docs-nav">
          <p>VIDLENS / FIELD GUIDE</p>
          <h2>阅读指南</h2>
          <nav aria-label="文档目录">
            {pages.map((p) => (
              <a key={p[0]} href={p[0] === "index" ? "/docs" : `/docs/${p[0]}`} aria-current={p[0] === id ? "page" : undefined}>
                {p[1]}
              </a>
            ))}
          </nav>
          <span>文字、画面与可回看的出处。</span>
        </aside>
        <main id="doc-main" className="doc-prose">
          <div className="doc-breadcrumb">
            使用文档 <span>/</span> {item?.[1] || "未找到"}
          </div>
          {raw ? (
            markdown(raw)
          ) : (
            <>
              <h1>这页文档还不存在</h1>
              <p>
                <a href="/docs">返回阅读指南</a>
              </p>
            </>
          )}
          <div className="doc-next">
            <span>还有想了解的？</span>
            <a href="https://github.com/junnhwan/vid-lens" target="_blank" rel="noopener noreferrer">
              在 GitHub 查看源码 ↗
            </a>
          </div>
        </main>
        <aside className="doc-toc">
          <span>本页内容</span>
          {headings.map((h) => (
            <a key={h} href={`#${slug(h)}`}>
              {h}
            </a>
          ))}
        </aside>
      </div>
      <SiteFooter />
    </>
  )
}
