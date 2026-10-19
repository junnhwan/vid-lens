import { DeskSelect, emptyProvider, InkSwitch, Provider, ProviderDraft, providerDraft, proxyOptions } from "./manageCatalog"
export interface ManageProvidersProps {
  openSection: (id: string) => boolean
  editProvider: (p?: Provider) => void
  providers: Provider[]
  pd: ProviderDraft
  dirty: boolean
  saveProvider: () => Promise<void>
  setPd: import("react").Dispatch<import("react").SetStateAction<ProviderDraft>>
  setDirty: (dirty: boolean) => void
  busy: string
  discover: () => Promise<void>
  discovery: string[]
  setPicked: import("react").Dispatch<import("react").SetStateAction<string[]>>
  picked: string[]
  importModels: () => Promise<void>
}
export function ManageProviders({ openSection, editProvider, providers, pd, dirty, saveProvider, setPd, setDirty, busy, discover, discovery, setPicked, picked, importModels }: ManageProvidersProps) {
  return ((
    <div className="manage-columns">
      <section className="provider-rail">
        <div className="manage-list-head">
          <h2>供应商</h2>
          <div className="head-actions">
            <button type="button" className="text-action" onClick={() => openSection("models")}>
              返回模型
            </button>
            <button type="button" className="btn quiet desk-pill" onClick={() => editProvider()}>
              新增
            </button>
          </div>
        </div>
        {providers.length ? (
          providers.map((p) => (
            <button
              type="button"
              className="provider-item"
              key={p.id}
              aria-current={pd.id === p.id ? "true" : undefined}
              onClick={() => editProvider(p)}
            >
              <span className={`status-dot ${p.enabled ? "on" : ""}`} />
              <span>
                <strong>{p.name}</strong>
                <small>
                  {p.enabled ? "已启用" : "已停用"} · {p.key_configured ? "密钥已配置" : "未配置密钥"}
                </small>
              </span>
              <span className="nudge">↗</span>
            </button>
          ))
        ) : (
          <p className="empty-state">还没有供应商。从右侧添加一个 OpenAI 兼容端点。</p>
        )}
      </section>
      <section className="manage-form">
        <h2>
          {pd.id ? "编辑供应商" : "添加供应商"}
          {dirty ? <span className="desk-dirty">未保存</span> : null}
        </h2>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void saveProvider()
          }}
        >
          <label className="desk-field">
            <span>名称</span>
            <input
              value={pd.name}
              onChange={(e) => {
                setPd({ ...pd, name: e.target.value })
                setDirty(true)
              }}
              required
            />
          </label>
          <label className="desk-field">
            <span>OpenAI 兼容 base URL</span>
            <input
              value={pd.base_url}
              placeholder="https://api.example.com/v1"
              spellCheck={false}
              onChange={(e) => {
                setPd({ ...pd, base_url: e.target.value })
                setDirty(true)
              }}
              required
            />
          </label>
          <label className="desk-field">
            <span>{pd.id ? "更新密钥（留空保持原值）" : "API Key"}</span>
            <input
              type="password"
              autoComplete="new-password"
              value={pd.api_key}
              spellCheck={false}
              onChange={(e) => {
                setPd({ ...pd, api_key: e.target.value })
                setDirty(true)
              }}
            />
          </label>
          <DeskSelect
            label="出站策略"
            value={pd.proxy_policy}
            options={proxyOptions}
            placeholder="选择出站策略"
            onChange={(id) => {
              setPd({ ...pd, proxy_policy: id })
              setDirty(true)
            }}
          />
          <div className="desk-switches">
            <InkSwitch
              checked={pd.enabled}
              label="启用供应商"
              onChange={(checked) => {
                setPd({ ...pd, enabled: checked })
                setDirty(true)
              }}
            />
          </div>
          <p className="fine">Key 保存后只显示配置状态。轮换密钥创建新修订；撤销旧 Key 可能让旧视频暂时不能继续问答。</p>
          <div className="form-actions">
            <button className="btn primary" disabled={!!busy}>
              {busy === "provider" ? "正在保存" : "保存供应商"}
            </button>
            <button
              type="button"
              className="btn quiet desk-pill"
              onClick={() => {
                setDirty(false)
                setPd(
                  providers.find((p) => p.id === pd.id) ? providerDraft(providers.find((p) => p.id === pd.id)!) : emptyProvider,
                )
              }}
            >
              撤销修改
            </button>
            {pd.id ? (
              <button type="button" className="btn quiet desk-pill" disabled={!!busy || dirty} onClick={() => void discover()}>
                {busy === "discover" ? "正在发现" : "拉取模型列表"}
              </button>
            ) : null}
          </div>
        </form>
        <div className="fold" data-open={discovery.length ? "true" : "false"}>
          <div className="fold-clip">
            {discovery.length ? (
              <div className="discovered">
                <div className="discovered-head">
                  <h3>
                    发现的候选 <span>{discovery.length}</span>
                  </h3>
                  <button
                    type="button"
                    className="text-action"
                    onClick={() => setPicked(picked.length === discovery.length ? [] : [...discovery])}
                  >
                    {picked.length === discovery.length ? "取消全选" : "全选"}
                  </button>
                </div>
                <p className="fine">发现只表示名字存在，不代表看图或工具能力。</p>
                {discovery.map((id) => (
                  <label className="ink-check" key={id}>
                    <input
                      type="checkbox"
                      checked={picked.includes(id)}
                      onChange={(e) => setPicked((prev) => (e.target.checked ? [...prev, id] : prev.filter((v) => v !== id)))}
                    />
                    <span />
                    <code>{id}</code>
                  </label>
                ))}
                <button type="button" className="btn primary" disabled={!!busy || !picked.length} onClick={() => void importModels()}>
                  导入 {picked.length} 个候选为停用模型
                </button>
              </div>
            ) : null}
          </div>
        </div>
      </section>
    </div>
  ))
}
