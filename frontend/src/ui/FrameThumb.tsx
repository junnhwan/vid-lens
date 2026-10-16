import { useEffect, useState } from "react"
import type { Frame } from "../contract/types"
import { formatClock, unavailableFrameCopy } from "../contract/text"

export function FrameThumb({
  frame,
  variant = "full",
  onBroken,
}: {
  frame: Frame | null | undefined
  variant?: "thumbnail" | "full"
  onBroken?: (frame: Frame) => void
}) {
  const [failed, setFailed] = useState(false)
  const src =
    frame && frame.availability === "ready"
      ? variant === "thumbnail"
        ? frame.urls.thumbnail || frame.urls.full
        : frame.urls.full || frame.urls.thumbnail
      : ""
  useEffect(() => setFailed(false), [src])
  if (!frame || frame.availability !== "ready" || failed || !src) {
    return <span className="frame-missing">{unavailableFrameCopy(frame?.unavailable_reason)}</span>
  }
  return (
    <img
      data-frame-id={frame.id}
      src={src}
      alt={frame.caption || formatClock(frame.time_ms)}
      onError={() => {
        setFailed(true)
        onBroken?.(frame)
      }}
    />
  )
}
