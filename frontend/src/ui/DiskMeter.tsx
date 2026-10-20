import { diskStatus, type SiteCapacity } from "../contract/capacity"

export function DiskMeter({
  capacity,
  state,
}: {
  capacity: SiteCapacity | null
  state: "loading" | "ready" | "error"
}) {
  if (state === "error" && !capacity) return <p className="disk-quiet">暂时读不到服务器磁盘状态。</p>
  if (!capacity || capacity.level === "open") return null
  return (
    <p className="disk-quiet" data-level={capacity.level}>
      {diskStatus(capacity)} · 服务器会保留至少 3 GiB 空间，清理后可重试。
    </p>
  )
}
