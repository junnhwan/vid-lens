import { ApiError, NetworkError } from "../api/error"
import type { ActivityStep, RequestView, TurnView } from "../contract/types"

export function errorText(error: unknown): string {
  if (error instanceof ApiError) {
    const fields = error.meta?.field_errors?.map((item) => item.message).filter(Boolean) ?? []
    if (fields.length) return fields.join(" ")
    return error.detail || "请求没有完成。"
  }
  if (error instanceof NetworkError) return error.message
  return "请求没有完成。"
}

export function normalizeStep(step: ActivityStep): ActivityStep {
  return {
    ...step,
    frames: Array.isArray(step.frames) ? step.frames : [],
    frame_ids: Array.isArray(step.frame_ids) ? step.frame_ids : [],
  }
}

export function isRequestView(value: unknown): value is RequestView {
  return !!value && typeof value === "object" && "request" in value && "activity" in value && "stream" in value
}

export function isTurnView(value: unknown): value is TurnView {
  return !!value && typeof value === "object" && "turn" in value && "activity" in value && "stream" in value
}

export class ObsoleteRequest extends Error { }
