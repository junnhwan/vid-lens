import { ApiError } from "./error"
import type { SharedRecordResponse } from "../contract/types"

export async function loadSharedRecord(token: string, signal?: AbortSignal, fetchImpl: typeof fetch = fetch): Promise<SharedRecordResponse> {
  const response = await fetchImpl(`/api/v1/shares/${encodeURIComponent(token)}`, {
    signal,
    credentials: "omit",
    cache: "no-store",
    referrerPolicy: "no-referrer",
    headers: { accept: "application/json" },
  })
  if (!response.ok) throw new ApiError({
    status: response.status,
    code: response.status === 404 ? "RESOURCE_NOT_FOUND" : "SERVICE_UNAVAILABLE",
    detail: response.status === 404 ? "分享链接不存在或已取消。" : "暂时无法读取分享内容，请稍后重试。",
  })
  return response.json() as Promise<SharedRecordResponse>
}
