import { useEffect, useState } from "react"
import { HttpApi } from "../api/http"
import { ApiError } from "../api/error"

interface Settings {
  revision: number
  reference_read_enabled: boolean
  search_enabled: boolean
  key_configured: boolean
  daily_limit: number
  monthly_limit: number
  daily_used: number
  monthly_used: number
  month_exhausted: boolean
}

export function NetworkSettings({ api, onDirty, onAuthLost }: { api: HttpApi; onDirty: (dirty: boolean) => void; onAuthLost: () => void }) {
  const [saved, setSaved] = useState<Settings | null>(null)
  const [draft, setDraft] = useState<Settings | null>(null)
  const [key, setKey] = useState("")
  const [clear, setClear] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const dirty = Boolean(saved && draft && (key || clear || saved.reference_read_enabled !== draft.reference_read_enabled || saved.search_enabled !== draft.search_enabled || saved.daily_limit !== draft.daily_limit || saved.monthly_limit !== draft.monthly_limit))
  useEffect(() => { onDirty(dirty) }, [dirty, onDirty])
  function failed(e: unknown) {
    if (e instanceof ApiError && e.status === 401) onAuthLost()
    setError(e instanceof ApiError ? e.detail : "未能确认操作结果，请重新读取配置后再试。")
  }
  function accept(value: Settings) {
    setSaved(value); setDraft(value); setKey(""); setClear(false); onDirty(false)
  }
  useEffect(() => {
    let stop = false
    void api.request<Settings>("GET", "/api/v1/admin/network-settings")
      .then((r) => { if (!stop) accept(r.body) })
      .catch((e) => { if (!stop) failed(e) })
    return () => { stop = true }
  }, [api])
  async function reload() {
    setBusy(true); setError(""); setNotice("")
    try { accept((await api.request<Settings>("GET", "/api/v1/admin/network-settings")).body) }
    catch (e) { failed(e) }
    finally { setBusy(false) }
  }
  async function save() {
    if (!draft || !saved || busy) return
    setBusy(true); setError(""); setNotice("")
    try {
      const r = await api.request<Settings>("PUT", "/api/v1/admin/network-settings", {
        expected_revision: saved.revision,
        reference_read_enabled: draft.reference_read_enabled,
        search_enabled: draft.search_enabled,
        daily_limit: draft.daily_limit,
        monthly_limit: draft.monthly_limit,
        api_key: key,
        clear_key: clear,
      })
      accept(r.body)
      setNotice("已保存并立即生效。访客刷新页面后可读取最新开关。")
    } catch (e) { failed(e) }
    finally { setBusy(false) }
  }
  function patch(values: Partial<Settings>) { setDraft((v) => v ? { ...v, ...values } : v) }
  return <section className="network-settings">
    <header><div><p className="eyebrow">WEB REFERENCES</p><h2>联网搜索</h2>
      <p className="fine">补充当前视频的私人问答。配置只对当前站点生效，本地与线上分别保存。</p></div>
      <a className="text-action" href="https://app.tavily.com/" target="_blank" rel="noopener noreferrer">申请 Tavily Key ↗</a>
    </header>
    {error ? <p className="management-message error" role="alert">{error}</p> : null}
    {notice ? <p className="management-message" role="status">{notice}</p> : null}
    {!draft || !saved ? <><p className="fine">{error ? "配置尚未读取。" : "正在读取联网配置…"}</p><button className="btn quiet" disabled={busy} onClick={() => void reload()}>重新读取</button></> : <form onSubmit={(e) => { e.preventDefault(); void save() }}>
      <fieldset disabled={busy}>
        <div className="desk-switches">
          <label className="ink-check"><input type="checkbox" checked={draft.reference_read_enabled} onChange={(e) => patch({ reference_read_enabled: e.target.checked, ...(!e.target.checked ? { search_enabled: false } : {}) })} /><span />允许读取公开网页</label>
          <label className="ink-check"><input type="checkbox" checked={draft.search_enabled} disabled={!draft.reference_read_enabled} onChange={(e) => patch({ search_enabled: e.target.checked })} /><span />启用 Tavily 搜索</label>
        </div>
        <label className="desk-field"><span>Tavily API Key · {saved.key_configured ? "已配置" : "尚未配置"}</span>
          <input type="password" autoComplete="new-password" spellCheck={false} maxLength={512} value={key} disabled={clear} placeholder={saved.key_configured ? "留空保留已有 Key；填写以替换" : "粘贴 tvly- 开头的 Key"} onChange={(e) => setKey(e.target.value)} />
        </label>
        {saved.key_configured ? <label className="ink-check"><input type="checkbox" checked={clear} onChange={(e) => { setClear(e.target.checked); if (e.target.checked) { setKey(""); patch({ search_enabled: false }) } }} /><span />清除已保存的 Key 并关闭搜索</label> : null}
        <div className="network-limits">
          <label className="desk-field"><span>全站每日搜索上限</span><input type="number" min={0} max={1000} step={1} required value={draft.daily_limit} onChange={(e) => patch({ daily_limit: e.target.valueAsNumber })} /></label>
          <label className="desk-field"><span>全站每月搜索上限</span><input type="number" min={0} max={1000} step={1} required value={draft.monthly_limit} onChange={(e) => patch({ monthly_limit: e.target.valueAsNumber })} /></label>
        </div>
        <p className="doc-note">今日已用 {saved.daily_used} / {saved.daily_limit} 次 · 本月已用 {saved.monthly_used} / {saved.monthly_limit} 次{saved.month_exhausted ? " · 上游月额度已耗尽" : ""}</p>
        <p className="fine">全体访客共用额度，按实际搜索请求计数；结果未知也计入。每日按北京时间零点重置，月度按 UTC 月份统计，设为 0 即停止搜索。每轮问答最多搜索 2 次。</p>
        <p className="fine">这里只使用 Basic 搜索；Tavily 控制台请保持 Pay as you go 关闭。同一 Key 若用于其他站点，其用量不计入本站额度。</p>
        <div className="form-actions"><button className="btn primary" disabled={!dirty}>{busy ? "正在保存…" : "保存联网配置"}</button><button className="btn quiet desk-pill" type="button" onClick={() => void reload()}>{dirty ? "撤销并重新读取" : "刷新用量"}</button></div>
      </fieldset>
    </form>}
  </section>
}
