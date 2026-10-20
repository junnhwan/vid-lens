import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { ApiError } from "../api/error"
import { formatDiskBytes } from "../contract/capacity"
import { DeskStatus } from "../ui/DeskStatus"
import type { VideoCleanup } from "./manageRoute"
interface Overview {
	settings: { revision: number; video_quota_bytes: number; image_quota_bytes: number; host_min_free_bytes: number; source_download_limit_bytes: number }
  original_bytes: number
  image_bytes: number
  export_bytes: number
  reserved_video_bytes: number
  reserved_image_bytes: number
  retention: "manual"
  admission?: {
    can_start_source: boolean
    additional_source_slots: number
    blocked_by: string[]
    host_available_bytes: number
    host_total_bytes: number
    host_required_bytes: number
    host_shortfall_bytes: number
    system_reserve_bytes: number
    source_reserve_bytes: number
  }
}
interface Video {
  id: string
  title: string
  url: string
  platform: string
  duration_ms: number | null
  request_count: number
  article_count: number
  latest_status: string
  last_requested_at: string
  original_bytes: number
  image_bytes: number
  asset_ids: string[]
  active: boolean
  protected: boolean
  deleting: boolean
  delete_failures: number
  cleanup?: VideoCleanup
}
interface CleanupCounts { ready: number; held: number; gone: number }
interface VideoPage { items: Video[]; total: number; offset: number; limit: number; counts?: CleanupCounts }
const gib = 1024 ** 3
const mib = 1024 ** 2
const pageSize = 20
const statusLabels: Record<string, string> = { waiting_source: "准备素材", generating: "正在成文", completed: "已完成", failed: "处理失败", canceled: "已取消" }
function date(value: string) { return new Date(value).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }) }
function videoTone(status: string): "ready" | "wait" | "live" | "dim" {
  if (status === "generating" || status === "waiting_source") return "live"
  if (status === "failed") return "wait"
  if (status === "completed") return "ready"
  return "dim"
}
function mediaMark(video: Video): { tone: "ready" | "wait" | "live" | "dim"; label: string; title?: string } {
  if (video.active || video.protected) return { tone: "live", label: "使用中" }
  if (video.deleting && video.delete_failures) return { tone: "wait", label: "清理待重试" }
  if (video.deleting) return { tone: "live", label: "清理中" }
  if (!video.asset_ids.length) return { tone: "dim", label: "已清理", title: "原视频已清理，或当时没有下载。" }
  return { tone: "ready", label: "可清理" }
}
function cleanupOf(video: Video): Exclude<VideoCleanup, ""> {
  if (video.cleanup === "ready" || video.cleanup === "held" || video.cleanup === "gone") return video.cleanup
  if (video.active || video.protected) return "held"
  return video.asset_ids.length ? "ready" : "gone"
}
function cleanupAction(video: Video) {
  const state = cleanupOf(video)
  if (state === "held") return { state, button: "", label: "暂不能清理", reason: "正在使用，处理结束后才能清理" }
  if (state === "gone") return { state, button: "", label: "无可清理", reason: "原视频已清理，或当时没有下载" }
  if (video.deleting) return { state, button: "重试清理", label: "", reason: video.delete_failures ? "上次清理没有完成" : "清理还没有结束" }
  return { state, button: "清理原视频", label: "", reason: "可以清理原视频和音频，文章与配图会留下" }
}
const cleanupFilters: { id: VideoCleanup; label: string }[] = [
  { id: "", label: "全部" },
  { id: "ready", label: "可清理" },
  { id: "held", label: "使用中" },
  { id: "gone", label: "已清理" },
]
function formatAdmissionBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes < 0) return "—"
  if (bytes >= 4 * gib) return formatDiskBytes(bytes)
  return `${Math.round(bytes / mib).toLocaleString("zh-Hans")} MiB`
}

