import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { choosePassage } from "../contract/text"
import { useVisitor } from "./useVisitor"
import { carryStill, hideView, showView } from "../ui/motion"
import { hitsInSelection } from "../ui/selection"
import { EntryView } from "../ui/EntryView"
import { Lightbox } from "../ui/Lightbox"
import { ReadView } from "../ui/ReadView"
import { RunView } from "../ui/RunView"
import { SiteHeader, SiteFooter } from "../ui/SiteHeader"

function requestIdFrom(pathname: string): string | undefined {
  const match = pathname.match(/^\/r\/([^/]+)\/?$/)
  if (!match) return undefined
  try {
    return decodeURIComponent(match[1])
  } catch {
    return match[1]
  }
}

function usePath() {
  const [path, setPath] = useState(() => window.location.pathname)
  useEffect(() => {
    const onPop = () => setPath(window.location.pathname)
    window.addEventListener("popstate", onPop)
    return () => window.removeEventListener("popstate", onPop)
  }, [])
  function navigate(next: string) {
    const url = `${next}${window.location.search}`
    if (url === `${window.location.pathname}${window.location.search}`) {
      setPath(next)
      return
    }
    window.history.pushState(null, "", url)
    setPath(next)
  }
  return { path, navigate }
}

export function App() {
  const { path, navigate } = usePath()
  const requestId = requestIdFrom(path)
  const visitor = useVisitor(requestId, navigate)
  const inputRef = useRef<HTMLElement>(null)
  const runRef = useRef<HTMLElement>(null)
  const readRef = useRef<HTMLElement>(null)
  const shown = useRef<HTMLElement | null>(null)
  const [homeTick, setHomeTick] = useState(0)
  const [pop, setPop] = useState<{
    left: number
    top: number
    label: string
    pick: () => void
  } | null>(null)
  const attachRef = useRef(visitor.attachPassage)
  const announceRef = useRef(visitor.announce)
  attachRef.current = visitor.attachPassage
  announceRef.current = visitor.announce

  useLayoutEffect(() => {
    const input = inputRef.current
    const run = runRef.current
    const read = readRef.current
    const next = visitor.phase === "input" ? input : visitor.phase === "reading" ? read : run
    const previous = shown.current
    const origin = previous?.querySelector<HTMLElement>("[data-carry-from]")
    const start = origin?.getBoundingClientRect()
    for (const el of [input, run, read]) {
      if (!el || el === next) continue
      if (shown.current === el) hideView(el)
      else el.hidden = true
    }
    if (next && shown.current !== next) {
      showView(next)
      shown.current = next
      if (previous) {
        const heading = next.querySelector<HTMLElement>("h1")
        if (heading) {
          heading.tabIndex = -1
          heading.focus({ preventScroll: true })
        }
        window.scrollTo({ top: 0, behavior: "instant" })
      }
    }
    if (previous === run && next === read && start && next) {
      const dest = next.querySelector<HTMLElement>("[data-carry-to]")
      const fromID = origin?.querySelector("img")?.getAttribute("data-frame-id")
      const toID = dest?.querySelector("img")?.getAttribute("data-frame-id")
      if (dest && fromID && fromID === toID) carryStill(start, dest)
    }
    document.body.dataset.phase = visitor.phase === "run" ? "generating" : visitor.phase
  }, [visitor.phase])

  useEffect(() => {
    if (visitor.phase === "input") document.title = "VidLens"
    if (visitor.phase === "reading") document.title = `${visitor.article?.article.title || "文章"} · VidLens`
  }, [visitor.phase, visitor.article?.article.title])

  useEffect(() => {
    if (!homeTick || visitor.phase !== "input") return
    const field = document.getElementById("link")
    if (field instanceof HTMLInputElement) {
      field.focus()
      field.select()
    }
  }, [homeTick, visitor.phase])

  useEffect(() => {
    const article = visitor.article?.article
    function hide() {
      setPop(null)
    }
    function sync(event: Event) {
      const target = event.target
      if (target instanceof Element && target.closest(".passage-pop")) return
      window.setTimeout(() => {
        if (visitor.phase !== "reading" || !article) {
          setPop(null)
          return
        }
        const prose = document.getElementById("prose")
        if (!prose) return
        const found = hitsInSelection(prose)
        if (!found.rect || !found.hits.length) {
          setPop(null)
          return
        }
        const enriched = found.hits.flatMap((hit) => {
          const section = article.sections.find((item) => item.id === hit.sectionId)
          const block = section?.blocks.find((item) => item.id === hit.blockId)
          return block ? [{ sectionId: hit.sectionId, blockId: hit.blockId, block }] : []
        })
        const raw = found.hits.map((hit) => hit.quote).join(" ")
        const result = choosePassage(enriched, raw, visitor.selectionMax)
        const rect = found.rect
        const left = Math.max(12, Math.min(rect.left + rect.width / 2 - 36, window.innerWidth - 84))
        let top = rect.top - 38
        if (top < 64) top = rect.bottom + 8
        if (!result.ok && result.reason === "empty") {
          setPop(null)
          return
        }
        const label =
          !result.ok && result.reason === "cross"
            ? "请只选择一段"
            : !result.ok && result.reason === "long"
              ? "这段太长了"
              : !result.ok
                ? "这段对不上正文"
                : "问这段"
        setPop({
          left,
          top,
          label,
          pick: () => {
            setPop(null)
            if (!result.ok) {
              const note =
                result.reason === "cross"
                  ? "请只选择一个段落。"
                  : result.reason === "long"
                    ? "这段超过了可选长度。"
                    : "这段文字对不上文章里的那一段。"
              announceRef.current(note)
              return
            }
            document.getSelection()?.removeAllRanges()
            attachRef.current(result.passage)
          },
        })
      }, 0)
    }
    document.addEventListener("mouseup", sync)
    document.addEventListener("scroll", hide, true)
    return () => {
      document.removeEventListener("mouseup", sync)
      document.removeEventListener("scroll", hide, true)
    }
  }, [visitor.phase, visitor.article, visitor.selectionMax])

  function newVideo() {
    setHomeTick((value) => value + 1)
    if (window.location.pathname !== "/") navigate("/")
  }

  const blocked = Boolean(visitor.lost || (!visitor.booted && visitor.bootError))

  return (
    <>
      <a className="skip" href="#main">
        跳到内容
      </a>
      <SiteHeader onNew={newVideo} />
      {visitor.mockMode ? (
        <p className="development-note" role="note">
          开发预览：模拟流程，不代表真实生成。
        </p>
      ) : null}
      <main id="main">
        {visitor.notice ? (
          <p className="history-note" role="status">
            {visitor.notice}
          </p>
        ) : null}
        {blocked ? (
          <section className="view-input">
            <div className="entry">
              <div className="entry-copy">
                <h1>{visitor.lost ? "需要重新开始" : "暂时连不上"}</h1>
                <p className="purpose">{visitor.lost || visitor.bootError}</p>
                <button type="button" className="btn primary" onClick={() => window.location.reload()}>
                  重新开始
                </button>
              </div>
            </div>
          </section>
        ) : (
          <>
            <EntryView
              csrf={visitor.csrfToken}
              viewRef={inputRef}
              link={visitor.link}
              error={visitor.linkError}
              busy={visitor.submitting}
              ready={visitor.booted}
              active={visitor.phase === "input"}
              onLink={visitor.setLink}
              onSubmit={visitor.submit}
              onSample={visitor.sample}
              purpose={visitor.purpose}
              onPurpose={visitor.setPurpose}
              detailLevel={visitor.detailLevel}
              onDetailLevel={visitor.setDetailLevel}
              catalog={visitor.catalog}
              selected={visitor.selectedModel}
              onSelect={visitor.setSelectedModel}
              catalogError={visitor.catalogError}
              onReloadCatalog={() => void visitor.reloadCatalog()}
            />
            <RunView
              viewRef={runRef}
              active={visitor.phase === "run" || visitor.phase === "holding"}
              view={visitor.request}
              steps={visitor.steps}
              holding={visitor.phase === "holding"}
              reconnecting={visitor.reconnecting}
              historyOpen={visitor.historyOpen}
              articleError={visitor.articleError}
              onToggleHistory={visitor.toggleHistory}
              onStop={visitor.stop}
              onRetry={visitor.retry}
              onRead={visitor.beginReading}
              onRetryArticle={visitor.retryArticle}
              onOpenFrame={visitor.openFrames}
              onBroken={visitor.refreshFrame}
            />
            <ReadView
              csrf={visitor.csrfToken}
              preview={visitor.mockMode}
              viewRef={readRef}
              view={visitor.request}
              article={visitor.article}
              articleError={visitor.articleError}
              externalAllowed={visitor.externalAllowed}
              externalAvailable={visitor.externalAvailable}
              searchAvailable={visitor.searchAvailable}
              referenceLinks={visitor.referenceLinks}
              onExternal={visitor.setExternalAllowed}
              onReferenceLinks={visitor.setReferenceLinks}
              steps={visitor.steps}
              frames={visitor.frames}
              qaOpen={visitor.qaOpen}
              turns={visitor.turns}
              stepsFor={visitor.stepsFor}
              draft={visitor.draft}
              frameChip={visitor.frameChip}
              passageChip={visitor.passageChip}
              suggesting={visitor.suggesting}
              qaBusy={visitor.qaBusy}
              qaError={visitor.qaError}
              expanded={visitor.expanded}
              focusToken={visitor.focusToken}
              active={visitor.phase === "reading"}
              onToggleQa={visitor.toggleQa}
              onRetryArticle={visitor.retryArticle}
              onOpenFrame={visitor.openFrames}
              onAskFrame={visitor.askFrame}
              onCite={visitor.cite}
              onBroken={visitor.refreshFrame}
              onDraft={visitor.setDraft}
              onSend={visitor.send}
              onStop={visitor.stopTurn}
              onRetryTurn={visitor.retryTurn}
              onCloseQa={visitor.closeQa}
              onToggleTurn={visitor.toggleTurn}
              onSuggest={visitor.suggest}
              onRemoveFrame={visitor.removeFrame}
              onRemovePassage={visitor.removePassage}
            />
          </>
        )}
      </main>
      {visitor.phase === "input" ? <SiteFooter /> : null}
      <div id="live" className="sr-only" aria-live="polite">
        {visitor.live}
      </div>
      <Lightbox
        frames={visitor.lightbox.frames}
        index={visitor.lightbox.index}
        open={visitor.lightbox.open && !blocked}
        origin={visitor.lightbox.origin}
        canAsk={visitor.phase === "reading" || visitor.phase === "holding"}
        onClose={visitor.closeLightbox}
        onIndex={visitor.setLightboxIndex}
        onAsk={visitor.askFrame}
        onBroken={visitor.refreshFrame}
      />
      <button
        type="button"
        className="passage-pop"
        hidden={!pop || visitor.phase !== "reading"}
        style={pop ? { left: pop.left, top: pop.top } : undefined}
        onMouseDown={(event) => {
          event.preventDefault()
          pop?.pick()
        }}
      >
        {pop?.label || "问这段"}
      </button>
    </>
  )
}
