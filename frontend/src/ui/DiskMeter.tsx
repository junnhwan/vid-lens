import { diskStatus, formatDiskBytes, type SiteCapacity } from "../contract/capacity"

export function DiskMeter({
  capacity,
  state,
}: {
  capacity: SiteCapacity | null
  state: "loading" | "ready" | "error"
}) {
  if (state === "error" && !capacity) return <p className="disk-quiet">暂时读不到工作区余量。</p>
  if (!capacity || capacity.level === "open") return null
  return (
    <p className="disk-quiet" data-level={capacity.level}>
      {diskStatus(capacity)} · 剩余 {formatDiskBytes(capacity.available_bytes)}
    </p>
  )
}