export function VideoStorage({ api, onDirty, onAuthLost, query, offset, cleanup, onRoute }: { api: HttpApi; onDirty: (dirty: boolean) => void; onAuthLost: () => void; query: string; offset: number; cleanup: VideoCleanup; onRoute: (next: { query: string; offset: number; cleanup: VideoCleanup }) => void }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [page, setPage] = useState<VideoPage | null>(null)
  const [hostMinFreeGiB, setHostMinFreeGiB] = useState("")
  const [sourceDownloadLimitMiB, setSourceDownloadLimitMiB] = useState("")
  const [search, setSearch] = useState(query)
  const [refresh, setRefresh] = useState(0)
  const [busy, setBusy] = useState("load")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [confirm, setConfirm] = useState<Video | null>(null)
  const [loadedAt, setLoadedAt] = useState("")
  const cancelRef = useRef<HTMLButtonElement>(null)
  const dirty = Boolean(overview && (Number(hostMinFreeGiB) * gib !== overview.settings.host_min_free_bytes || Number(sourceDownloadLimitMiB) * mib !== overview.settings.source_download_limit_bytes))
  useEffect(() => { setSearch(query) }, [query])
  useEffect(() => { onDirty(dirty || Boolean(confirm) || busy === "save" || busy === "delete") }, [dirty, confirm, busy, onDirty])
  useEffect(() => { if (confirm) cancelRef.current?.focus() }, [confirm])
  function failed(e: unknown) {
    if (e instanceof ApiError && e.status === 401) onAuthLost()
    setError(e instanceof ApiError ? e.detail : "未能确认操作结果，请刷新列表核对后再操作。")
  }
  function accept(v: Overview) {
    setOverview(v)
    setHostMinFreeGiB(String(v.settings.host_min_free_bytes / gib))
    setSourceDownloadLimitMiB(String(v.settings.source_download_limit_bytes / mib))
  }
  useEffect(() => {
    let stop = false
    setBusy("load"); setError(""); setConfirm(null)
    void Promise.all([
      api.request<Overview>("GET", "/api/v1/admin/storage"),
      api.request<VideoPage>("GET", `/api/v1/admin/videos?limit=${pageSize}&offset=${offset}&q=${encodeURIComponent(query)}${cleanup ? `&cleanup=${cleanup}` : ""}`),
    ]).then(([a, b]) => {
      if (stop) return
      accept(a.body); setPage(b.body); setLoadedAt(new Date().toLocaleTimeString("zh-CN", { hour12: false }))
    }).catch((e) => { if (!stop) failed(e) }).finally(() => { if (!stop) setBusy("") })
    return () => { stop = true }
  }, [api, query, offset, cleanup, refresh])
  async function save() {
    if (!overview || busy) return
    setBusy("save"); setError(""); setNotice("")
    try {
      const r = await api.request<Overview>("PUT", "/api/v1/admin/storage", { expected_revision: overview.settings.revision, video_quota_bytes: overview.settings.video_quota_bytes, image_quota_bytes: overview.settings.image_quota_bytes, host_min_free_bytes: Math.round(Number(hostMinFreeGiB) * gib), source_download_limit_bytes: Math.round(Number(sourceDownloadLimitMiB) * mib) })
      accept(r.body); setNotice("存储设置已保存；新视频按服务器实时空闲空间和保护空间准入。")
    } catch (e) { failed(e) }
    finally { setBusy("") }
  }
  async function remove() {
    if (!confirm || busy) return
    setBusy("delete"); setError(""); setNotice("")
    try {
      const r = await api.request<{ deleted_bytes: number; pending_files: number }>("DELETE", `/api/v1/admin/videos/${encodeURIComponent(confirm.id)}/media`, { asset_ids: confirm.asset_ids })
      setConfirm(null)
      setNotice(r.body.pending_files ? `已从 VidLens 工作区移除 ${formatDiskBytes(r.body.deleted_bytes)}，另有 ${r.body.pending_files} 个文件等待重试，仍计入占用。` : `已从 VidLens 工作区移除原视频与音频 ${formatDiskBytes(r.body.deleted_bytes)}，文章和配图继续保留；主机实际可用空间以页面上方准入状态为准。`)
      setRefresh((v) => v + 1)
    } catch (e) { failed(e) }
    finally { setBusy("") }
  }
  function reload() { setNotice(""); setRefresh((v) => v + 1) }
  const admission = overview?.admission
  const admissionScaleMax = admission?.host_total_bytes || 0
  const hostFreeRatio = admission && admissionScaleMax > 0 ? Math.max(0, Math.min(1, admission.host_available_bytes / admissionScaleMax)) : 0
  const admissionLineRatio = admission && admissionScaleMax > 0 ? Math.max(0, Math.min(1, admission.host_required_bytes / admissionScaleMax)) : 0
  const hostDiskBlocked = Boolean(admission?.blocked_by.includes("host_disk"))
  const otherTaskReserve = admission ? Math.max(0, admission.host_required_bytes - admission.system_reserve_bytes - admission.source_reserve_bytes) : 0
  return <section className="video-storage" aria-busy={Boolean(busy)}>
    <header className="storage-heading"><div><p className="eyebrow">VIDEO ARCHIVE</p><h2>视频与存储</h2><p className="fine">全站访客处理过的视频。原视频由你决定何时清理。</p></div>
      <div className="storage-refresh"><span className="fine">{loadedAt ? `${loadedAt} 更新` : "正在读取"}</span><button className="btn quiet desk-pill" disabled={Boolean(busy) || Boolean(confirm)} onClick={reload}>{busy === "load" ? "正在读取…" : dirty ? "撤销并刷新" : "刷新列表"}</button></div></header>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {overview ? <>
      {admission ? <section className={`storage-admission ${admission.can_start_source ? "is-open" : "is-closed"}`} aria-label="新视频存储准入状态" aria-live="polite">
        <div className="storage-admission-lead">
          <span className="eyebrow">服务器实际磁盘空间</span>
          <h3>{admission.can_start_source ? "可以开始新视频" : hostDiskBlocked ? `还差 ${formatAdmissionBytes(admission.host_shortfall_bytes)} 空间` : "暂时不能开始新视频"}</h3>
          {hostDiskBlocked ? <p>服务器现在空闲 {formatAdmissionBytes(admission.host_available_bytes)}，下一条需要 {formatAdmissionBytes(admission.host_required_bytes)}。释放显示的差额后，刷新列表即可重新判断。</p> : <p>服务器现在空闲 {formatAdmissionBytes(admission.host_available_bytes)}；新视频会保留至少 {formatAdmissionBytes(admission.system_reserve_bytes)} 给服务器。</p>}
        </div>
        <dl className="storage-admission-metrics">
          <div><dt>服务器空闲 / 总空间</dt><dd>{formatAdmissionBytes(admission.host_available_bytes)} / {formatAdmissionBytes(admission.host_total_bytes)}</dd></div>
          <div><dt>开始下一条至少需要</dt><dd>{formatAdmissionBytes(admission.host_required_bytes)}</dd></div>
          <div><dt>服务器保护空间</dt><dd>{formatAdmissionBytes(admission.system_reserve_bytes)}</dd></div>
        </dl>
        <figure className="storage-admission-visual">
          <div className="storage-admission-meter" aria-hidden="true">
            <i className="storage-admission-meter-free" style={{ width: `${hostFreeRatio * 100}%` }} />
            <b className="storage-admission-meter-required" style={{ left: `${admissionLineRatio * 100}%` }} />
          </div>
          <figcaption><span>主机已用 {formatAdmissionBytes(Math.max(0, admission.host_total_bytes - admission.host_available_bytes))}</span><span>空闲 {formatAdmissionBytes(admission.host_available_bytes)}</span><span>下一条准入线 {formatAdmissionBytes(admission.host_required_bytes)}</span></figcaption>
        </figure>
        <div className="storage-admission-note">
          <p>看上面的两个数字即可：服务器空闲量高于“下一条准入线”就能开始。准入线已经自动算入保护空间和任务预留，不用手动计算。</p>
          <p>单条音视频上限为 {formatDiskBytes(overview.settings.source_download_limit_bytes)}；实际下载并入磁盘占用后，系统会相应调整任务预留。</p>
          {otherTaskReserve > 0 ? <p>当前任务及导出已预留 {formatAdmissionBytes(otherTaskReserve)}。</p> : null}
        </div>
      </section> : null}
      <div className="storage-summary storage-usage">
        <div><span>VidLens 视频与音频占用</span><strong>{formatDiskBytes(overview.original_bytes)}</strong><p>导出文件 {formatDiskBytes(overview.export_bytes)} · 运行任务预留 {formatDiskBytes(overview.reserved_video_bytes)}</p></div>
        <div><span>VidLens 配图占用</span><strong>{formatDiskBytes(overview.image_bytes)}</strong><p>运行任务预留 {formatDiskBytes(overview.reserved_image_bytes)} · 正式文章配图持续保留</p></div>
      </div>
      <details className="storage-settings"><summary>存储设置 <span>单条上限 {formatDiskBytes(overview.settings.source_download_limit_bytes)} · 主机至少保留 {formatDiskBytes(overview.settings.host_min_free_bytes)}</span></summary>
        <form onSubmit={(e) => { e.preventDefault(); void save() }}><fieldset disabled={Boolean(busy) || Boolean(confirm)}>
          <label className="desk-field"><span>单条来源音视频上限 · MiB</span><input type="number" min={64} max={2048} step={16} required value={sourceDownloadLimitMiB} onChange={(e) => setSourceDownloadLimitMiB(e.target.value)} /></label>
          <label className="desk-field"><span>服务器最低保留 · GiB</span><input type="number" min={3} max={20} step={0.25} required value={hostMinFreeGiB} onChange={(e) => setHostMinFreeGiB(e.target.value)} /></label>
          <button className="btn primary" disabled={!dirty}>{busy === "save" ? "正在保存…" : "保存存储设置"}</button>
        </fieldset><p className="fine">准入按服务器实时空闲空间计算，并至少保留 3 GiB。最低保留量可以在 3–20 GiB 范围内调整；单条上限限制单个来源的音视频大小。修改只影响新任务，不会删除已有视频。</p></form>
      </details>
    </> : <p className="fine">{busy ? "正在读取视频存储…" : "存储状态未能读取，请刷新重试。"}</p>}
    <form className="storage-search" onSubmit={(e) => { e.preventDefault(); setConfirm(null); const next = search.trim(); onRoute({ query: next, offset: 0, cleanup }); if (next === query && offset === 0) reload() }}>
      <label className="desk-field"><span>查找视频</span><input type="search" placeholder="搜索标题、链接或平台" maxLength={200} value={search} disabled={Boolean(busy) || dirty || Boolean(confirm)} onChange={(e) => setSearch(e.target.value)} /></label><button className="btn quiet desk-pill" disabled={Boolean(busy) || dirty || Boolean(confirm)}>搜索</button><span className="fine">{page ? `${cleanup === "ready" ? "可清理" : cleanup === "held" ? "使用中" : cleanup === "gone" ? "已清理" : "全部"} ${page.total} 条` : ""}</span>
    </form>
    <div className="storage-filters" role="group" aria-label="按能否清理筛选">
      {cleanupFilters.map((item) => {
        const count = page?.counts ? (item.id ? page.counts[item.id] : page.counts.ready + page.counts.held + page.counts.gone) : undefined
        return <button key={item.id || "all"} type="button" data-cleanup={item.id || "all"} aria-pressed={cleanup === item.id} disabled={Boolean(busy) || dirty || Boolean(confirm)} onClick={() => { setConfirm(null); if (cleanup !== item.id) onRoute({ query, offset: 0, cleanup: item.id }) }}>{item.label}{count === undefined ? "" : ` ${count.toLocaleString("zh-Hans")}`}</button>
      })}
    </div>
    {page ? <>
      {page.items.length ? <ul className="storage-list">
        {page.items.map((v) => {
          const media = mediaMark(v)
          const action = cleanupAction(v)
          const attention = v.deleting && v.delete_failures > 0
          const platform = v.platform === "bilibili" ? "哔哩哔哩" : v.platform === "youtube" ? "YouTube" : v.platform
          return <li key={v.id} data-cleanup={action.state} className={[attention ? "is-wait" : "", confirm?.id === v.id ? "is-confirming" : ""].filter(Boolean).join(" ") || undefined}>
            <article className="storage-card">
              <div className="storage-card-main">
                <a href={v.url} target="_blank" rel="noopener noreferrer">{v.title} ↗</a>
                <p className="storage-card-meta"><span>{platform}{v.duration_ms ? ` · ${Math.ceil(v.duration_ms / 60000)} 分钟` : ""}</span><time dateTime={v.last_requested_at}>{date(v.last_requested_at)}</time></p>
              </div>
              <div className="storage-card-tags">
                <span className="storage-marks"><DeskStatus tone={videoTone(v.latest_status)}>{statusLabels[v.latest_status] || v.latest_status}</DeskStatus>{v.active ? <DeskStatus tone="live">进行中</DeskStatus> : null}<DeskStatus tone={media.tone} title={media.title}>{media.label}</DeskStatus></span>
                <span className="storage-card-counts">{v.request_count} 次提交 · {v.article_count} 篇文章</span>
                {action.button ? null : <span className="storage-card-note">{action.reason}</span>}
              </div>
              <p className="storage-card-size"><strong>{formatDiskBytes(v.original_bytes)}</strong><span>配图 {formatDiskBytes(v.image_bytes)}</span></p>
              <div className="storage-card-action">{action.button ? <button className={`storage-clean${v.deleting ? " is-retry" : ""}`} disabled={Boolean(busy) || dirty || Boolean(confirm)} title={dirty ? "请先保存或撤销存储设置" : action.reason} onClick={() => { setConfirm(v); setError(""); setNotice("") }}>{action.button}</button> : <DeskStatus tone={action.state === "held" ? "wait" : "dim"}>{action.label}</DeskStatus>}</div>
            </article>
            {confirm?.id === v.id ? <div className="storage-confirm" role="group" aria-label={`确认清理 ${v.title}`}><div><strong>清理「{v.title}」的原素材？</strong><p>将删除原视频与音频 {formatDiskBytes(v.original_bytes)}。文章、配图和处理记录保留；后续需要原画面时可能重新下载。</p></div><button ref={cancelRef} className="btn quiet desk-pill" disabled={Boolean(busy)} onClick={() => setConfirm(null)}>取消</button><button className="btn storage-danger" disabled={Boolean(busy)} onClick={() => void remove()}>{busy === "delete" ? "正在清理…" : "确认清理"}</button></div> : null}
          </li>
        })}
      </ul> : <p className="storage-empty">{query && cleanup ? "没有符合当前搜索和筛选的视频。" : query ? "没有找到匹配的视频。" : cleanup === "ready" ? "现在没有可以清理的原视频。" : cleanup === "held" ? "现在没有使用中的视频。" : cleanup === "gone" ? "还没有已清理的视频。" : offset > 0 ? "这一页没有视频。" : "还没有视频处理记录。"}</p>}
      <div className="storage-pagination"><span className="fine">{page.total ? `${offset + 1}–${Math.min(offset + page.items.length, page.total)} / ${page.total}` : "0 条记录"}</span><button className="btn quiet desk-pill" disabled={Boolean(busy) || dirty || Boolean(confirm) || offset === 0} onClick={() => onRoute({ query, cleanup, offset: Math.max(0, offset - pageSize) })}>上一页</button><button className="btn quiet desk-pill" disabled={Boolean(busy) || dirty || Boolean(confirm) || offset + pageSize >= page.total} onClick={() => onRoute({ query, cleanup, offset: offset + pageSize })}>下一页</button></div>
    </> : null}
    <p className="fine storage-footnote">相同视频的提交合并展示，失败与取消记录也会保留。只有还能清理的视频会显示清理按钮；使用中的要等处理结束，已清理的不再占用原视频。清理前会再确认一次，文章和配图不会被删掉。</p>
  </section>
}
