import { describe, expect, it } from "vitest"
import type { ActivityStep } from "../contract/types"
import { ResourceStream, type LiveSocket } from "./live"

class FakeSocket implements LiveSocket {
  readyState = 1
  onerror: ((this: LiveSocket, ev: Event) => void) | null = null
  private listeners = new Map<string, ((event: MessageEvent) => void)[]>()
  closed = false

  addEventListener(type: string, listener: (event: MessageEvent) => void) {
    const list = this.listeners.get(type) ?? []
    list.push(listener)
    this.listeners.set(type, list)
  }

  emit(type: string, data: unknown, lastEventId = "old-cursor") {
    const event = { data: JSON.stringify(data), lastEventId } as MessageEvent
    for (const listener of this.listeners.get(type) ?? []) listener(event)
  }

  close() {
    this.closed = true
    this.readyState = 2
  }
}

function envelope(event: Record<string, unknown>) {
  return event
}

describe("resource stream", () => {
  it("handles a reset that inherited an old last event id before cursor checks", async () => {
    const sockets: FakeSocket[] = []
    let snapshots = 0
    let auths = 0
    const stream = new ResourceStream({
      events: () => {
        const socket = new FakeSocket()
        sockets.push(socket)
        return socket
      },
      getSnapshot: async () => ({ request: { status: "generating" }, stream: { cursor: "fresh", revision: 3, run_id: "run", seq: 4 } }),
      onSnapshot: () => {
        snapshots += 1
        return "stay"
      },
      onUpsert: () => "stay",
      onAuth: () => {
        auths += 1
      },
      onStatus: () => undefined,
      scope: { type: "request", id: "req" },
      resourcePath: "/api/v1/requests/req/events",
    })
    stream.start("cursor-1")
    sockets[0].emit("stream.reset", {
      schema_version: 1,
      scope: { type: "request", id: "req" },
      payload: { code: "CURSOR_EXPIRED", snapshot_url: "/api/v1/requests/req" },
    })
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(sockets[0].closed).toBe(true)
    expect(snapshots).toBe(1)
    expect(auths).toBe(0)
    expect(sockets.length).toBe(2)
    stream.stop()
  })

  it("does not open a second socket while the browser is still reconnecting", () => {
    const socket = new FakeSocket()
    let opened = 0
    const stream = new ResourceStream({
      events: () => {
        opened += 1
        return socket
      },
      getSnapshot: async () => ({}),
      onSnapshot: () => "stay",
      onUpsert: () => "stay",
      onAuth: () => undefined,
      onStatus: () => undefined,
      scope: { type: "request", id: "req" },
      resourcePath: "/api/v1/requests/req/events",
    })
    stream.start("cursor-1")
    socket.readyState = 0
    socket.onerror?.call(socket, new Event("error"))
    expect(opened).toBe(1)
    stream.stop()
  })

  it("applies an upsert only through the caller and closes on a terminal snapshot", () => {
    const socket = new FakeSocket()
    const applied: string[] = []
    const stream = new ResourceStream({
      events: () => socket,
      getSnapshot: async () => ({}),
      onSnapshot: () => "close",
      onUpsert: (step) => {
        applied.push(step.id)
        return "stay"
      },
      onAuth: () => undefined,
      onStatus: () => undefined,
      scope: { type: "turn", id: "turn-1" },
      resourcePath: "/api/v1/conversations/c/turns/turn-1/events",
    })
    stream.start("cursor-1")
    const step = { id: "step-1" } as ActivityStep
    socket.emit("activity.upsert", envelope({
      schema_version: 1,
      scope: { type: "turn", id: "turn-1" },
      position: { cursor: "c2", revision: 1, run_id: "run", seq: 2 },
      payload: { step, current_step_id: "step-1" },
    }))
    expect(applied).toEqual(["step-1"])
    socket.emit("view.snapshot", envelope({
      schema_version: 1,
      scope: { type: "turn", id: "turn-1" },
      position: { cursor: "c3", revision: 1, run_id: "run", seq: 3 },
      payload: { turn: { status: "completed" } },
    }))
    expect(socket.closed).toBe(true)
    stream.stop()
  })
})
