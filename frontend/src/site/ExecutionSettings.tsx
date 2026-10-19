import { useEffect, useState } from "react"
import { ApiError } from "../api/error"
import type { HttpApi } from "../api/http"
import { ServiceQueue } from "../ui/ServiceQueue"

interface Settings { revision: number; max_video_running: number }

export function ExecutionSettings({ api, onDirty, onAuthLost }: { api: HttpApi; onDirty: (dirty: boolean) => void; onAuthLost: () => void }) {
  const [saved, setSaved] = useState<Settings | null>(null)
  const [limit, setLimit] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const dirty = saved !== null && limit !== String(saved.max_video_running)
  useEffect(() => { onDirty(dirty) }, [dirty, onDirty])
  function accept(v: Settings) { setSaved(v); setLimit(String(v.max_video_running)) }
  function failed(e: unknown) {
    if (e instanceof ApiError && e.status === 401) onAuthLost()
    setError(e instanceof ApiError ? e.detail : "未能确认队列配置，请重新读取后再试。")
  }
  useEffect(() => {
    let stopped = false
    api.request<Settings>("GET", "/api/v1/admin/execution-settings")
      .then((r) => { if (!stopped) accept(r.body) })
      .catch((e) => { if (!stopped) failed(e) })
    return () => { stopped = true }
  }, [api])
  async function reload() {
    setBusy(true); setError(""); setNotice("")
    try { accept((await api.request<Settings>("GET", "/api/v1/admin/execution-settings")).body) }
    catch (e) { failed(e) }
    finally { setBusy(false) }
  }
  async function save() {
    if (!saved || busy) return
    setBusy(true); setError(""); setNotice("")
    try {
      const r = await api.request<Settings>("PUT", "/api/v1/admin/execution-settings", { expected_revision: saved.revision, max_video_running: Number(limit) })
      accept(r.body)
      setNotice(`已保存，最大同时运行 ${r.body.max_video_running} 个。等待任务会按新上限继续调度。`)
    } catch (e) { failed(e) }
    finally { setBusy(false) }
  }
  return <section className="network-settings execution-settings">
    <header><div><p className="eyebrow">PROCESSING QUEUE</p><h2>任务与队列</h2>
      <p className="fine">按使用情况调整全站视频处理上限，保存后生效，无需重启服务。</p></div></header>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {!saved ? <><p className="fine">{error ? "配置尚未读取。" : "正在读取队列配置…"}</p><button className="btn quiet" disabled={busy} onClick={() => void reload()}>重新读取</button></> : <form onSubmit={(e) => { e.preventDefault(); void save() }}>
      <fieldset disabled={busy}>
        <label className="desk-field execution-limit"><span>最大同时运行的视频任务数</span>
          <div><input type="number" min={1} max={64} step={1} required value={limit} onChange={(e) => setLimit(e.target.value)} /><span>个</span></div>
        </label>
        <div className="execution-presets" aria-label="常用运行上限"><span>快速选择</span>{[2, 4, 6, 10].map((n) => <button key={n} className="btn quiet desk-pill" type="button" aria-pressed={limit === String(n)} onClick={() => setLimit(String(n))}>{n} 个</button>)}</div>
        <p className="fine">可设置 1–64 个。提高上限会开放更多运行名额；降低上限时，已开始的任务继续完成，再按新上限调度。</p>
        <p className="fine">问答独立调度。下载、转写和抽帧按各自资源名额运行，实际处理速度也受服务器和模型服务影响。</p>
        <div className="form-actions"><button className="btn primary" disabled={!dirty}>{busy ? "正在保存…" : "保存运行上限"}</button><button className="btn quiet desk-pill" type="button" onClick={() => void reload()}>{dirty ? "撤销并重新读取" : "重新读取"}</button></div>
      </fieldset>
    </form>}
    <div className="execution-live"><p className="eyebrow">LIVE ACTIVITY</p><ServiceQueue active /></div>
  </section>
}
