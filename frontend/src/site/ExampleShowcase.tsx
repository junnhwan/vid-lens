import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { ApiError } from "../api/error"
import { formatDuration, platformLabel } from "../contract/text"

interface PublishedArticle {
  article_id: string
  title: string
  platform: string
  url: string
  duration_ms: number | null
  published_at: string
  shown: boolean
  position: number
}

const maxExamples = 24

function publishedDate(value: string) {
  return new Date(value).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false })
}

export function ExampleShowcase({ api, onAuthLost }: { api: HttpApi; onAuthLost: () => void }) {
  const [articles, setArticles] = useState<PublishedArticle[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [saved, setSaved] = useState<string[]>([])
  const [busy, setBusy] = useState("load")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const dirty = selected.join("\n") !== saved.join("\n")
  const authLost = useRef(onAuthLost)
  authLost.current = onAuthLost

  useEffect(() => {
    let stop = false
    setBusy("load")
    setError("")
    void api.request<{ items: PublishedArticle[] }>("GET", "/api/v1/admin/examples")
      .then(({ body }) => {
        if (stop) return
        const items = Array.isArray(body.items) ? body.items : []
        const order = items.filter((item) => item.shown).sort((a, b) => a.position - b.position).map((item) => item.article_id)
        setArticles(items)
        setSelected(order)
        setSaved(order)
      })
      .catch((reason: unknown) => {
        if (stop) return
        if (reason instanceof ApiError && reason.status === 401) authLost.current()
        else setError(reason instanceof ApiError ? reason.detail : "公开示例暂时没能读取。")
      })
      .finally(() => { if (!stop) setBusy("") })
    return () => { stop = true }
  }, [api])

  function toggle(id: string) {
    setNotice("")
    setSelected((current) => current.includes(id) ? current.filter((item) => item !== id) : current.length >= maxExamples ? current : [...current, id])
  }

  function move(id: string, delta: number) {
    setNotice("")
    setSelected((current) => {
      const index = current.indexOf(id)
      const next = index + delta
      if (index < 0 || next < 0 || next >= current.length) return current
      const copy = current.slice()
      const [item] = copy.splice(index, 1)
      copy.splice(next, 0, item)
      return copy
    })
  }

  async function save() {
    setBusy("save")
    setError("")
    setNotice("")
    try {
      const { body } = await api.request<{ items: PublishedArticle[] }>("PUT", "/api/v1/admin/examples", { article_ids: selected })
      const items = Array.isArray(body.items) ? body.items : []
      const order = items.filter((item) => item.shown).sort((a, b) => a.position - b.position).map((item) => item.article_id)
      setArticles(items)
      setSelected(order)
      setSaved(order)
      setNotice(order.length ? "公开示例已更新。" : "已清空选择。示例页现在没有公开文章。")
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 401) authLost.current()
      else setError(reason instanceof ApiError ? reason.detail : "公开示例暂时没能保存。")
    } finally {
      setBusy("")
    }
  }

  const byID = new Map(articles.map((item) => [item.article_id, item]))
  const ordered = [
    ...selected.map((id) => byID.get(id)).filter((item): item is PublishedArticle => Boolean(item)),
    ...articles.filter((item) => !selected.includes(item.article_id)).sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at)),
  ]

  return <section className="example-showcase" aria-labelledby="example-showcase-title">
    <h3 id="example-showcase-title">公开示例</h3>
    <p>勾选后，这篇图文和已经保存的追问会出现在公开示例页。没有勾选时，示例页不展示文章。最多 {maxExamples} 篇，可调整已选图文的顺序。</p>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {busy === "load" && articles.length === 0 ? <p className="fine">正在读取已发布图文…</p> : null}
    {busy !== "load" && articles.length === 0 && !error ? <p className="fine">还没有已发布的图文。</p> : null}
    {ordered.length ? <ul className="example-showcase-list">
      {ordered.map((item) => {
        const checked = selected.includes(item.article_id)
        const index = selected.indexOf(item.article_id)
        return <li className="example-showcase-item" key={item.article_id}>
          <label>
            <input type="checkbox" checked={checked} disabled={Boolean(busy) || (!checked && selected.length >= maxExamples)} onChange={() => toggle(item.article_id)} />
            <span><strong>{item.title}</strong><small>{[platformLabel(item.platform), formatDuration(item.duration_ms), publishedDate(item.published_at)].filter(Boolean).join(" · ")}</small></span>
          </label>
          {checked ? <span className="example-showcase-order">
            <button type="button" className="btn quiet desk-pill" disabled={Boolean(busy) || index === 0} onClick={() => move(item.article_id, -1)}>上移</button>
            <button type="button" className="btn quiet desk-pill" disabled={Boolean(busy) || index === selected.length - 1} onClick={() => move(item.article_id, 1)}>下移</button>
          </span> : null}
        </li>
      })}
    </ul> : null}
    {articles.length ? <div className="example-showcase-actions">
      <button type="button" className="btn primary" disabled={!dirty || Boolean(busy)} onClick={() => void save()}>{busy === "save" ? "正在保存…" : "保存示例"}</button>
      <span className="fine">已选 {selected.length} / {maxExamples}</span>
    </div> : null}
  </section>
}
