import { useEffect, useState, type Ref } from "react"
import type { ModelCatalog } from "../contract/types"
import { HttpApi } from "../api/http"
import { LikeButton } from "./LikeButton"
import { ChoiceMenu } from "./ChoiceMenu"
import { Lens } from "./Lens"
import { RepoPeek } from "./RepoPeek"
import { useRise } from "./countUp"

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
  const readings = useRise(stats?.completed_readings, active)
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
          <p className="entry-deck">循着原视频的文字与画面整理重点，留下可回看的出处，也留一点继续追问的空间。</p>
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
                <span aria-hidden="true" className="nudge">
                  {" "}
                  ↗
                </span>
              </button>
            </div>
            <div className="form-options">
              <ChoiceMenu
                label="本次使用的模型"
                value={selected}
                disabled={busy || !catalog?.models.length}
                placeholder={catalog ? "暂无可用模型" : "正在读取模型"}
                unavailable={selected && !model ? "原模型已不可用，请重新选择" : undefined}
                options={(catalog?.models ?? []).map((item) => ({
                  id: item.id,
                  label: item.display_name,
                  hint: item.description,
                  brand: item.brand_id,
                }))}
                onChange={onSelect}
              />
              <button
                type="button"
                className="purpose-toggle"
                aria-expanded={expanded}
                aria-controls="purpose-field"
                onClick={() => setExpanded((v) => !v)}
              >
                <span className="purpose-mark" aria-hidden="true" />
                <span className="purpose-copy">
                  <span aria-hidden={expanded ? true : undefined}>添加关注点</span>
                  <span aria-hidden={expanded ? undefined : true}>收起关注点</span>
                </span>
                <small>可选</small>
              </button>
            </div>
            <p className="model-help">
              模型来自其他非官方渠道，可能临时不可用。
              <span className="model-note">
                <button type="button" className="model-note-mark" aria-describedby="model-source-note">
                  <span aria-hidden="true">?</span>
                  <span className="sr-only">关于这些模型</span>
                </button>
                <span className="model-note-card" id="model-source-note" role="tooltip">
                  名称对应模型系列。
                  <br />
                  线路可能中断，回答质量也会起伏。
                </span>
              </span>
              <span className="model-help-rule">提交后，此视频的解读与问答固定使用同一模型。</span>
            </p>
            <div className="fold" data-open={expanded ? "true" : "false"}>
              <div className="fold-clip">
                <div className="purpose-field" id="purpose-field">
                  <label htmlFor="purpose">这次想重点了解什么？</label>
                  <textarea
                    id="purpose"
                    value={purpose}
                    rows={3}
                    tabIndex={expanded ? 0 : -1}
                    aria-hidden={expanded ? undefined : true}
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
          </form>
          <div className="entry-foot">
            <a className="sample-link" href="/example">
              先看一篇真实示例 <span className="nudge nudge-forward" aria-hidden="true">→</span>
            </a>
            <p className="entry-note">单条或单个分 P，较长视频要几分钟。</p>
          </div>
          <div className="proof">
            <p className="census">
              {readings !== undefined ? (
                <>
                  <strong>{readings.toLocaleString("zh-Hans")}</strong>
                  <span>次解读</span>
                </>
              ) : (
                <span>从一条视频开始</span>
              )}
            </p>
            <div className="thanks">
              <LikeButton count={stats?.likes} ready={ready} csrf={csrf} active={active} />
              <RepoPeek>
                <a className="star-link" href="https://github.com/junnhwan/vid-lens" target="_blank" rel="noopener noreferrer">
                  <svg viewBox="0 0 16 16" aria-hidden="true">
                    <path d="M8 1.35l1.62 3.28 3.62.53-2.62 2.55.62 3.6L8 9.62 4.76 11.3l.62-3.6L2.76 5.16l3.62-.53L8 1.35z" />
                  </svg>
                  GitHub Star
                </a>
              </RepoPeek>
            </div>
          </div>
        </div>
        <aside
          className={`entry-stage entry-art abstract-stage ${active ? "is-live" : ""}`}
          id="entry-stage"
          aria-label="流动的抽象镜片与视频切片"
        >
          <div className="stage-stack">
            <p className="lens-kicker">FRAME / TEXT / CONTEXT</p>
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
            <div className="stage-caption">
              <span>循着画面，找到理解的焦点。</span>
              <span>01 — ∞</span>
            </div>
          </div>
        </aside>
      </div>
    </section>
  )
}
