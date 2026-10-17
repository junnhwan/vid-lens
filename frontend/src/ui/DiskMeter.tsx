import { useEffect, useState } from "react"
import { diskLabel, diskNote, diskRatio, diskStatus, formatDiskBytes, formatDiskPercent, type SiteCapacity } from "../contract/capacity"
import { prefersReduced } from "./motion"

export function DiskMeter({
  capacity,
  state,
  active,
}: {
  capacity: SiteCapacity | null
  state: "loading" | "ready" | "error"
  active: boolean
}) {
  const [drawn, setDrawn] = useState(false)
  useEffect(() => {
    if (!capacity || !active) return
    if (prefersReduced()) {
      setDrawn(true)
      return
    }
    const frame = requestAnimationFrame(() => setDrawn(true))
    return () => cancelAnimationFrame(frame)
  }, [capacity, active])

  const size = 92
  const stroke = 6
  const radius = (size - stroke) / 2 - 4
  const circumference = 2 * Math.PI * radius
  const center = size / 2
  const used = capacity ? diskRatio(capacity.used_bytes, capacity.total_bytes) : 0
  const ceiling = capacity ? diskRatio(capacity.ceiling_bytes, capacity.total_bytes) : 0
  const angle = ceiling * Math.PI * 2 - Math.PI / 2
  const shown = drawn ? used : 0
  const stop = {
    cx: center + Math.cos(angle) * radius,
    cy: center + Math.sin(angle) * radius,
  }

  return (
    <section
      className="disk-meter"
      data-level={capacity?.level ?? state}
      aria-busy={state === "loading" ? true : undefined}
      aria-label={capacity ? diskLabel(capacity) : "视频工作区"}
    >
      <svg className="disk-ring" viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
        <circle className="disk-track" cx={center} cy={center} r={radius} />
        <circle
          className="disk-arc"
          cx={center}
          cy={center}
          r={radius}
          strokeDasharray={circumference}
          strokeDashoffset={circumference * (1 - shown)}
        />
        {capacity ? <circle className="disk-stop" r="3.5" {...stop} /> : null}
        <text className="disk-percent" x={center} y={center + 1}>
          {capacity ? formatDiskPercent(capacity.used_bytes, capacity.total_bytes) : ""}
          {capacity ? <tspan className="disk-percent-unit">%</tspan> : null}
        </text>
      </svg>
      <div className="disk-copy">
        <p className="disk-kicker">视频工作区</p>
        <p className="disk-status">
          {capacity ? diskStatus(capacity) : state === "error" ? "暂时读不到" : "正在读取"}
        </p>
        {capacity ? (
          <>
            <p className="disk-figures">
              <span>剩余 {formatDiskBytes(capacity.available_bytes)}</span>
              <span>配额 {formatDiskBytes(capacity.total_bytes)}</span>
            </p>
            <div className="disk-scale" aria-hidden="true">
              <span style={{ width: `${shown * 100}%` }} />
              <i style={{ left: `${ceiling * 100}%` }} />
            </div>
            <p className="disk-note">{capacity.reserved_bytes > 0 ? `任务预留 ${formatDiskBytes(capacity.reserved_bytes)}。` : ""}{diskNote(capacity)}</p>
          </>
        ) : (
          <p className="disk-note">查看视频与处理素材的专用空间。</p>
        )}
      </div>
    </section>
  )
}
