import { useEffect, useState } from "react"
import { readRepoCache, subscribeRepo } from "../api/github"

function useRepo() {
  const [info, setInfo] = useState(readRepoCache)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    return subscribeRepo((next) => {
      if (next) setInfo(next)
      setFailed(!next)
    })
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
          <small>{count ? `${count} 颗星${failed ? " · 待更新" : ""}` : failed ? "打开 GitHub" : "读取星标"}</small>
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
