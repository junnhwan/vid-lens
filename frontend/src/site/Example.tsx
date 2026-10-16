import { useEffect, useState } from "react"
import type { ArticleResponse, Citation, Frame } from "../contract/types"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"
import { BlockView } from "../ui/Blocks"
import { Lightbox } from "../ui/Lightbox"
import { ModelIdentity } from "../ui/ModelIdentity"

export function Example() {
  const [data, setData] = useState<(ArticleResponse & { quality_note?: string }) | null>(null)
  const [error, setError] = useState("")
  const [lightbox, setLightbox] = useState<{
    frames: Frame[]
    index: number
    origin: HTMLElement | null
  }>({ frames: [], index: 0, origin: null })
  useEffect(() => {
    let active = true
    void fetch("/example/article.json")
      .then((r) => {
        if (!r.ok) throw new Error()
        return r.json()
      })
      .then((d) => {
        if (active) {
          setData(d)
          document.title = `${d.article.title} · 真实示例 · VidLens`
        }
      })
      .catch(() => {
        if (active) setError("暂时无法读取示例。")
      })
    return () => {
      active = false
    }
  }, [])
  const frames = new Map((data?.frames || []).map((f) => [f.id, f]))
  function cite(c: Citation) {
    const time = c.time_ms ?? c.range?.start_ms ?? 0
    const url = new URL(data?.article.source.canonical_url || "https://www.bilibili.com")
    url.searchParams.set("t", String(Math.floor(time / 1000)))
    window.open(url.toString(), "_blank", "noopener,noreferrer")
  }
  return (
    <>
      <SiteHeader />
      <main className="example-layout">
        <header className="example-intro">
          <p className="eyebrow">来自真实生成记录</p>
          <h1>
            先读一篇，<span>再开始自己的。</span>
          </h1>
          <p>预先生成的真实图文。这里的浏览不会创建模型任务，也不计入完成次数。</p>
          <a
            href={data?.article.source.canonical_url || "https://www.bilibili.com/video/BV14Upq6yEq7/"}
            target="_blank"
            rel="noopener noreferrer"
          >
            打开原视频 ↗
          </a>
        </header>
        {error ? <p role="alert">{error}</p> : null}
        {data ? (
          <article className="sheet example-sheet">
            <header className="article-head">
              <h1>{data.article.title}</h1>
              <p className="meta">预先生成的真实示例 · {new Date(data.article.published_at).toLocaleDateString("zh-Hans")}</p>
              <ModelIdentity model={data.generation_model} />
              {data.quality_note ? <p className="doc-note">{data.quality_note}</p> : null}
            </header>
            <div className="prose">
              {data.article.sections.map((s) => (
                <section key={s.id}>
                  <h2 id={s.id}>{s.title}</h2>
                  {s.blocks.map((b) => (
                    <BlockView
                      key={b.id}
                      block={b}
                      sectionId={s.id}
                      frames={frames}
                      citations={data.article.citations}
                      onCite={cite}
                      onOpenFrame={(f, index, origin) => setLightbox({ frames: f, index, origin })}
                      onAskFrame={() => {}}
                      onBroken={() => {}}
                      readOnly
                    />
                  ))}
                </section>
              ))}
              {data.article.limitations.length ? (
                <aside className="limits">
                  <h2>还没确认的部分</h2>
                  {data.article.limitations.map((l, i) => (
                    <p key={i}>{l.message}</p>
                  ))}
                </aside>
              ) : null}
            </div>
            <footer className="example-end">
              <p>带着自己的问题，再读一条视频。</p>
              <a href="/" className="btn primary">
                开始自己的解读 ↗
              </a>
            </footer>
          </article>
        ) : (
          <p className="meta">正在读取已生成的图文…</p>
        )}
      </main>
      <SiteFooter />
      <Lightbox
        canAsk={false}
        readOnly
        open={lightbox.frames.length > 0}
        frames={lightbox.frames}
        index={lightbox.index}
        origin={lightbox.origin}
        onClose={() => setLightbox({ frames: [], index: 0, origin: null })}
        onIndex={(index) => setLightbox((v) => ({ ...v, index }))}
        onAsk={() => {}}
        onBroken={() => {}}
      />
    </>
  )
}
