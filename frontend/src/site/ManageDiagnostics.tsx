import { useRise } from "../ui/countUp"
import { usagePageSize } from "./manageCatalog"
interface Metric {
  value: number | null
  status: string
}

function metric(m?: Metric) {
  if (!m || m.status === "unknown") return "未知"
  if (m.status !== "reported") return m.status === "null" ? "空值" : "异常"
  return (m.value ?? 0).toLocaleString("zh-Hans")
}

function metricKnown(m?: Metric) {
  return m?.status === "reported"
}

function duration(ms: number | null | undefined) {
  if (ms === null || ms === undefined) return "未知"
  if (ms < 1000) return `${ms}ms`
  const seconds = ms / 1000
  return `${seconds >= 10 ? seconds.toFixed(0) : seconds.toFixed(1)}s`
}

function stamp(iso: string) {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return date.toLocaleString("sv-SE", { timeZone: "Asia/Shanghai", hour12: false })
}

function pageWindow(current: number, total: number) {
  const wanted = [1, total, current - 1, current, current + 1].filter((n, i, all) => n >= 1 && n <= total && all.indexOf(n) === i).sort((a, b) => a - b)
  const items: (number | "gap")[] = []
  let previous = 0
  for (const n of wanted) {
    if (previous && n - previous > 1) items.push("gap")
    items.push(n)
    previous = n
  }
  return items
}

function RisingFigure({ value, label }: { value?: number; label: string }) {
  const shown = useRise(value, value !== undefined)
  return (
    <div>
      <span>{label}</span>
      <strong>{shown === undefined ? "—" : shown.toLocaleString("zh-Hans")}</strong>
    </div>
  )
}

