import { useEffect, useState, type Ref } from "react"
import type { ModelCatalog } from "../contract/types"
import { HttpApi } from "../api/http"
import { LikeButton } from "./LikeButton"
import { ModelIcon } from "./ModelIdentity"
import { Lens } from "./Lens"

export function EntryView({
  link,
  error,
  busy,
  ready = true,
  viewRef,
  csrf,
  onLink,
  onSubmit,
  purpose,
  onPurpose,
  catalog,
  selected,
  onSelect,
  catalogError,
  onReloadCatalog,
  active,
}: {
  link: string
  error: string
  busy: boolean
  ready?: boolean
  active: boolean
  viewRef?: Ref<HTMLElement>
  csrf: string
  onLink: (value: string) => void
  onSubmit: () => void
  onSample: () => void
  purpose: string
  onPurpose: (v: string) => void
  catalog: ModelCatalog | null
  selected: string
  onSelect: (v: string) => void
  catalogError: string
  onReloadCatalog: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [stats, setStats] = useState<{
    completed_readings: number
    likes: number
  } | null>(null)
  useEffect(() => {
    let active = true
    void new HttpApi()
      .stats()
      .then((v) => {
        if (active) setStats(v)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [])
  const model = catalog?.models.find((m) => m.id === selected)
  return (
    <section className="view-input" ref={viewRef}>
      <div className="entry editorial-entry">
        <div className="entry-copy">
          <p className="eyebrow">
            <span /> A CLOSER LOOK AT VIDEO
          </p>
          <h1>
            把视频，
            <br />
            读成一篇<span>图文。</span>
          </h1>
          <p className="entry-intro">
            循着原视频的文字与画面整理重点，
            <br />
            留下可回看的出处，也留一点继续追问的空间。
          </p>
          <form
            className="generation-form"
            onSubmit={(e) => {
              e.preventDefault()
              onSubmit()
            }}
          >
            <label htmlFor="link" className="field-label">
              视频链接
            </label>
            <div className="link-form">
              <input
                id="link"
                name="link"
                type="text"
                inputMode="url"
                autoComplete="off"
                spellCheck={false}
                placeholder="粘贴 B 站或 YouTube 视频链接"
                value={link}
                aria-invalid={error ? true : undefined}
                onChange={(e) => onLink(e.target.value)}
              />
              <button type="submit" className="btn primary" disabled={busy || !ready || !model}>
                {busy ? "正在提交" : "开始解读"}
                <span aria-hidden="true"> ↗</span>
              </button>
            </div>
            <div className="form-options">
              <label className="model-select">
                <ModelIcon brand={model?.brand_id} />
                <span className="sr-only">本次使用的模型</span>
                <select
                  aria-label="本次使用的模型"
                  value={selected}
                  disabled={busy || !catalog?.models.length}
                  onChange={(e) => onSelect(e.target.value)}
                >
                  {selected && !model ? (
                    <option value={selected} disabled>
                      原模型已不可用，请重新选择
                    </option>
                  ) : null}
                  {catalog?.models.length ? (
                    catalog.models.map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.display_name}
                      </option>
                    ))
                  ) : (
                    <option value="">{catalog ? "暂无可用模型" : "正在读取模型"}</option>
                  )}
                </select>
              </label>
              <button
                type="button"
                className="purpose-toggle"
                aria-expanded={expanded}
                aria-controls="purpose-field"
                onClick={() => setExpanded((v) => !v)}
              >
                {expanded ? "− 收起关注点" : "+ 添加关注点"}
                <small>可选</small>
              </button>
            </div>
            <div className="purpose-field" id="purpose-field" hidden={!expanded}>
              <label htmlFor="purpose">这次想重点了解什么？</label>
              <textarea
                id="purpose"
                value={purpose}
                rows={3}
                placeholder="比如：重点整理操作步骤、适用条件和容易忽略的细节。"
                onChange={(e) => onPurpose(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
                    e.preventDefault()
                    onSubmit()
                  }
                }}
              />
              <div>
                <span>留空也可以直接解读。</span>
                <span data-over={Array.from(purpose).length > 500}>{Array.from(purpose).length} / 500</span>
              </div>
            </div>
            {error ? (
              <p className="form-error" role="alert">
                {error}
              </p>
            ) : null}
            {catalogError ? (
              <p className="form-error">
                {catalogError}{" "}
                <button type="button" className="text-action" onClick={onReloadCatalog}>
                  重新读取模型
                </button>
              </p>
            ) : null}
            <p className="model-help">模型由站长提供，可能临时不可用。提交后，此视频的解读与问答固定使用同一模型。</p>
          </form>
          <div className="entry-bottom">
            <p>支持单个分 P 或单条视频。较长视频可能需要几分钟。</p>
            <a className="sample-link" href="/example">
              先看一篇真实示例 <span aria-hidden="true">→</span>
            </a>
          </div>
          <div className="proof">
            <p className="census">
              {stats ? (
                <>
                  已完成 <strong>{stats.completed_readings.toLocaleString("zh-Hans")}</strong> 次解读
                </>
              ) : (
                "从一条视频，开始另一种阅读。"
              )}
            </p>
            <div className="thanks">
              <LikeButton count={stats?.likes} ready={ready} csrf={csrf} />
              <a className="star-link" href="https://github.com/junnhwan/vid-lens" target="_blank" rel="noopener noreferrer">
                GitHub Star ↗
              </a>
            </div>
          </div>
        </div>
        <aside
          className={`entry-stage entry-art abstract-stage ${active ? "is-live" : ""}`}
          id="entry-stage"
          aria-label="流动的抽象镜片与视频切片"
        >
          <span className="lens-coordinate lens-coordinate-top">FRAME / TEXT / CONTEXT</span>
          <Lens />
          <div className="art-paper lens-paper-note">
            <span className="art-paper-label">从画面，到阅读</span>
            <h2>
              让值得留下的内容，
              <br />
              有迹可循。
            </h2>
            <div className="paper-lines">
              <i />
              <i />
              <i />
            </div>
            <p>阅读重点 · 回看出处 · 继续提问</p>
          </div>
          <div className="lens-annotation">
            <span className="lens-annotation-line" />
            <span>循着画面，找到理解的焦点。</span>
          </div>
          <div className="lens-coordinate lens-coordinate-bottom">
            <span>VIDLENS / A CLOSER LOOK</span>
            <span>01 — ∞</span>
          </div>
        </aside>
      </div>
    </section>
  )
}
