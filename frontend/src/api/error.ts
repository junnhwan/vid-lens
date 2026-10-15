import type { Problem } from "../contract/types"

export class ApiError extends Error {
  status: number
  code: string
  detail: string
  meta: Problem["meta"]

  constructor(problem: Problem) {
    super(problem.detail || problem.code || "请求没有完成")
    this.name = "ApiError"
    this.status = problem.status
    this.code = problem.code
    this.detail = problem.detail
    this.meta = problem.meta ?? null
  }
}

export class NetworkError extends Error {
  constructor(message = "网络结果还不能确认") {
    super(message)
    this.name = "NetworkError"
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError
}

export function replayable(error: unknown): boolean {
  if (error instanceof NetworkError) return true
  if (error instanceof ApiError) return error.status === 503 || error.status === 429 || error.code === "SERVICE_UNAVAILABLE"
  return false
}

export function sessionLost(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}
