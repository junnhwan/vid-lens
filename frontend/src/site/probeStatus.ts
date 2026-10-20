export interface ProbeClock {
  status: string
  mode: string
  created_at: string
  started_at?: string
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
  const start = probe.started_at || probe.created_at
  if (isOpenProbe(probe)) return elapsedText(start, now)
  if (!probe.updated_at) return ""
  const end = Date.parse(probe.updated_at)
  if (!Number.isFinite(end)) return ""
  return elapsedText(start, end)
}

export function liveStatusText(probe: ProbeClock, now: number) {
  const name = probe.status === "queued" ? "准备检查" : "正在检查"
  const elapsed = probeTiming(probe, now)
  return elapsed ? `${name} · ${elapsed}` : name
}

export function probeFailureText(probe: { failure_code?: string; status: string; submissions: number }) {
  const messages: Record<string, string> = {
    PROVIDER_ACCOUNT_COOLDOWN: "供应商的可用账号正在冷却，请恢复后再检查。",
    PROVIDER_RATE_LIMITED: "供应商限流，请稍后再检查。",
    PROVIDER_MODEL_UNAVAILABLE: "供应商当前没有这个模型的可用通道，请检查模型 ID 或线路状态。",
    PROVIDER_AUTH_FAILED: "供应商拒绝了认证或访问权限，请检查密钥和模型权限。",
    PROVIDER_REQUEST_REJECTED: "供应商拒绝了检测请求，请检查该线路支持的参数与工具协议。",
    PROVIDER_UNAVAILABLE: "供应商服务暂不可用，请恢复后再检查。",
    PROVIDER_CONNECTION_FAILED: "连接中断，未取得完整响应。",
    PROVIDER_OUTCOME_UNKNOWN: "未能确认上游是否完成了请求。",
    PROBE_EXECUTION_TIMEOUT: "检测调用超过时限。",
    PROBE_QUEUE_TIMEOUT: "检查未能及时启动，尚未验证模型能力。",
    PROBE_RESOURCE_BUSY: "本机检测名额或内存暂不可用，尚未验证模型能力。",
    PROBE_INTERRUPTED: "检测执行被中断。",
    CAPABILITY_FIXTURE_UNAVAILABLE: "服务器缺少检测图片，需要修复检测素材。",
    CAPABILITY_OBSERVATION_MISMATCH: "返回的截图字段与图片不符，读图检查未通过。",
    CAPABILITY_CHECK_INCOMPLETE: "模型未完成读图和结构化提交，检查未通过。",
    CAPABILITY_IMAGE_NOT_USED: "模型在取得图片前提交了结果，检查未通过。",
    CAPABILITY_MODEL_CALL_FAILED: "模型请求未能完成；记录中没有更细的失败分类。",
  }
  const text = messages[probe.failure_code || ""] || ""
  return text && probe.status === "unknown" && probe.submissions > 0
    ? `${text} 请求可能已被上游接收，不会自动重发。`
    : text
}

export function probeStatusText(probe: { failure_code?: string; status: string }) {
  if (probe.status === "unknown") return "结果未知"
  if (probe.failure_code?.startsWith("PROVIDER_")) return "供应商调用未完成"
  if (probe.failure_code?.startsWith("PROBE_")) return "检测未完成"
  return ({ queued: "准备检查", running: "正在检查", succeeded: "检查通过", failed: "检查失败" } as Record<string, string>)[probe.status] || probe.status
}
