export interface SiteCapacity {
  total_bytes: number
  available_bytes: number
  used_bytes: number
  reserve_bytes: number
  ceiling_bytes: number
  stop_percent: number
  accepting: boolean
  level: "open" | "tight" | "closed"
  blocked_by?: "disk" | "media" | ""
}

export function formatDiskBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—"
  const gib = bytes / (1024 * 1024 * 1024)
  if (gib >= 100) return `${Math.round(gib)} GB`
  if (gib >= 1) return `${trim(gib.toFixed(1))} GB`
  const mib = bytes / (1024 * 1024)
  if (mib >= 10) return `${Math.round(mib)} MB`
  if (mib >= 1) return `${trim(mib.toFixed(1))} MB`
  return `${Math.max(0, Math.round(bytes / 1024))} KB`
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
  if (capacity.level === "closed" && capacity.blocked_by === "media") return "临时文件已经占满，新的下载会先停下。"
  if (capacity.level === "closed") return "余量不够留出工作区，新的下载会先停下。"
  if (capacity.level === "tight") return "再放一条长视频，可能会碰到停线。"
  return "下载前会留出工作区。过了刻度，新视频会先停下。"
}

export function diskLabel(capacity: SiteCapacity): string {
  return `服务器磁盘，已用 ${formatDiskPercent(capacity.used_bytes, capacity.total_bytes)}%，可用 ${formatDiskBytes(capacity.available_bytes)}，${diskStatus(capacity)}。${diskNote(capacity)}`
}

function trim(value: string): string {
  return value.endsWith(".0") ? value.slice(0, -2) : value
}
