import { ModelIcon } from "../ui/ModelIdentity"
import { actionNote, brandOptions, cacheOptions, catalogLine, DeskSelect, InkSwitch, labels, LensGlyph, Model, ModelDraft, nextStep, Probe, Provider } from "./manageCatalog"
import { ManageRouteOrder } from "./ManageRouteOrder"
import { isOpenProbe, liveStatusText, modelActivity, probeTiming } from "./probeStatus"
export interface ManageModelsProps {
  ready: boolean
  error: string
  models: Model[]
  composing: boolean
  providers: Provider[]
  openProviders: (target?: Provider) => void
  beginAddProvider: () => void
  busy: string
  importCurrent: () => Promise<void>
  editModel: (m?: Model) => void
  filter: string
  filtered: Model[]
  setFilter: import("react").Dispatch<import("react").SetStateAction<string>>
  probeByModel: Record<string, Probe[]>
  now: number
  md: ModelDraft
  modelMeta: (model: Pick<Model, "display_name" | "upstream_id" | "provider_id" | "route_priority">) => string
  selected: Model | undefined
  cancelCompose: () => void
  dirty: boolean
  selectedProbes: Probe[]
  checking: boolean
  probe: (m: Model, mode: string) => Promise<void>
  capabilityProbe: Probe | undefined
  toggleModel: (m: Model) => Promise<void>
  makeDefault: (m: Model) => Promise<void>
  setRecommended: (m: Model, recommended: boolean) => Promise<void>
  setError: import("react").Dispatch<import("react").SetStateAction<string>>
  setNotice: import("react").Dispatch<import("react").SetStateAction<string>>
  saveModel: () => Promise<void>
  patchModel: (partial: Partial<ModelDraft>) => void
  advanced: boolean
  setAdvanced: import("react").Dispatch<import("react").SetStateAction<boolean>>
  orderedRouteModels: Model[]
  providerName: (id: string) => string
  moveRoute: (id: string, toIndex: number) => void
  routeOrderChanged: boolean
  saveRouteOrder: () => Promise<void>
  cacheChecking: boolean
  cacheProbe: Probe | undefined
  revertModel: () => void
}
export function ManageModels({ ready, error, models, composing, providers, openProviders, beginAddProvider, busy, importCurrent, editModel, filter, filtered, setFilter, probeByModel, now, md, modelMeta, selected, cancelCompose, dirty, selectedProbes, checking, probe, capabilityProbe, toggleModel, makeDefault, setRecommended, setError, setNotice, saveModel, patchModel, advanced, setAdvanced, orderedRouteModels, providerName, moveRoute, routeOrderChanged, saveRouteOrder, cacheChecking, cacheProbe, revertModel }: ManageModelsProps) {
  return ((
    !ready ? (
      error ? null : (
        <p className="desk-wait" role="status">
          <i />
          正在读取目录…
        </p>
      )
    ) : !models.length && !composing ? (
      <section className="setup-sheet manage-form">
        <p className="eyebrow">
          <span />
          先准备一个可选用的模型
        </p>
        <h2>三步之后，访客才能选到它</h2>
        <ol className="setup-rail">
          <li data-state={providers.length ? "done" : "current"}>
            <b>1</b>
            <div>
              <strong>添加端点</strong>
              <p>一个 OpenAI 兼容地址，和只保存一次的密钥。</p>
              {providers.length ? (
                <button type="button" className="text-action" onClick={() => openProviders()}>
                  查看 {providers.length} 个供应商
                </button>
              ) : (
                <button type="button" className="btn primary" onClick={beginAddProvider}>
                  添加供应商
                </button>
              )}
            </div>
          </li>
          <li data-state={providers.length ? "current" : "wait"}>
            <b>2</b>
            <div>
              <strong>把模型放进目录</strong>
              <p>从端点拉取名字，导入现有配置，或直接填写上游 ID。放进来的模型先保持停用。</p>
              <div className="setup-actions">
                <button
                  type="button"
                  className={providers.length ? "btn primary" : "btn quiet desk-pill"}
                  disabled={!providers.length}
                  onClick={() => openProviders()}
                >
                  去拉取模型列表
                </button>
                <button type="button" className="btn quiet desk-pill" disabled={!!busy} onClick={() => void importCurrent()}>
                  导入现有配置
                </button>
                <button type="button" className="text-action" disabled={!providers.length} onClick={() => editModel()}>
                  手动填写
                </button>
              </div>
            </div>
          </li>
          <li data-state="wait">
            <b>3</b>
            <div>
              <strong>检查后启用</strong>
              <p>用真实图片和工具试一次。通过并启用之后，才会出现在访客面前。</p>
            </div>
          </li>
        </ol>
      </section>
    ) : (
      <div className={models.length ? "model-stage" : "model-compose"}>
        {models.length ? (
          <section className="model-ledger">
            <div className="ledger-head">
              <span>目录</span>
              <span>{filter ? `${filtered.length} / ${models.length}` : models.length}</span>
            </div>
            <label className="ledger-search">
              <span className="sr-only">筛选模型</span>
              <LensGlyph />
              <input
                id="model-filter"
                placeholder="筛选名称、供应商或上游 ID"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
            </label>
            <div className="ledger-scroll">
              {filtered.length ? (
                <ul className="ledger-list">
                  {filtered.map((m) => {
                    const line = catalogLine(m, modelActivity(probeByModel[m.id]), now)
                    return (
                      <li key={m.id} data-current={!composing && md.id === m.id ? "true" : "false"}>
                        <button
                          type="button"
                          className="ledger-open"
                          aria-current={!composing && md.id === m.id ? "true" : undefined}
                          onClick={() => editModel(m)}
                        >
                          <span className="ledger-mark">
                            <ModelIcon brand={m.brand_id} name={m.display_name} />
                          </span>
                          <span className="ledger-copy">
                            <strong>
                              {m.display_name}
                              {m.recommended ? <i className="ledger-recommend">推荐</i> : null}
                            </strong>
                            <small>{modelMeta(m)}</small>
                            <em data-tone={line.tone}>{line.text}</em>
                          </span>
                        </button>
                      </li>
                    )
                  })}
                </ul>
              ) : (
                <p className="empty-state">没有符合「{filter}」的模型。</p>
              )}
            </div>
            <div className="ledger-foot">
              <button type="button" className="text-action" aria-current={composing ? "true" : undefined} disabled={!providers.length} onClick={() => editModel()}>
                手动添加
              </button>
            </div>
          </section>
        ) : null}
        {composing || selected ? (
          <section className="manage-form model-sheet" key={composing ? "new" : selected?.id}>
            {composing ? (
              <button type="button" className="text-action sheet-back" onClick={cancelCompose}>
                {models.length ? "返回这条目录" : "返回准备步骤"}
              </button>
            ) : null}
            <header className="sheet-id">
              <span className="ledger-mark">
                <ModelIcon brand={composing ? md.brand_id : selected?.brand_id} name={composing ? md.display_name : selected?.display_name} />
              </span>
              <div>
                <h2>
                  {composing ? "添加模型" : selected?.display_name}
                  {dirty ? <span className="desk-dirty">未保存</span> : null}
                </h2>
                <p>{composing ? "先保存为停用。检查通过后才能启用。" : selected ? modelMeta(selected) : ""}</p>
              </div>
              {!composing && selected ? (
                <span className={`desk-seal ${catalogLine(selected, modelActivity(selectedProbes), now).tone}`}>
                  {catalogLine(selected, modelActivity(selectedProbes), now).text}
                </span>
              ) : null}
            </header>
            {!composing && selected ? (
              <>
                <div className="sheet-actions">
                  <button
                    type="button"
                    className={nextStep(selected) === "probe" && selected.provider_enabled && !dirty && !busy && !checking ? "btn primary" : "btn quiet desk-pill"}
                    disabled={!!busy || dirty || checking || !selected.provider_enabled}
                    onClick={() => void probe(selected, "capabilities")}
                  >
                    {checking && capabilityProbe ? (
                      <>
                        <span className="desk-spin" aria-hidden="true">
                          ↻
                        </span>
                        {liveStatusText(capabilityProbe, now)}
                      </>
                    ) : (
                      "检查图片与工具"
                    )}
                  </button>
                  <button
                    type="button"
                    className={nextStep(selected) === "enable" && !dirty && !busy && !checking ? "btn primary" : "btn quiet desk-pill"}
                    disabled={!!busy || dirty || checking || (!selected.enabled && (!selected.verified || !selected.provider_enabled))}
                    onClick={() => void toggleModel(selected)}
                  >
                    {selected.enabled ? "停用" : "启用"}
                  </button>
                  <button
                    type="button"
                    className={nextStep(selected) === "default" && !dirty && !busy && !checking ? "btn primary" : "btn quiet desk-pill"}
                    disabled={!!busy || dirty || checking || !selected.enabled || !selected.verified || !selected.provider_enabled || selected.is_default}
                    onClick={() => void makeDefault(selected)}
                  >
                    {selected.is_default ? "已是默认" : "设为默认"}
                  </button>
                </div>
                <div className="recommend-switch">
                  <InkSwitch
                    checked={selected.recommended}
                    disabled={!!busy}
                    label="首页推荐"
                    onChange={(checked) => void setRecommended(selected, checked)}
                  />
                  <small>勾选后，首页的模型菜单会标出推荐。</small>
                </div>
              </>
            ) : null}
            {!composing && selected && actionNote(selected, dirty, Boolean(modelActivity(selectedProbes))) ? (
              <p className="sheet-reason" id="model-action-note">
                {actionNote(selected, dirty, Boolean(modelActivity(selectedProbes)))}
                {!selected.provider_enabled ? (
                  <button type="button" className="text-action" onClick={() => openProviders(providers.find((p) => p.id === selected.provider_id))}>
                    打开这个供应商
                  </button>
                ) : null}
              </p>
            ) : null}
            <form
              onSubmit={(e) => {
                e.preventDefault()
                if (!md.provider_id) {
                  setError("请先选择供应商。")
                  setNotice("")
                  return
                }
                void saveModel()
              }}
            >
              {composing ? (
                <div className="form-grid">
                  <DeskSelect
                    label="供应商"
                    value={md.provider_id}
                    placeholder={providers.length ? "选择供应商" : "还没有供应商"}
                    options={providers.map((p) => ({
                      id: p.id,
                      label: p.name,
                      hint: `${p.enabled ? "已启用" : "已停用"} · ${p.key_configured ? "密钥已配置" : "未配置密钥"}`,
                    }))}
                    onChange={(id) => patchModel({ provider_id: id })}
                  />
                  <label className="desk-field">
                    <span>显示名</span>
                    <input value={md.display_name} onChange={(e) => patchModel({ display_name: e.target.value })} required />
                  </label>
                  <label className="desk-field">
                    <span>上游 model ID</span>
                    <input value={md.upstream_id} spellCheck={false} onChange={(e) => patchModel({ upstream_id: e.target.value })} required />
                  </label>
                </div>
              ) : null}
              <div className="tune" data-open={advanced ? "true" : "false"}>
                <button
                  type="button"
                  className="tune-toggle"
                  aria-expanded={advanced}
                  onClick={() => {
                    if (advanced && (!md.display_name.trim() || !md.upstream_id.trim() || !md.provider_id)) {
                      setError("显示名、上游 ID 和供应商需要保留。")
                      return
                    }
                    setError("")
                    setAdvanced((open) => !open)
                  }}
                >
                  <span>
                    <strong>调整参数</strong>
                    <small>线路顺序、额度、缓存与说明</small>
                  </span>
                  <i aria-hidden="true" />
                </button>
                {advanced ? (
                  <div className="tune-body">
                    {composing ? null : (
                      <div className="form-grid">
                        <DeskSelect
                          label="供应商"
                          value={md.provider_id}
                          placeholder={providers.length ? "选择供应商" : "还没有供应商"}
                          disabled={!!md.id}
                          unavailable={md.provider_id && !providers.some((p) => p.id === md.provider_id) ? "原供应商已不在目录中" : undefined}
                          options={providers.map((p) => ({
                            id: p.id,
                            label: p.name,
                            hint: `${p.enabled ? "已启用" : "已停用"} · ${p.key_configured ? "密钥已配置" : "未配置密钥"}`,
                          }))}
                          onChange={(id) => patchModel({ provider_id: id })}
                        />
                        <label className="desk-field">
                          <span>显示名</span>
                          <input value={md.display_name} onChange={(e) => patchModel({ display_name: e.target.value })} required />
                        </label>
                        <label className="desk-field">
                          <span>上游 model ID</span>
                          <input value={md.upstream_id} spellCheck={false} onChange={(e) => patchModel({ upstream_id: e.target.value })} required />
                        </label>
                      </div>
                    )}
                    <div className="tune-grid">
                      <DeskSelect
                        label="品牌标识"
                        value={md.brand_id}
                        options={brandOptions}
                        placeholder="选择品牌"
                        onChange={(id) => patchModel({ brand_id: id })}
                      />
                      <label className="desk-field">
                        <span>输出 tokens 上限</span>
                        <input
                          type="number"
                          min={1024}
                          value={md.max_output_tokens}
                          onChange={(e) => patchModel({ max_output_tokens: Number(e.target.value) })}
                        />
                      </label>
                      <label className="desk-field">
                        <span>兼容旧版字符额度</span>
                        <input
                          type="number"
                          min={10000}
                          value={md.max_input_chars}
                          onChange={(e) => patchModel({ max_input_chars: Number(e.target.value) })}
                        />
                      </label>
                      <label className="desk-field">
                        <span>实际输入字符上限（0 沿用旧配置）</span>
                        <input type="number" min={0} max={8000000} value={md.input_chars_limit} onChange={(e) => patchModel({ input_chars_limit: Number(e.target.value) })} />
                      </label>
                      <label className="desk-field">
                        <span>共享上下文 tokens（0 表示未知）</span>
                        <input type="number" min={0} max={4000000} value={md.context_tokens} onChange={(e) => patchModel({ context_tokens: Number(e.target.value) })} />
                      </label>
                      <DeskSelect
                        label="缓存策略"
                        value={md.cache_policy}
                        options={cacheOptions}
                        placeholder="选择缓存策略"
                        onChange={(id) => patchModel({ cache_policy: id })}
                      />
                    </div>
                    {!composing && selected && orderedRouteModels.length > 1 ? (
                      <ManageRouteOrder {...{ orderedRouteModels, providerName, moveRoute, busy, dirty, routeOrderChanged, saveRouteOrder }} />
                    ) : null}
                    <label className="desk-field">
                      <span>给访客的一句说明</span>
                      <input maxLength={240} value={md.description} onChange={(e) => patchModel({ description: e.target.value })} />
                    </label>
                    <div className="desk-switches">
                      <InkSwitch
                        checked={md.disable_thinking}
                        label="发送关闭思考的兼容参数"
                        onChange={(checked) => patchModel({ disable_thinking: checked })}
                      />
                    </div>
                    {!composing && selected ? (
                      <div className="tune-extra">
                        <button
                          type="button"
                          className="btn quiet desk-pill"
                          disabled={!!busy || dirty || cacheChecking || !selected.provider_enabled}
                          onClick={() => void probe(selected, "cache")}
                        >
                          {cacheChecking && cacheProbe ? (
                            <>
                              <span className="desk-spin" aria-hidden="true">
                                ↻
                              </span>
                              {cacheProbe.status === "queued" ? "排队中" : "正在检查缓存"} · {probeTiming(cacheProbe, now)}
                            </>
                          ) : (
                            "串行检查缓存"
                          )}
                        </button>
                        <p className="fine">
                          缓存检查单独串行，不批量探测。显式提示要等端点报告实际创建或命中。一次小样本通过，不代表长视频质量。
                        </p>
                      </div>
                    ) : (
                      <p className="fine">这些值已按常用额度填好。改输出上限或输入保护会在保存时生成新修订。</p>
                    )}
                  </div>
                ) : null}
              </div>
              {composing || dirty ? (
                <>
                  <div className="form-actions">
                    <button className="btn primary" disabled={!!busy || (!composing && !dirty)}>
                      {busy === "model" ? "正在保存" : composing ? "保存为停用模型" : "保存参数"}
                    </button>
                    <button type="button" className="btn quiet desk-pill" onClick={revertModel}>
                      撤销
                    </button>
                  </div>
                  <p className="fine">
                    {composing
                      ? "保存后仍是停用。检查通过，才能启用。"
                      : "改名称、额度或缓存会生成新修订，并先停用。检查通过后再启用。"}
                  </p>
                </>
              ) : null}
            </form>
            {!composing && selected ? (
              <section className="probe-history">
                <h3>最近检查</h3>
                {selectedProbes.length ? (
                  <ol className="probe-rail">
                    {selectedProbes.map((p) => {
                      const timing = probeTiming(p, now)
                      return (
                        <li key={p.id} data-status={p.status}>
                          <i aria-hidden="true" />
                          <div>
                            <strong>
                              {labels[p.status] || p.status}
                              {isOpenProbe(p) && timing ? ` · ${timing}` : ""}
                            </strong>
                            <span>
                              {p.mode === "cache" ? "缓存样本" : "图片与工具"} · {p.submissions} 次物理提交
                            </span>
                            {!isOpenProbe(p) && timing ? <small>用时 {timing}</small> : null}
                            {p.failure_code ? <code>{p.failure_code}</code> : null}
                            {p.mode === "cache" && p.status === "succeeded" ? (
                              <small>
                                {p.result?.usage_reported ? "缓存字段已报告" : "缓存字段未报告"} ·{" "}
                                {p.result?.cache_hit_observed ? "观察到命中" : "未取得命中证据"} · 账单折扣未知
                              </small>
                            ) : null}
                          </div>
                          <time dateTime={p.created_at}>{new Date(p.created_at).toLocaleString("zh-Hans")}</time>
                        </li>
                      )
                    })}
                  </ol>
                ) : (
                  <p className="fine">尚无检查记录。</p>
                )}
              </section>
            ) : null}
          </section>
        ) : (
          <p className="desk-wait" role="status">
            <i />
            正在读取目录…
          </p>
        )}
      </div>
    )
  ))
}
