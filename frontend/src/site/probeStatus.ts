export interface ProbeClock {
  status: string
  mode: string
  created_at: string
  updated_at?: string
}

export function isOpenProbe(probe: { status: string }) {
  return probe.status === "queued" || probe.status === "running"
}

export function openProbe<T extends { status: string; mode: string }>(items: T[] | undefined, mode?: string) {
  return items?.find((probe) => isOpenProbe(probe) && (!mode || probe.mode === mode))
}

export function modelActivity<T extends { status: string; mode: string }>(items: T[] | undefined) {
  return openProbe(items, "capabilities") ?? openProbe(items, "cache")
}

export function elapsedText(start: string, end: number) {
  const from = Date.parse(start)
  if (!Number.isFinite(from)) return ""
  const seconds = Math.max(0, Math.floor((end - from) / 1000))
  const minutes = Math.floor(seconds / 60)
  const rest = seconds % 60
  if (minutes >= 60) {
    const hours = Math.floor(minutes / 60)
    return `${hours} 小时 ${minutes % 60} 分 ${String(rest).padStart(2, "0")} 秒`
  }
  if (minutes > 0) return `${minutes} 分 ${String(rest).padStart(2, "0")} 秒`
  return `${rest} 秒`
}

export function probeTiming(probe: ProbeClock, now: number) {
  if (isOpenProbe(probe)) return elapsedText(probe.created_at, now)
  if (!probe.updated_at) return ""
  const end = Date.parse(probe.updated_at)
  if (!Number.isFinite(end)) return ""
  return elapsedText(probe.created_at, end)
}

export function liveStatusText(probe: ProbeClock, now: number) {
  const name = probe.status === "queued" ? "排队中" : "正在检查"
  const elapsed = elapsedText(probe.created_at, now)
  return elapsed ? `${name} · ${elapsed}` : name
}
