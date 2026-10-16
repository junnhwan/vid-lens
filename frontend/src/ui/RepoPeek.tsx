import { useEffect, useState, type ReactNode } from "react"

const REPO = "junnhwan/vid-lens"
const CACHE = "vidlens-repo-card"

type RepoInfo = {
  description: string
  stars: number
  forks: number
}

function readCache(): RepoInfo | null {
  try {
    const raw = sessionStorage.getItem(CACHE)
    if (!raw) return null
    const parsed = JSON.parse(raw) as RepoInfo
    if (typeof parsed.stars !== "number" || typeof parsed.forks !== "number") return null
    return { description: parsed.description || "", stars: parsed.stars, forks: parsed.forks }
  } catch {
    return null
  }
}

let pending: Promise<RepoInfo | null> | null = null

function loadRepo() {
  const cached = readCache()
  if (cached) return Promise.resolve(cached)
  if (!pending) {
    pending = fetch(`https://api.github.com/repos/${REPO}`, {
      headers: { Accept: "application/vnd.github+json" },
    })
      .then((response) => {
        if (!response.ok) throw new Error(String(response.status))
        return response.json() as Promise<{ description?: string; stargazers_count?: number; forks_count?: number }>
      })
      .then((body) => {
        const info: RepoInfo = {
          description: body.description?.trim() || "",
          stars: body.stargazers_count ?? 0,
          forks: body.forks_count ?? 0,
        }
        try {
          sessionStorage.setItem(CACHE, JSON.stringify(info))
        } catch {
          /* private mode */
        }
        return info
      })
      .catch(() => null)
  }
  return pending
}

export function RepoPeek({ children }: { children: ReactNode }) {
  const [info, setInfo] = useState<RepoInfo | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let live = true
    void loadRepo().then((next) => {
      if (!live) return
      if (next) setInfo(next)
      else setFailed(true)
    })
    return () => {
      live = false
    }
  }, [])
  return (
    <span className="repo-peek">
      {children}
      <span className="repo-card" aria-hidden="true">
        <span className="repo-kicker">GitHub</span>
        <strong>junnhwan/vid-lens</strong>
        <span className="repo-desc">
          {info ? info.description || "这个仓库还没有简介。" : failed ? "仓库动态暂时读不到。" : "正在读取仓库…"}
        </span>
        {info ? (
          <span className="repo-stats">
            <span>★ {info.stars.toLocaleString("zh-Hans")}</span>
            <span>fork {info.forks.toLocaleString("zh-Hans")}</span>
          </span>
        ) : null}
      </span>
    </span>
  )
}
