import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const CACHE = "vidlens-repo-card"
const oldInfo = { description: "VidLens", stars: 51, forks: 6 }
const latestInfo = { description: "VidLens", stars: 56, forks: 7 }

function repoResponse(stars: number, forks: number) {
  return new Response(JSON.stringify({ description: "VidLens", stargazers_count: stars, forks_count: forks }))
}

beforeEach(() => {
  vi.resetModules()
  const entries = new Map<string, string>()
  vi.stubGlobal("sessionStorage", {
    getItem: (key: string) => entries.get(key) ?? null,
    setItem: (key: string, value: string) => entries.set(key, value),
    removeItem: (key: string) => entries.delete(key),
  })
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe("GitHub repository updates", () => {
  it("revalidates a cached 51 stars and updates both stars and forks", async () => {
    sessionStorage.setItem(CACHE, JSON.stringify(oldInfo))
    const fetchRepo = vi.fn().mockResolvedValue(repoResponse(56, 7))
    vi.stubGlobal("fetch", fetchRepo)
    const { loadRepo, readRepoCache } = await import("./github")

    expect(readRepoCache()).toEqual(oldInfo)
    expect(await loadRepo()).toEqual(latestInfo)
    expect(readRepoCache()).toEqual(latestInfo)
    expect(fetchRepo).toHaveBeenCalledWith("https://api.github.com/repos/junnhwan/vid-lens", {
      headers: { Accept: "application/vnd.github+json" },
      cache: "no-cache",
    })
  })

  it("shares a request while it is in flight, then permits a later refresh", async () => {
    const fetchRepo = vi.fn()
      .mockResolvedValueOnce(repoResponse(51, 6))
      .mockResolvedValueOnce(repoResponse(56, 7))
    vi.stubGlobal("fetch", fetchRepo)
    const { loadRepo } = await import("./github")

    const first = loadRepo()
    expect(loadRepo()).toBe(first)
    expect(await first).toEqual(oldInfo)
    expect(await loadRepo()).toEqual(latestInfo)
    expect(fetchRepo).toHaveBeenCalledTimes(2)
  })

  it("can refresh again when session storage is unavailable", async () => {
    vi.stubGlobal("sessionStorage", {
      getItem: () => { throw new Error("unavailable") },
      setItem: () => { throw new Error("unavailable") },
    })
    const fetchRepo = vi.fn()
      .mockResolvedValueOnce(repoResponse(51, 6))
      .mockResolvedValueOnce(repoResponse(56, 7))
    vi.stubGlobal("fetch", fetchRepo)
    const { loadRepo } = await import("./github")

    expect(await loadRepo()).toEqual(oldInfo)
    expect(await loadRepo()).toEqual(latestInfo)
    expect(fetchRepo).toHaveBeenCalledTimes(2)
  })

  it("retries after a failed request instead of keeping the failed promise", async () => {
    const fetchRepo = vi.fn()
      .mockRejectedValueOnce(new Error("network unavailable"))
      .mockResolvedValueOnce(repoResponse(56, 7))
    vi.stubGlobal("fetch", fetchRepo)
    const { loadRepo } = await import("./github")

    expect(await loadRepo()).toBeNull()
    expect(await loadRepo()).toEqual(latestInfo)
    expect(fetchRepo).toHaveBeenCalledTimes(2)
  })

  it("retains the last successful counts during a rate limit, then retries", async () => {
    sessionStorage.setItem(CACHE, JSON.stringify(oldInfo))
    vi.stubGlobal("fetch", vi.fn()
      .mockResolvedValueOnce(new Response("rate limited", { status: 403 }))
      .mockResolvedValueOnce(repoResponse(56, 7)))
    const { loadRepo, readRepoCache } = await import("./github")

    expect(await loadRepo()).toBeNull()
    expect(readRepoCache()).toEqual(oldInfo)
    expect(await loadRepo()).toEqual(latestInfo)
  })

  it("refreshes on return to the page and every five minutes while visible", async () => {
    vi.useFakeTimers()
    const page = Object.assign(new EventTarget(), { hidden: false })
    const browser = Object.assign(new EventTarget(), { setInterval, clearInterval })
    vi.stubGlobal("document", page)
    vi.stubGlobal("window", browser)
    const fetchRepo = vi.fn()
      .mockResolvedValueOnce(repoResponse(51, 6))
      .mockResolvedValueOnce(repoResponse(56, 7))
      .mockResolvedValueOnce(repoResponse(57, 7))
      .mockResolvedValueOnce(repoResponse(58, 7))
    vi.stubGlobal("fetch", fetchRepo)
    const { subscribeRepo } = await import("./github")
    const onUpdate = vi.fn()
    const unsubscribe = subscribeRepo(onUpdate)

    await vi.advanceTimersByTimeAsync(0)
    expect(onUpdate).toHaveBeenLastCalledWith(oldInfo)
    browser.dispatchEvent(new Event("focus"))
    await vi.advanceTimersByTimeAsync(0)
    expect(onUpdate).toHaveBeenLastCalledWith(latestInfo)

    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(onUpdate).toHaveBeenLastCalledWith({ ...latestInfo, stars: 57 })
    page.hidden = true
    page.dispatchEvent(new Event("visibilitychange"))
    browser.dispatchEvent(new Event("focus"))
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(fetchRepo).toHaveBeenCalledTimes(3)

    page.hidden = false
    page.dispatchEvent(new Event("visibilitychange"))
    await vi.advanceTimersByTimeAsync(0)
    expect(onUpdate).toHaveBeenLastCalledWith({ ...latestInfo, stars: 58 })
    unsubscribe()
  })

  it("stops listeners, polling and pending callbacks when the view is left", async () => {
    vi.useFakeTimers()
    const page = Object.assign(new EventTarget(), { hidden: false })
    const browser = Object.assign(new EventTarget(), { setInterval, clearInterval })
    vi.stubGlobal("document", page)
    vi.stubGlobal("window", browser)
    let finish!: (response: Response) => void
    const fetchRepo = vi.fn().mockReturnValue(new Promise<Response>((resolve) => { finish = resolve }))
    vi.stubGlobal("fetch", fetchRepo)
    const { subscribeRepo } = await import("./github")
    const onUpdate = vi.fn()
    const unsubscribe = subscribeRepo(onUpdate)
    unsubscribe()
    finish(repoResponse(56, 7))
    await vi.advanceTimersByTimeAsync(0)

    browser.dispatchEvent(new Event("focus"))
    page.dispatchEvent(new Event("visibilitychange"))
    await vi.advanceTimersByTimeAsync(5 * 60_000)
    expect(onUpdate).not.toHaveBeenCalled()
    expect(fetchRepo).toHaveBeenCalledTimes(1)
  })
})
