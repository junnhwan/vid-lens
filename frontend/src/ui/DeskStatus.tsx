import type { ReactNode } from "react"

export function DeskStatus({ tone, title, children }: { tone: "ready" | "wait" | "live" | "dim"; title?: string; children: ReactNode }) {
  return <span className={`desk-status is-${tone}`} title={title}>{children}</span>
}
