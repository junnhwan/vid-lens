import { describe, expect, it } from "vitest"
import { diskLabel, diskNote, diskStatus, formatDiskBytes, formatDiskPercent, type SiteCapacity } from "./capacity"

const open: SiteCapacity = {
  total_bytes: 40 * 1024 * 1024 * 1024,
  available_bytes: 12 * 1024 * 1024 * 1024,
  used_bytes: 28 * 1024 * 1024 * 1024,
  reserve_bytes: 6 * 1024 * 1024 * 1024,
  ceiling_bytes: 31 * 1024 * 1024 * 1024,
  stop_percent: 85,
  accepting: true,
  level: "open",
}

describe("disk capacity copy", () => {
  it("formats binary sizes without a trailing zero", () => {
    expect(formatDiskBytes(10 * 1024 * 1024 * 1024)).toBe("10 GB")
    expect(formatDiskBytes(10.5 * 1024 * 1024 * 1024)).toBe("10.5 GB")
    expect(formatDiskBytes(512 * 1024 * 1024)).toBe("512 MB")
  })

  it("rounds the ring percent to one decimal", () => {
    expect(formatDiskPercent(71, 100)).toBe("71")
    expect(formatDiskPercent(773, 1000)).toBe("77.3")
  })

  it("describes the admission state", () => {
    expect(diskStatus(open)).toBe("还可以接收")
    expect(diskStatus({ ...open, level: "tight" })).toBe("余量不多")
    expect(diskStatus({ ...open, level: "closed", accepting: false, blocked_by: "disk" })).toBe("暂时不能下载")
    expect(diskNote({ ...open, level: "closed", accepting: false, blocked_by: "media" })).toContain("临时文件")
    expect(diskLabel(open)).toContain("服务器磁盘")
  })
})
