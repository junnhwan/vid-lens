import { useEffect, useState } from "react"

type Theme = "system" | "light" | "dark"
function storedTheme(): Theme {
  try {
    const s = localStorage.getItem("vidlens-theme")
    return s === "light" || s === "dark" ? s : "system"
  } catch {
    return "system"
  }
}
export function ThemeControl() {
  const [theme, setTheme] = useState<Theme>(storedTheme)
  useEffect(() => {
    const query = matchMedia("(prefers-color-scheme: dark)")
    const apply = () => {
      document.documentElement.dataset.theme = theme === "system" ? (query.matches ? "dark" : "light") : theme
      document.documentElement.dataset.themePreference = theme
    }
    apply()
    query.addEventListener("change", apply)
    try {
      localStorage.setItem("vidlens-theme", theme)
    } catch {
      /* system remains usable */
    }
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
  return (
    <label className="theme-control">
      <span className="sr-only">配色主题</span>
      <select value={theme} onChange={(e) => setTheme(e.target.value as Theme)} aria-label="配色主题">
        <option value="system">跟随系统</option>
        <option value="light">浅色</option>
        <option value="dark">深色</option>
      </select>
    </label>
  )
}
export function SiteHeader({ onNew }: { onNew?: () => void }) {
  return (
    <header className="topbar">
      <div className="topbar-inner">
        <a className="wordmark" href="/" aria-label="VidLens 首页">
          <img src="/favicon.svg" width="27" height="27" alt="" />
          <span>
            VidLens<span className="wordmark-note">视频的另一种读法</span>
          </span>
        </a>
        <nav className="site-nav" aria-label="主导航">
          <a href="/docs">使用文档</a>
          <ThemeControl />
          {onNew ? (
            <button type="button" className="btn quiet" id="new-video" onClick={onNew}>
              新视频 <span aria-hidden="true">↗</span>
            </button>
          ) : (
            <a href="/" className="btn quiet">
              解读视频 ↗
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
      <a href="/docs/privacy">隐私与保留</a>
      <a href="https://github.com/junnhwan/vid-lens" target="_blank" rel="noopener noreferrer">
        开源项目 ↗
      </a>
    </footer>
  )
}
