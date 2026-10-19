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

function meta(item: PublishedArticle) {
  return [platformLabel(item.platform), formatDuration(item.duration_ms), publishedDate(item.published_at)].filter(Boolean).join(" · ")
}

export function ExampleShowcase({ api, onDirty, onAuthLost }: { api: HttpApi; onDirty: (dirty: boolean) => void; onAuthLost: () => void }) {
  const [articles, setArticles] = useState<PublishedArticle[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [saved, setSaved] = useState<string[]>([])
  const [finder, setFinder] = useState("")
  const [busy, setBusy] = useState("load")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const dirty = selected.join("\n") !== saved.join("\n")
  const authLost = useRef(onAuthLost)
  authLost.current = onAuthLost
  const reportDirty = useRef(onDirty)
  reportDirty.current = onDirty

  useEffect(() => {
    reportDirty.current(dirty)
    return () => reportDirty.current(false)
  }, [dirty])

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

  function add(id: string) {
    setNotice("")
    setSelected((current) => current.includes(id) || current.length >= maxExamples ? current : [...current, id])
  }

  function remove(id: string) {
    setNotice("")
    setSelected((current) => current.filter((item) => item !== id))
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
  const ordered = selected.map((id) => byID.get(id)).filter((item): item is PublishedArticle => Boolean(item))
  const needle = finder.trim().toLocaleLowerCase("zh-CN")
  const available = articles
    .filter((item) => !selected.includes(item.article_id))
    .filter((item) => !needle || `${item.title} ${item.platform} ${item.url}`.toLocaleLowerCase("zh-CN").includes(needle))
    .sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at))

  return <section className="example-showcase" aria-labelledby="example-showcase-title">
    <header className="storage-heading">
      <div>
        <p className="eyebrow">PUBLIC EXAMPLES</p>
        <h2 id="example-showcase-title">公开示例</h2>
        <p className="fine">左边是访客示例页上的顺序，第 1 篇会默认打开。右边是可以加入的已发布图文。最多 {maxExamples} 篇，保存后访客才看得到。</p>
      </div>
    </header>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {busy === "load" && articles.length === 0 ? <p className="fine">正在读取已发布图文…</p> : null}
    {busy !== "load" && articles.length === 0 && !error ? <p className="fine">还没有已发布的图文。</p> : null}
    {articles.length ? <div className="example-boards">
      <section className="example-board" aria-labelledby="example-order-title">
        <h3 id="example-order-title">示例页顺序</h3>
        {ordered.length ? <ol className="example-order">
          {ordered.map((item, index) => <li key={item.article_id}>
            <span className="example-index">{index + 1}</span>
            <span><strong>{item.title}</strong><small>{meta(item)}{index === 0 ? " · 访客默认打开" : ""}</small></span>
            <span className="example-showcase-order">
              <button type="button" className="btn quiet desk-pill" disabled={Boolean(busy) || index === 0} onClick={() => move(item.article_id, -1)}>上移</button>
              <button type="button" className="btn quiet desk-pill" disabled={Boolean(busy) || index === ordered.length - 1} onClick={() => move(item.article_id, 1)}>下移</button>
              <button type="button" className="btn quiet desk-pill" disabled={Boolean(busy)} onClick={() => remove(item.article_id)}>移出</button>
              <a className="btn quiet desk-pill" href={`/example?video=${item.article_id}`} target="_blank" rel="noopener noreferrer">查看示例</a>
            </span>
          </li>)}
        </ol> : <p className="example-empty">示例页现在没有文章。</p>}
      </section>
      <section className="example-board" aria-labelledby="example-available-title">
        <h3 id="example-available-title">可加入的图文</h3>
        <label className="desk-field"><span>查找已发布图文</span><input type="search" value={finder} maxLength={200} placeholder="搜索标题、平台或链接" onChange={(event) => setFinder(event.target.value)} /></label>
        {available.length ? <ul className="example-available">
          {available.map((item) => <li key={item.article_id}>
            <span><strong>{item.title}</strong><small>{meta(item)}</small></span>
            <button type="button" className="btn quiet desk-pill" disabled={Boolean(busy) || selected.length >= maxExamples} onClick={() => add(item.article_id)}>{selected.length >= maxExamples ? "已满" : "加入"}</button>
          </li>)}
        </ul> : <p className="fine">{needle ? "没有找到可加入的图文。" : "已发布的图文都已在示例页上。"}</p>}
      </section>
    </div> : null}
    {articles.length ? <div className={`example-showcase-actions${dirty ? " is-dirty" : ""}`}>
      <button type="button" className="btn primary" disabled={!dirty || Boolean(busy)} onClick={() => void save()}>{busy === "save" ? "正在保存…" : "保存示例"}</button>
      <button type="button" className="btn quiet desk-pill" disabled={!dirty || Boolean(busy)} onClick={() => { setNotice(""); setSelected(saved) }}>撤销</button>
      <span className="fine">{dirty ? "访客仍看到上一版。" : "已与示例页一致。"} 已选 {selected.length} / {maxExamples}</span>
    </div> : null}
  </section>
}
