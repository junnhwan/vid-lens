export function statusName(status: string): string {
  if (status === "waiting_source") return "准备来源"
  if (status === "generating") return "处理中"
  if (status === "completed") return "已完成"
  if (status === "failed") return "未完成"
  if (status === "canceled") return "已停止"
  return "状态未知"
}

export function isRunning(status: string): boolean {
  return status === "waiting_source" || status === "generating"
}

export function displayUrl(value: string): string {
  try {
    const url = new URL(value)
    return `${url.hostname}${url.pathname}${url.search}`
  } catch {
    return value
  }
}

export function displayDate(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return "时间未知"
  return new Intl.DateTimeFormat("zh-CN", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date)
}
