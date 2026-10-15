import type { StreamPosition } from "./types"

export interface Clock {
  revision: number
  runId: string | null
  seq: number
  cursor: string
}

export type SnapshotDecision = "install" | "ignore" | "confirm" | "resync"
export type UpsertDecision = "apply" | "ignore" | "resync"

export function clockFrom(position: StreamPosition | null | undefined): Clock | null {
  if (!position || typeof position.cursor !== "string") return null
  return {
    revision: position.revision,
    runId: position.run_id ?? null,
    seq: position.seq,
    cursor: position.cursor,
  }
}

function sameRun(a: string | null, b: string | null): boolean {
  return a === b
}

export function decideSnapshot(
  clock: Clock | null,
  next: Clock,
  terminal: boolean,
  source: "sse" | "http",
): SnapshotDecision {
  if (!clock) return "install"
  if (next.revision < clock.revision) return "ignore"
  if (next.revision > clock.revision) return "install"
  if (!sameRun(next.runId, clock.runId)) return source === "http" ? "install" : "resync"
  if (next.cursor === clock.cursor || next.seq < clock.seq) {
    return terminal && next.seq === clock.seq ? "confirm" : "ignore"
  }
  if (next.seq === clock.seq) return terminal ? "confirm" : "ignore"
  return "install"
}

export function decideUpsert(clock: Clock | null, next: Clock, stepLastSeq: number): UpsertDecision {
  if (!clock) return "resync"
  if (next.revision < clock.revision) return "ignore"
  if (next.revision > clock.revision) return "resync"
  if (!sameRun(next.runId, clock.runId)) return "resync"
  if (next.seq <= clock.seq) return "ignore"
  if (next.seq !== clock.seq + 1 || stepLastSeq !== next.seq) return "resync"
  return "apply"
}

export function isTerminalStatus(status: string | null | undefined): boolean {
  return status === "completed" || status === "failed" || status === "canceled" || status === "succeeded"
}
