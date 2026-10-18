import { useEffect, useState } from "react"

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

function useRepo() {
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
  return { info, failed }
}

export function StarLink() {
  const { info, failed } = useRepo()
  const count = info ? info.stars.toLocaleString("zh-Hans") : ""
  return (
    <span className="repo-peek">
      <a
        className="star-link"
        href="https://github.com/junnhwan/vid-lens"
        target="_blank"
        rel="noopener noreferrer"
        aria-label={count ? `在 GitHub 为 VidLens 加星，目前 ${count} 颗` : "在 GitHub 为 VidLens 加星"}
      >
        <span className="star-seal" aria-hidden="true">
          <svg viewBox="0 0 16 16">
            <path d="M8 1.2l1.72 3.72 4.06.46-3.04 2.78.84 4.02L8 10.22l-3.58 2.06.84-4.02L2.22 5.38l4.06-.46L8 1.2z" />
          </svg>
        </span>
        <span className="star-copy">
          <strong>给仓库加星</strong>
          <small>{count ? `${count} 颗星` : failed ? "打开 GitHub" : "读取星标"}</small>
        </span>
      </a>
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
