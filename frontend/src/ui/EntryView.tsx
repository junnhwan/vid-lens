import { useEffect, useRef, type Ref } from "react"
import { Lens } from "./Lens"
import { LikeButton } from "./LikeButton"
import { prefersReduced } from "./motion"

const visitors = 1286

function formatCount(value: number) {
  return value.toLocaleString("zh-Hans")
}

export function EntryView({
  link,
  error,
  busy,
  ready = true,
  mock,
  active,
  viewRef,
  onLink,
  onSubmit,
  onSample,
}: {
  link: string
  error: string
  busy: boolean
  ready?: boolean
  mock: boolean
  active: boolean
  viewRef?: Ref<HTMLElement>
  onLink: (value: string) => void
  onSubmit: () => void
  onSample: () => void
}) {
  const stageRef = useRef<HTMLElement>(null)
  const countRef = useRef<HTMLElement>(null)
  const played = useRef(false)

  useEffect(() => {
    if (!active) return
    const stage = stageRef.current
    if (!stage) return
    stage.classList.remove("is-live")
    void stage.offsetWidth
    stage.classList.add("is-live")
    const count = countRef.current
    if (!count || played.current) return
    played.current = true
    if (prefersReduced()) {
      count.textContent = formatCount(visitors)
      return
    }
    const from = Math.max(0, visitors - 120)
    const start = performance.now()
    const step = (now: number) => {
      const t = Math.min(1, (now - start) / 880)
      const eased = 1 - (1 - t) ** 3
      count.textContent = formatCount(Math.round(from + (visitors - from) * eased))
      if (t < 1) requestAnimationFrame(step)
    }
    requestAnimationFrame(step)
  }, [active])

  return (
    <section className="view-input" ref={viewRef}>
      <div className="entry">
        <div className="entry-copy">
          <h1>贴上视频链接</h1>
          <form
            className="link-form"
            onSubmit={(event) => {
              event.preventDefault()
              onSubmit()
            }}
          >
            <label className="sr-only" htmlFor="link">视频链接</label>
            <input
              id="link"
              name="link"
              type="text"
              inputMode="url"
              autoComplete="off"
              spellCheck={false}
              placeholder="https://www.bilibili.com/video/…"
              value={link}
              aria-invalid={error ? true : undefined}
              onChange={(event) => onLink(event.target.value)}
            />
            <button type="submit" className="btn primary" disabled={busy || !ready}>{busy ? "正在提交" : "开始理解"}</button>
          </form>
          <p className="form-error" hidden={!error}>{error}</p>
          <div className="input-side">
            <button type="button" className="btn quiet" onClick={onSample}>使用示例视频</button>
            <p className="fine">{mock ? "示例只在本地预览里走模拟过程，不会打开链接。" : "B 站单个分 P，或 YouTube 单条视频。"}</p>
          </div>
          <div className="proof">
            <p className="census">已经有 <strong ref={countRef}>{formatCount(visitors)}</strong> 人用它看完一条长视频</p>
            <div className="thanks">
              <LikeButton />
              <a className="star-link" href="https://github.com/junnhwan/vid-lens" target="_blank" rel="noopener noreferrer">
                <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 1.4l1.7 3.5 3.9.6-2.8 2.7.7 3.8L8 10.2 4.5 12l.7-3.8L2.4 5.5l3.9-.6z" /></svg>
                <span>在 GitHub 上 Star</span>
              </a>
            </div>
          </div>
        </div>
        <aside className="entry-stage" id="entry-stage" ref={stageRef}>
          <Lens />
        </aside>
      </div>
    </section>
  )
}
