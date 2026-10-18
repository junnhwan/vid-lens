const REPO = "junnhwan/vid-lens"
const CACHE = "vidlens-repo-card"

export type RepoInfo = {
  description: string
  stars: number
  forks: number
}

export function readRepoCache(): RepoInfo | null {
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

export function loadRepo() {
  if (!pending) {
    // Session data is only for the initial display; refreshes always revalidate.
    pending = fetch(`https://api.github.com/repos/${REPO}`, {
      headers: { Accept: "application/vnd.github+json" },
      cache: "no-cache",
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
      .finally(() => {
        pending = null
      })
  }
  return pending
}

export function subscribeRepo(onUpdate: (info: RepoInfo | null) => void) {
  let live = true
  const refresh = () => {
    if (document.hidden) return
    void loadRepo().then((info) => {
      if (live) onUpdate(info)
    })
  }
  refresh()
  window.addEventListener("focus", refresh)
  document.addEventListener("visibilitychange", refresh)
  // Poll sparingly because GitHub limits unauthenticated requests.
  const timer = window.setInterval(refresh, 5 * 60_000)
  return () => {
    live = false
    window.clearInterval(timer)
    window.removeEventListener("focus", refresh)
    document.removeEventListener("visibilitychange", refresh)
  }
}
