export interface SiteCapacity {
  scope: "video_workspace"
  total_bytes: number
  available_bytes: number
  used_bytes: number
  reserved_bytes: number
  workspace_bytes: number
  ceiling_bytes: number
  accepting: boolean
  level: "open" | "tight" | "closed"
}

export function formatDiskBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—"
  const gib = bytes / (1024 * 1024 * 1024)
  if (gib >= 100) return `${Math.round(gib)} GiB`
  if (gib >= 1) return `${trim(gib.toFixed(1))} GiB`
  const mib = bytes / (1024 * 1024)
  if (mib >= 10) return `${Math.round(mib)} MiB`
  if (mib >= 1) return `${trim(mib.toFixed(1))} MiB`
  if (bytes === 0) return "0 MiB"
  return `${Math.max(1, Math.round(bytes / 1024))} KiB`
}

export function formatDiskPercent(used: number, total: number): string {
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return "—"
  const rounded = Math.round(Math.min(100, Math.max(0, (used / total) * 100)) * 10) / 10
  return Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1)
}

export function diskRatio(part: number, total: number): number {
  if (!Number.isFinite(part) || !Number.isFinite(total) || total <= 0) return 0
  return Math.min(1, Math.max(0, part / total))
}

export function diskStatus(capacity: SiteCapacity): string {
  if (capacity.level === "closed") return "暂时不能下载"
  if (capacity.level === "tight") return "余量不多"
  return "还可以接收"
}

export function diskNote(capacity: SiteCapacity): string {
  if (capacity.level === "closed") return "暂时无法准备新素材，等待站长清理或增加可用空间。"
  if (capacity.level === "tight") return "视频专用空间余量不多，下载前会先检查容量。"
  return "视频与处理素材的专用配额，下载前预留工作空间。"
}

export function diskLabel(capacity: SiteCapacity): string {
  return `视频工作区，已用 ${formatDiskPercent(capacity.used_bytes, capacity.total_bytes)}%，剩余配额 ${formatDiskBytes(capacity.available_bytes)}，任务预留 ${formatDiskBytes(capacity.reserved_bytes)}，${diskStatus(capacity)}。${diskNote(capacity)}`
}

function trim(value: string): string {
  return value.endsWith(".0") ? value.slice(0, -2) : value
}
