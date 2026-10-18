import { useEffect, useRef } from "react"
import { ResourceStream } from "../api/live"

class Subscription {
  private stream: ResourceStream | null = null
  private resourceId = ""

  get id() { return this.resourceId }

  attach(id: string, stream: ResourceStream, cursor: string) {
    this.stop()
    this.resourceId = id
    this.stream = stream
    stream.start(cursor)
  }

  stop() {
    this.stream?.stop()
    this.stream = null
    this.resourceId = ""
  }
}

// One route owns every asynchronous resource underneath it. Observing the
// route during render fences old callbacks before effect cleanup runs.
export class VisitorWorkspace {
  private generation = 0
  route: string | undefined
  readonly request = new Subscription()
  readonly turn = new Subscription()

  get epoch() { return this.generation }

  owns(generation: number, requestId: string | undefined) {
    return generation === this.generation && requestId === this.route
  }

  ownsRequest(generation: number, id: string, currentId: string | undefined) {
    return this.owns(generation, id) && id === currentId
  }

  invalidate() {
    this.generation += 1
    this.request.stop()
    this.turn.stop()
  }

  begin() {
    this.invalidate()
    return this.generation
  }
}

export function useVisitorWorkspace(requestId: string | undefined) {
  const ref = useRef<VisitorWorkspace | null>(null)
  if (!ref.current) ref.current = new VisitorWorkspace()
  const workspace = ref.current
  workspace.route = requestId
  useEffect(() => () => workspace.invalidate(), [workspace])
  return workspace
}
