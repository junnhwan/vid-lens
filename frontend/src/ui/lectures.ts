export type Lecture = {
  id: string
  kind: string
  title: string
  channel: string
  duration: string
  hint: string
  url: string
  purpose: string
}

export const lectures: Lecture[] = [
  {
    id: "attention",
    kind: "动画",
    title: "Attention 一步步怎么算",
    channel: "3Blue1Brown",
    duration: "约 26 分钟",
    hint: "向量和矩阵都画在动画里",
    url: "https://www.youtube.com/watch?v=eMlx5fFNoYc",
    purpose: "请顺着动画里的向量和矩阵，解释 Attention 在算什么，并指出关键画面出现的时间。",
  },
  {
    id: "solid",
    kind: "代码",
    title: "SOLID 在 Python 里长什么样",
    channel: "ArjanCodes",
    duration: "约 21 分钟",
    hint: "同一题的三版代码都在屏幕上",
    url: "https://www.youtube.com/watch?v=uxwjXLjJOoM",
    purpose: "请对比视频里前后几版 Python 代码的设计差异，说明为什么要重构，以及各自的取舍。",
  },
]

function videoKey(value: string) {
  const text = value.trim()
  if (!text) return ""
  try {
    const url = new URL(text)
    const host = url.hostname.replace(/^www\./, "")
    if (host === "youtu.be") return url.pathname.split("/").filter(Boolean)[0] ?? ""
    if (host === "youtube.com" || host === "m.youtube.com" || host === "music.youtube.com") return url.searchParams.get("v") ?? ""
  } catch {
    return text
  }
  return text
}

export function sameVideo(link: string, url: string) {
  const key = videoKey(link)
  return key !== "" && key === videoKey(url)
}
