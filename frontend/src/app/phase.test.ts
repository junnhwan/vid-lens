import { describe, expect, it } from "vitest"
import { phaseOnArrival } from "./useVisitor"

describe("phase on arrival", () => {
  it("opens a request link on the processing view", () => {
    expect(phaseOnArrival("f5cc0b9c0ab11eca7471a19b8da3205d")).toBe("run")
  })

  it("opens the home page only when the address has no request", () => {
    expect(phaseOnArrival(undefined)).toBe("input")
  })
})
