import { useState } from "react"
import { Model } from "./manageCatalog"
export interface ManageRouteOrderProps {
  orderedRouteModels: Model[]
  providerName: (id: string) => string
  moveRoute: (id: string, toIndex: number) => void
  busy: string
  dirty: boolean
  routeOrderChanged: boolean
  saveRouteOrder: () => Promise<void>
}
export function ManageRouteOrder({ orderedRouteModels, providerName, moveRoute, busy, dirty, routeOrderChanged, saveRouteOrder }: ManageRouteOrderProps) {
  const [draggingRoute, setDraggingRoute] = useState("")
  return (<section className="route-order-panel" aria-labelledby="route-order-title">
    <header className="route-order-head">
      <div>
        <span className="route-order-kicker">同型号备用线路</span>
        <h3 id="route-order-title">设置供应商尝试顺序</h3>
      </div>
      <span className="route-order-count">{orderedRouteModels.length} 条线路</span>
    </header>
    <p className="route-order-help">
      拖动线路调整顺序。恢复时会先尝试发送次数较少的供应商；次数相同时按这里的顺序选择。只有已启用且检查通过的线路会参与请求。
    </p>
    <ol className="route-order-list">
      {orderedRouteModels.map((model, index) => {
        const provider = providerName(model.provider_id)
        const status = !model.provider_enabled
          ? { text: "供应商已停用", tone: "dim" }
          : !model.verified
            ? { text: "待检查", tone: "wait" }
            : !model.enabled
              ? { text: "模型已停用", tone: "dim" }
              : { text: "参与路由", tone: "ready" }
        return (
          <li
            key={model.id}
            data-dragging={draggingRoute === model.id ? "true" : "false"}
            onDragOver={(event) => event.preventDefault()}
            onDrop={(event) => {
              event.preventDefault()
              const id = event.dataTransfer.getData("text/plain") || draggingRoute
              if (id) moveRoute(id, index)
              setDraggingRoute("")
            }}
          >
            <button
              type="button"
              className="route-order-handle"
              draggable={!busy && !dirty}
              disabled={!!busy || dirty}
              aria-label={`拖动${provider}线路，当前第${index + 1}位`}
              onDragStart={(event) => {
                setDraggingRoute(model.id)
                event.dataTransfer.effectAllowed = "move"
                event.dataTransfer.setData("text/plain", model.id)
              }}
              onDragEnd={() => setDraggingRoute("")}
            >
              <span className="route-order-grip" aria-hidden="true">⠿</span>
            </button>
            <span className="route-order-position">{index + 1}</span>
            <span className="route-order-copy">
              <strong>{provider}</strong>
              <small>{model.upstream_id}</small>
            </span>
            <span className="route-order-actions">
              <span className="route-order-status" data-tone={status.tone}>{status.text}</span>
              <button
                type="button"
                className="route-nudge"
                aria-label={`将${provider}线路上移`}
                disabled={!!busy || dirty || index === 0}
                onClick={() => moveRoute(model.id, index - 1)}
              >
                ↑
              </button>
              <button
                type="button"
                className="route-nudge"
                aria-label={`将${provider}线路下移`}
                disabled={!!busy || dirty || index === orderedRouteModels.length - 1}
                onClick={() => moveRoute(model.id, index + 1)}
              >
                ↓
              </button>
            </span>
          </li>
        )
      })}
    </ol>
    <footer className="route-order-foot">
      <span>{routeOrderChanged ? "顺序已调整，保存后生效。" : "可拖动排序，也可用上下箭头微调。"}</span>
      <button type="button" className="btn primary" disabled={!!busy || dirty || !routeOrderChanged} onClick={() => void saveRouteOrder()}>
        {busy === "route-order" ? "正在保存" : "保存顺序"}
      </button>
    </footer>
  </section>)
}
