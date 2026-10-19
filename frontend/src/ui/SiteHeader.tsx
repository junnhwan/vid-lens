import { useEffect, useState } from "react"
import { flushSync } from "react-dom"

type Theme = "system" | "light" | "dark"
function storedTheme(): Theme {
  try {
    const s = localStorage.getItem("vidlens-theme")
    return s === "light" || s === "dark" ? s : "system"
  } catch {
    return "system"
  }
}
function resolveTheme(theme: Theme) {
  if (theme !== "system") return theme
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light"
}
function commitTheme(theme: Theme) {
  document.documentElement.dataset.theme = resolveTheme(theme)
  document.documentElement.dataset.themePreference = theme
  try {
    localStorage.setItem("vidlens-theme", theme)
  } catch {
    /* preference stays in memory */
  }
}
export function ThemeControl() {
  const [theme, setTheme] = useState<Theme>(storedTheme)
  useEffect(() => {
    const query = matchMedia("(prefers-color-scheme: dark)")
    const apply = () => commitTheme(theme)
    apply()
    query.addEventListener("change", apply)
    return () => query.removeEventListener("change", apply)
  }, [theme])
  useEffect(() => {
    const update = () => {
      document.body.dataset.pageHidden = String(document.hidden)
    }
    update()
    document.addEventListener("visibilitychange", update)
    return () => document.removeEventListener("visibilitychange", update)
  }, [])
  function choose(next: Theme, origin: HTMLButtonElement) {
    if (next === theme) return
    const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches
    const rect = origin.getBoundingClientRect()
    const x = rect.left + rect.width / 2
    const y = rect.top + rect.height / 2
    const reveal = !reduced && resolveTheme(next) !== document.documentElement.dataset.theme
    const apply = () => {
      flushSync(() => setTheme(next))
      commitTheme(next)
    }
    const start = (document as Document & { startViewTransition?: (cb: () => void) => void }).startViewTransition
    if (!reveal || !start) {
      apply()
      return
    }
    const radius = Math.hypot(Math.max(x, innerWidth - x), Math.max(y, innerHeight - y))
    document.documentElement.style.setProperty("--theme-x", `${x}px`)
    document.documentElement.style.setProperty("--theme-y", `${y}px`)
    document.documentElement.style.setProperty("--theme-r", `${radius}px`)
    start.call(document, apply)
  }
  const options: { id: Theme; label: string; icon: "system" | "sun" | "moon" }[] = [
    { id: "system", label: "跟随系统", icon: "system" },
    { id: "light", label: "浅色", icon: "sun" },
    { id: "dark", label: "深色", icon: "moon" },
  ]
  return (
    <div className="theme-switch" role="group" aria-label="配色主题">
      {options.map((option) => (
        <button
          key={option.id}
          type="button"
          className="theme-switch-item"
          aria-pressed={theme === option.id}
          aria-label={option.label}
          title={option.label}
          onClick={(event) => choose(option.id, event.currentTarget)}
        >
          <ThemeIcon name={option.icon} />
        </button>
      ))}
    </div>
  )
}
function ThemeIcon({ name }: { name: "system" | "sun" | "moon" }) {
  if (name === "sun") {
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="12" cy="12" r="3.4" />
        <path d="M12 3.2v2.1M12 18.7v2.1M3.2 12h2.1M18.7 12h2.1M5.8 5.8l1.5 1.5M16.7 16.7l1.5 1.5M18.2 5.8l-1.5 1.5M7.3 16.7 5.8 18.2" />
      </svg>
    )
  }
  if (name === "moon") {
    return (
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <path d="M15.6 14.8A6.1 6.1 0 0 1 9.2 6.4 6.4 6.4 0 1 0 15.6 14.8z" />
      </svg>
    )
  }
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <rect x="3.5" y="4.5" width="17" height="11.5" rx="1.6" />
      <path d="M9 19.8h6M12 16v3.8" />
    </svg>
  )
}
function Wordmark() {
  return (
    <svg className="wordmark-art" viewBox="0 0 228 42" aria-hidden="true">
      <path className="wordmark-letter" d="M3 7.5 16.2 34h3.2L33 7.5" />
      <path className="wordmark-letter" d="M46 16.5V34" />
      <g className="wordmark-iris">
        <circle cx="46" cy="8.2" r="4.3" />
        <circle cx="46" cy="8.2" r="1.35" className="wordmark-pupil" />
        <path d="M42.2 8.2h7.6" />
      </g>
      <path className="wordmark-letter" d="M78 7.5V34" />
      <path className="wordmark-letter" d="M78 20.2c0-4.8-3.8-7.6-9.2-7.6-6.6 0-10.6 4.2-10.6 10.2s4 10.4 10.6 10.4c5.4 0 9.2-2.6 9.2-7" />
      <path className="wordmark-letter" d="M96 7.5V34h20" />
      <path className="wordmark-letter" d="M136.5 23.2h-16" />
      <path className="wordmark-letter" d="M136.8 23.2c.4-6.4-3.6-10.4-10.2-10.4-7.4 0-11.6 4.8-11.6 11.2 0 6.8 4.4 11.2 11.4 11.2 5.6 0 9.4-2.6 10.6-6.6" />
      <path className="wordmark-letter" d="M152 34V17.2" />
      <path className="wordmark-letter" d="M152 21c0-4.6 3.2-7.2 8-7.2 5.6 0 9 3.4 9 8.2V34" />
      <path className="wordmark-letter" d="M196 16.2c-3.6-3.2-10.6-3.4-13.2.6-2.6 4 1.2 6 5.4 7.4 5.2 1.6 8.2 3.4 7.2 7.4-1.2 4.6-7.6 6.2-12.6 3.4" />
      <path className="wordmark-swash" d="M4 38.5h206" />
    </svg>
  )
}
export function SiteHeader({ onNew, onHistory }: { onNew?: () => void; onHistory?: () => void }) {
  return (
    <header className="topbar">
      <div className="topbar-inner">
        <a className="wordmark" href="/" aria-label="VidLens 首页">
          <Wordmark />
          <span className="wordmark-note">视频的另一种读法</span>
        </a>
        <nav className="site-nav" aria-label="主导航">
          <a href="/docs">使用文档</a>
          {onHistory ? (
            <a
              href="/history"
              aria-current={location.pathname === "/history" ? "page" : undefined}
              onClick={(event) => {
                event.preventDefault()
                onHistory()
              }}
            >
              处理记录
            </a>
          ) : null}
          <ThemeControl />
          {onNew ? (
            <button type="button" className="btn nav-cta" id="new-video" onClick={onNew}>
              新视频
            </button>
          ) : (
            <a href="/" className="btn nav-cta">
              解读视频
            </a>
          )}
        </nav>
      </div>
    </header>
  )
}
export function SiteFooter() {
  return (
    <footer className="site-footer">
      <span>循着文字与画面，读懂视频。</span>
      <a href="/announcements">公告</a>
      <a href="/docs/privacy">隐私与保留</a>
      <a href="https://github.com/junnhwan/vid-lens" target="_blank" rel="noopener noreferrer">
        开源项目 <span className="nudge" aria-hidden="true">↗</span>
      </a>
    </footer>
  )
}