export function Diagnostics({
  data,
  offset,
  onPage,
  busy,
  spinning,
}: {
  data: Record<string, unknown> | null
  offset: number
  onPage: (offset: number) => void
  busy: boolean
  spinning: boolean
}) {
  const summary = data?.summary as Record<string, number> | undefined
  const recent = (data?.recent || []) as {
    submission?: number
    created_at: string
    display_name: string
    provider_name?: string | null
    reporting: {
      input: Metric
      output: Metric
      cache_read: Metric
      cache_write: Metric
      stream_status: string
      first_response_ms: number | null
      first_meaningful_delta_ms?: number | null
      response_finished_at?: string | null
      total_ms: number
      hints_sent: boolean
      cache_policy: string
    }
  }[]
  const serverPaged = typeof data?.offset === "number"
  const total = serverPaged ? (summary?.physical_submissions ?? recent.length) : recent.length
  const rows = serverPaged ? recent : recent.slice(offset, offset + usagePageSize)
  const pages = Math.max(1, Math.ceil(total / usagePageSize))
  const page = Math.min(pages, Math.floor(offset / usagePageSize) + 1)
  const figures = [
    ["physical_submissions", "物理提交"],
    ["reported", "缓存字段已报告"],
    ["unknown", "未报告或异常"],
    ["hits", "观察到命中"],
    ["reported_zero", "明确报告为零"],
    ["incomplete", "未完整结束"],
  ] as const
  return (
    <section className="diagnostics">
      <header>
        <div>
          <h2>缓存与实际用量</h2>
          <p className="fine">未知样本不进入命中比例的分母；字段报告、实际命中与账单折扣分别展示。时间为提交开始；响应用时不含工具执行和文章校验，供应商后台可能按完成时刻记账。</p>
        </div>
        <button type="button" className="btn quiet desk-pill" disabled={busy} onClick={() => onPage(offset)}>
          <span className={spinning ? "desk-spin" : "desk-reload"} aria-hidden="true">
            ↻
          </span>
          重新读取
        </button>
      </header>
      {!data ? (
        <p className={busy ? "desk-wait" : "empty-state"} role="status">
          {busy ? <i /> : null}
          {busy ? "正在读取用量…" : "还没有读到用量。"}
        </p>
      ) : (
        <>
          <div className="diagnostic-metrics">
            {figures.map(([id, label]) => (
              <RisingFigure key={id} label={label} value={summary?.[id]} />
            ))}
          </div>
          <p className="doc-note">
            账单折扣：未知。历史仅规范化记录：
            {String(data.legacy_normalized_only ?? "—")} 条，不能按其中的 0 推断真实未命中。
          </p>
          <div className="usage-sheet">
            {rows.length ? (
              <table className="usage-table">
                <caption className="sr-only">模型调用记录</caption>
                <thead>
                  <tr>
                    <th>提交开始（北京时间）</th>
                    <th>模型</th>
                    <th className="num">响应用时 / 首个内容</th>
                    <th className="num">输入</th>
                    <th className="num">输出</th>
                    <th className="num">缓存读 / 写</th>
                    <th>结束</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row, i) => {
                    const report = row.reporting
                    const complete = report?.stream_status === "complete"
                    return (
                      <tr key={`${row.created_at}-${row.submission ?? i}`}>
                        <td>
                          <span className="usage-span"><time dateTime={row.created_at}>{stamp(row.created_at)}</time>{report?.response_finished_at ? <small>完成 {stamp(report.response_finished_at)}</small> : null}</span>
                        </td>
                        <td className="usage-model" title={[row.display_name, row.provider_name].filter(Boolean).join(" · ")}>
                          <span className="usage-span">
                            <span>{row.display_name}</span>
                            <small>{row.provider_name || "历史线路未知"}</small>
                          </span>
                        </td>
                        <td className="num">
                          <span className="usage-span">
                            <span>{duration(report?.total_ms)}</span>
                            <small title="首个非空正文或工具调用增量；历史首字节记录不混作首个内容">{duration(report?.first_meaningful_delta_ms)}</small>
                          </span>
                        </td>
                        <td className={metricKnown(report?.input) ? "num" : "num muted"}>{metric(report?.input)}</td>
                        <td className={metricKnown(report?.output) ? "num" : "num muted"}>{metric(report?.output)}</td>
                        <td className="num">
                          <span className={metricKnown(report?.cache_read) ? undefined : "muted"}>{metric(report?.cache_read)}</span>
                          <span className="usage-split"> / </span>
                          <span className={metricKnown(report?.cache_write) ? undefined : "muted"}>{metric(report?.cache_write)}</span>
                        </td>
                        <td>
                          <span className={`usage-end ${complete ? "on" : ""}`}>{complete ? "完整" : "未完整"}</span>
                          {report?.hints_sent ? <span className="usage-hint">显式</span> : null}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            ) : (
              <p className="empty-state">
                {offset > 0 ? "这一页没有记录。" : "还没有本版本的物理提交用量。已有历史记录保持原事实。"}
              </p>
            )}
            {total > 0 ? (
              <div className="usage-pager">
                <p>
                  共 {total.toLocaleString("zh-Hans")} 条
                  {pages > 1 ? ` · 每页 ${usagePageSize}` : ""}
                </p>
                {pages > 1 ? (
                  <nav aria-label="用量分页">
                    <button type="button" className="usage-page" disabled={busy || page <= 1} onClick={() => onPage(offset - usagePageSize)}>
                      上一页
                    </button>
                    {pageWindow(page, pages).map((item, i) =>
                      item === "gap" ? (
                        <span key={`gap-${i}`} className="usage-gap">
                          …
                        </span>
                      ) : (
                        <button
                          key={item}
                          type="button"
                          className="usage-page"
                          aria-current={item === page ? "page" : undefined}
                          disabled={busy}
                          onClick={() => onPage((item - 1) * usagePageSize)}
                        >
                          {item}
                        </button>
                      ),
                    )}
                    <button type="button" className="usage-page" disabled={busy || page >= pages} onClick={() => onPage(offset + usagePageSize)}>
                      下一页
                    </button>
                  </nav>
                ) : null}
              </div>
            ) : null}
          </div>
        </>
      )}
    </section>
  )
}
