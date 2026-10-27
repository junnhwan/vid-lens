import { useEffect, useState, type Ref } from "react"
import type { SiteCapacity } from "../contract/capacity"
import type { DetailLevel, ModelCatalog } from "../contract/types"
import { HttpApi } from "../api/http"
import { DiskMeter } from "./DiskMeter"
import { LikeButton } from "./LikeButton"
import { ChoiceMenu } from "./ChoiceMenu"
import { homeModelOption } from "./homeModel"
import { Lens } from "./Lens"
import { LectureShelf } from "./LectureShelf"
import type { Lecture } from "./lectures"
import { RecentReadings } from "./RecentReadings"
import { ServiceQueue } from "./ServiceQueue"
import { StarLink } from "./RepoPeek"
import { useRise } from "./countUp"
import { prefersReduced } from "./motion"

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
  detailLevel,
  onDetailLevel,
  regenerate,
  onRegenerate,
  catalog,
  selected,
  onSelect,
  catalogError,
  onReloadCatalog,
  active,
  onOpenRecord,
  onHistory,
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
  detailLevel: DetailLevel
  onDetailLevel: (v: DetailLevel) => void
  regenerate: boolean
  onRegenerate: (v: boolean) => void
  catalog: ModelCatalog | null
  selected: string
  onSelect: (v: string) => void
  catalogError: string
  onReloadCatalog: () => void
  onOpenRecord: (id: string) => void
  onHistory: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [stats, setStats] = useState<{
    completed_readings: number
    likes: number
  } | null>(null)
  const [capacity, setCapacity] = useState<SiteCapacity | null>(null)
  const [capacityState, setCapacityState] = useState<"loading" | "ready" | "error">("loading")
  useEffect(() => {
    let live = true
    void new HttpApi()
      .stats()
      .then((v) => {
        if (live) setStats(v)
      })
      .catch(() => {})
    const loadCapacity = () => {
      void new HttpApi()
        .capacity()
        .then((v) => {
          if (!live) return
          setCapacity(v)
          setCapacityState("ready")
        })
        .catch(() => {
          if (!live) return
          setCapacityState((current) => (current === "ready" ? "ready" : "error"))
        })
    }
    loadCapacity()
    const timer = window.setInterval(loadCapacity, 20000)
    return () => {
      live = false
      window.clearInterval(timer)
    }
  }, [])
  const model = catalog?.models.find((m) => m.id === selected)
  const readings = useRise(stats?.completed_readings, active)
  function fillLecture(item: Lecture) {
    onLink(item.url)
    onPurpose(item.purpose)
    setExpanded(true)
    window.requestAnimationFrame(() => {
      const form = document.getElementById("link")?.closest("form")
      document.getElementById("link")?.focus({ preventScroll: true })
      form?.scrollIntoView({ behavior: prefersReduced() ? "auto" : "smooth", block: "center" })
    })
  }
  return (
    <section className="view-input" ref={viewRef}>
      <div className="entry editorial-entry">
        <div className="entry-copy">
          <p className="eyebrow">
            <span /> A CLOSER LOOK AT VIDEO
          </p>
          <h1>
            A video,
            <br />
            <em>
              read <span>closely.</span>
            </em>
          </h1>
          <p className="entry-deck">循着原视频的文字与画面整理重点，留下可回看的出处，也留一点继续追问的空间。</p>
          <p className="entry-fit" id="video-fit">
            <strong>主要面向技术讲解、科普、课程与教程。</strong>
            <span>音乐、风景、纯剪辑等缺少讲述的视频，可整理的信息较少。</span>
            <span className="entry-caution">无关内容，或恶意提交的超大视频，会被直接清理。</span>
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
                aria-describedby="video-fit"
                value={link}
                aria-invalid={error ? true : undefined}
                onChange={(e) => onLink(e.target.value)}
              />
              <button type="submit" className="btn primary" disabled={busy || !ready || !model} aria-describedby="wait-expect">
                {busy ? "正在提交" : "开始解读"}
                <span aria-hidden="true" className="nudge">
                  {" "}
                  ↗
                </span>
              </button>
            </div>
            <p className="entry-expect" id="wait-expect">
              较长视频要几分钟。
            </p>
            <div className="form-options">
              <ChoiceMenu
                label="本次使用的模型"
                value={selected}
                disabled={busy || !catalog?.models.length}
                placeholder={catalog ? "暂无可用模型" : "正在读取模型"}
                unavailable={selected && !model ? "原模型已不可用，请重新选择" : undefined}
                options={(catalog?.models ?? []).map(homeModelOption)}
                onChange={onSelect}
              />
              <ChoiceMenu
                label="解读详细程度"
                placeholder="标准"
                value={detailLevel}
                disabled={busy}
                options={[
                  { id: "concise", label: "精简", hint: "掌握全片主线，合并重复解释" },
                  { id: "standard", label: "标准", hint: "理解主要内容、原因和关键步骤" },
                  { id: "detailed", label: "详细", hint: "保留更多有依据的参数、例子和过程" },
                ]}
                onChange={(value) => onDetailLevel(value as DetailLevel)}
              />
              <ChoiceMenu
                label="文章生成方式"
                placeholder="优先复用文章"
                value={regenerate ? "fresh" : "reuse"}
                disabled={busy}
                options={[
                  { id: "reuse", label: "优先复用文章", hint: "有匹配的成稿时直接打开，也可等待共享解读" },
                  { id: "fresh", label: "重新生成文章", hint: "独立生成新文章，视频和转写仍可复用" },
                ]}
                onChange={(value) => onRegenerate(value === "fresh")}
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
              <span className="model-note model-note-end">
                <button type="button" className="model-note-mark" aria-describedby="model-source-note">
                  <span aria-hidden="true">?</span>
                  <span className="sr-only">关于模型和档位</span>
                </button>
                <span className="model-note-card" id="model-source-note" role="tooltip">
                  三档都覆盖全片主要内容，详细档通常需要更多处理。
                  <br />
                  模型来自其他非官方渠道，可能临时不可用。名称对应模型系列，线路可能中断，回答质量也会起伏。
                  <br />
                  提交后，此视频的解读与问答固定使用同一模型。
                </span>
              </span>
            </div>
            {regenerate ? <p className="entry-expect">本次会重新调用模型生成文章，已有的视频和转写仍可复用。</p> : null}
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
          <ServiceQueue active={active} />
          <RecentReadings active={active} onOpen={onOpenRecord} onHistory={onHistory} />
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
            <div className="stage-rail">
              <div className="stage-rail-head">
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
                <a className="sample-link" href="/example">
                  查看真实示例 <span className="nudge nudge-forward" aria-hidden="true">→</span>
                </a>
              </div>
              <div className="thanks">
                <LikeButton count={stats?.likes} ready={ready} csrf={csrf} active={active} />
                <StarLink />
              </div>
              <p className="entry-note">单条或单个分 P。</p>
            </div>
            <DiskMeter capacity={capacity} state={capacityState} />
          </div>
        </aside>
      </div>
      <LectureShelf link={link} onPick={fillLecture} />
    </section>
  )
}
