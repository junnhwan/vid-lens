import { Fragment, useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"
import { ApiError } from "../api/error"
import { diskRatio, formatDiskBytes } from "../contract/capacity"
interface Overview {
  settings: { revision: number; video_quota_bytes: number; image_quota_bytes: number }
  original_bytes: number
  image_bytes: number
  export_bytes: number
  reserved_video_bytes: number
  reserved_image_bytes: number
  retention: "manual"
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
}
interface VideoPage { items: Video[]; total: number; offset: number; limit: number }
const gib = 1024 ** 3
const pageSize = 20
const statusLabels: Record<string, string> = { waiting_source: "准备素材", generating: "正在成文", completed: "已完成", failed: "处理失败", canceled: "已取消" }
function date(value: string) { return new Date(value).toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }) }

export function VideoStorage({ api, onDirty, onAuthLost }: { api: HttpApi; onDirty: (dirty: boolean) => void; onAuthLost: () => void }) {
  const [overview, setOverview] = useState<Overview | null>(null)
  const [page, setPage] = useState<VideoPage | null>(null)
  const [videoQuota, setVideoQuota] = useState("")
  const [imageQuota, setImageQuota] = useState("")
  const [search, setSearch] = useState("")
  const [query, setQuery] = useState("")
  const [offset, setOffset] = useState(0)
  const [refresh, setRefresh] = useState(0)
  const [busy, setBusy] = useState("load")
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [confirm, setConfirm] = useState<Video | null>(null)
  const [loadedAt, setLoadedAt] = useState("")
  const cancelRef = useRef<HTMLButtonElement>(null)
  const dirty = Boolean(overview && (Number(videoQuota) * gib !== overview.settings.video_quota_bytes || Number(imageQuota) * gib !== overview.settings.image_quota_bytes))
  useEffect(() => { onDirty(dirty || Boolean(confirm) || busy === "save" || busy === "delete") }, [dirty, confirm, busy, onDirty])
  useEffect(() => { if (confirm) cancelRef.current?.focus() }, [confirm])
  function failed(e: unknown) {
    if (e instanceof ApiError && e.status === 401) onAuthLost()
    setError(e instanceof ApiError ? e.detail : "未能确认操作结果，请刷新列表核对后再操作。")
  }
  function accept(v: Overview) {
    setOverview(v)
    setVideoQuota(String(v.settings.video_quota_bytes / gib))
    setImageQuota(String(v.settings.image_quota_bytes / gib))
  }
  useEffect(() => {
    let stop = false
    setBusy("load"); setError(""); setConfirm(null)
    void Promise.all([
      api.request<Overview>("GET", "/api/v1/admin/storage"),
      api.request<VideoPage>("GET", `/api/v1/admin/videos?limit=${pageSize}&offset=${offset}&q=${encodeURIComponent(query)}`),
    ]).then(([a, b]) => {
      if (stop) return
      accept(a.body); setPage(b.body); setLoadedAt(new Date().toLocaleTimeString("zh-CN", { hour12: false }))
    }).catch((e) => { if (!stop) failed(e) }).finally(() => { if (!stop) setBusy("") })
    return () => { stop = true }
  }, [api, query, offset, refresh])
  async function save() {
    if (!overview || busy) return
    setBusy("save"); setError(""); setNotice("")
    try {
      const r = await api.request<Overview>("PUT", "/api/v1/admin/storage", { expected_revision: overview.settings.revision, video_quota_bytes: Math.round(Number(videoQuota) * gib), image_quota_bytes: Math.round(Number(imageQuota) * gib) })
      accept(r.body); setNotice("配额已保存，新的下载会立即按新配额检查容量。")
    } catch (e) { failed(e) }
    finally { setBusy("") }
  }
  async function remove() {
    if (!confirm || busy) return
    setBusy("delete"); setError(""); setNotice("")
    try {
      const r = await api.request<{ deleted_bytes: number; pending_files: number }>("DELETE", `/api/v1/admin/videos/${encodeURIComponent(confirm.id)}/media`, { asset_ids: confirm.asset_ids })
      setConfirm(null)
      setNotice(r.body.pending_files ? `已清理 ${formatDiskBytes(r.body.deleted_bytes)}，另有 ${r.body.pending_files} 个文件等待重试，仍计入占用。` : `已清理原视频与音频 ${formatDiskBytes(r.body.deleted_bytes)}，文章和配图继续保留。`)
      setRefresh((v) => v + 1)
    } catch (e) { failed(e) }
    finally { setBusy("") }
  }
  function reload() { setNotice(""); setRefresh((v) => v + 1) }
  const videoUsed = overview ? overview.original_bytes + overview.export_bytes : 0
  return <section className="video-storage" aria-busy={Boolean(busy)}>
    <header className="storage-heading"><div><p className="eyebrow">VIDEO ARCHIVE</p><h2>视频与存储</h2><p className="fine">全站访客处理过的视频。原视频由你决定何时清理。</p></div>
      <div className="storage-refresh"><span className="fine">{loadedAt ? `${loadedAt} 更新` : "正在读取"}</span><button className="btn quiet desk-pill" disabled={Boolean(busy) || Boolean(confirm)} onClick={reload}>{busy === "load" ? "正在读取…" : dirty ? "撤销并刷新" : "刷新列表"}</button></div></header>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {overview ? <>
      <div className="storage-summary">
        <div><span>视频工作区</span><strong>{formatDiskBytes(videoUsed)} <small>/ {formatDiskBytes(overview.settings.video_quota_bytes)}</small></strong><div className="storage-bar"><i style={{ width: `${diskRatio(videoUsed, overview.settings.video_quota_bytes) * 100}%` }} /><b style={{ width: `${diskRatio(overview.reserved_video_bytes, overview.settings.video_quota_bytes) * 100}%` }} /></div><p>原视频与音频 {formatDiskBytes(overview.original_bytes)} · 导出 {formatDiskBytes(overview.export_bytes)} · 任务预留 {formatDiskBytes(overview.reserved_video_bytes)}</p></div>
        <div><span>配图空间</span><strong>{formatDiskBytes(overview.image_bytes)} <small>/ {formatDiskBytes(overview.settings.image_quota_bytes)}</small></strong><div className="storage-bar"><i style={{ width: `${diskRatio(overview.image_bytes, overview.settings.image_quota_bytes) * 100}%` }} /><b style={{ width: `${diskRatio(overview.reserved_image_bytes, overview.settings.image_quota_bytes) * 100}%` }} /></div><p>任务预留 {formatDiskBytes(overview.reserved_image_bytes)} · 正式文章配图持续保留</p></div>
      </div>
      <details className="storage-settings"><summary>调整服务配额 <span>当前 {formatDiskBytes(overview.settings.video_quota_bytes)} 视频 / {formatDiskBytes(overview.settings.image_quota_bytes)} 配图</span></summary>
        <form onSubmit={(e) => { e.preventDefault(); void save() }}><fieldset disabled={Boolean(busy) || Boolean(confirm)}>
          <label className="desk-field"><span>视频工作区配额 · GiB</span><input type="number" min={1} max={10240} step="any" required value={videoQuota} onChange={(e) => setVideoQuota(e.target.value)} /></label>
          <label className="desk-field"><span>配图配额 · GiB</span><input type="number" min={1} max={10240} step="any" required value={imageQuota} onChange={(e) => setImageQuota(e.target.value)} /></label>
          <button className="btn primary" disabled={!dirty}>{busy === "save" ? "正在保存…" : "保存配额"}</button>
        </fieldset><p className="fine">配额是 VidLens 可使用的上限，不会在系统中预占磁盘。空间不足时暂停新下载；调整配额不会自动删除视频。</p></form>
      </details>
    </> : <p className="fine">{busy ? "正在读取视频存储…" : "存储状态未能读取，请刷新重试。"}</p>}
    <form className="storage-search" onSubmit={(e) => { e.preventDefault(); setConfirm(null); setOffset(0); setQuery(search.trim()); if (search.trim() === query && offset === 0) reload() }}>
      <label className="desk-field"><span>查找视频</span><input type="search" placeholder="搜索标题、链接或平台" maxLength={200} value={search} disabled={Boolean(busy) || dirty || Boolean(confirm)} onChange={(e) => setSearch(e.target.value)} /></label><button className="btn quiet desk-pill" disabled={Boolean(busy) || dirty || Boolean(confirm)}>搜索</button><span className="fine">{page ? `共 ${page.total} 条视频` : ""}</span>
    </form>
    {page ? <>
      <div className="storage-table-wrap"><table className="storage-table"><thead><tr><th scope="col">视频来源</th><th scope="col">处理情况</th><th scope="col">原素材 / 配图</th><th scope="col">最近提交</th><th scope="col">管理</th></tr></thead><tbody>
        {page.items.map((v) => <Fragment key={v.id}><tr>
          <td><a href={v.url} target="_blank" rel="noopener noreferrer">{v.title} ↗</a><p>{v.platform === "bilibili" ? "哔哩哔哩" : v.platform === "youtube" ? "YouTube" : v.platform}{v.duration_ms ? ` · ${Math.ceil(v.duration_ms / 60000)} 分钟` : ""}</p></td>
          <td><span className="storage-status" data-active={v.active}>{statusLabels[v.latest_status] || v.latest_status}</span><p>{v.request_count} 次提交 · {v.article_count} 篇文章{v.active ? " · 有任务进行中" : ""}</p></td>
          <td><strong>{formatDiskBytes(v.original_bytes)}</strong><p>配图 {formatDiskBytes(v.image_bytes)}{v.deleting ? v.delete_failures ? " · 清理待重试" : " · 清理中" : !v.asset_ids.length ? " · 原视频已清理或未下载" : " · 手动保留"}</p></td>
          <td><time dateTime={v.last_requested_at}>{date(v.last_requested_at)}</time></td>
          <td><button className="text-action storage-delete" disabled={Boolean(busy) || dirty || v.active || v.protected || !v.asset_ids.length || Boolean(confirm)} title={v.active || v.protected ? "正在使用，处理结束后可清理" : "清理原视频与音频，保留文章与配图"} onClick={() => { setConfirm(v); setError(""); setNotice("") }}>{v.active || v.protected ? "使用中" : v.deleting ? "重试清理" : "清理原视频"}</button></td>
        </tr>{confirm?.id === v.id ? <tr className="storage-confirm"><td colSpan={5}><div role="group" aria-label={`确认清理 ${v.title}`}><div><strong>清理「{v.title}」的原素材？</strong><p>将删除原视频与音频 {formatDiskBytes(v.original_bytes)}。文章、配图和处理记录保留；后续需要原画面时可能重新下载。</p></div><button ref={cancelRef} className="btn quiet desk-pill" disabled={Boolean(busy)} onClick={() => setConfirm(null)}>取消</button><button className="btn storage-danger" disabled={Boolean(busy)} onClick={() => void remove()}>{busy === "delete" ? "正在清理…" : "确认清理"}</button></div></td></tr> : null}</Fragment>)}
        {!page.items.length ? <tr><td colSpan={5} className="storage-empty">{query ? "没有找到匹配的视频。" : "还没有视频处理记录。"}</td></tr> : null}
      </tbody></table></div>
      <div className="storage-pagination"><span className="fine">{page.total ? `${offset + 1}–${Math.min(offset + page.items.length, page.total)} / ${page.total}` : "0 条记录"}</span><button className="btn quiet desk-pill" disabled={Boolean(busy) || dirty || Boolean(confirm) || offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}>上一页</button><button className="btn quiet desk-pill" disabled={Boolean(busy) || dirty || Boolean(confirm) || offset + pageSize >= page.total} onClick={() => setOffset(offset + pageSize)}>下一页</button></div>
    </> : null}
    <p className="fine storage-footnote">相同视频的提交合并展示，失败与取消记录也会保留。仅清理你确认的原素材；正在使用的文件受到保护，文章配图不会随之删除。</p>
  </section>
}
