import { useEffect, useRef, useState, type MutableRefObject } from "react"
import { ApiError } from "../api/error"
import { HttpApi } from "../api/http"
import { message, type Probe } from "./manageCatalog"
import { isOpenProbe, modelActivity } from "./probeStatus"

interface ProbeMonitorOptions {
  api: MutableRefObject<HttpApi>
  auth: boolean | null
  busy: string
  selectedId: string | undefined
  refresh(): Promise<unknown>
  setError(message: string): void
  onAuthLost(): void
}
export function useProbeMonitor({ api, auth, busy, selectedId, refresh, setError, onAuthLost }: ProbeMonitorOptions) {
  const [probeByModel, setProbeByModel] = useState<Record<string, Probe[]>>({})
  const currentProbes = useRef(probeByModel)
  currentProbes.current = probeByModel
  const [now, setNow] = useState(() => Date.now())
  const watched = [
    ...new Set(
      [selectedId, ...Object.keys(probeByModel).filter((id) => modelActivity(probeByModel[id]))].filter(
        (id): id is string => Boolean(id),
      ),
    ),
  ].sort()
  const watchKey = watched.join(",")
  const ticking = watched.some((id) => Boolean(modelActivity(probeByModel[id])))
  useEffect(() => {
    if (!auth || !ticking) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [auth, ticking])
  useEffect(() => {
    if (!auth || !watchKey) return
    const ids = watchKey.split(",")
    let stop = false,
      timer: ReturnType<typeof setTimeout> | undefined
    async function poll() {
      if (document.hidden) {
        timer = setTimeout(poll, 2000)
        return
      }
      let failure: unknown
      const pages = await Promise.all(
        ids.map(async (id) => {
          try {
            const r = await api.current.request<{ items: Probe[] }>("GET", `/api/v1/admin/models/${id}/probes`)
            return { id, items: r.body.items, failed: false }
          } catch (e) {
            failure = failure ?? e
            return { id, items: [] as Probe[], failed: true }
          }
        }),
      )
      if (stop) return
      const finished = pages.some(page => !page.failed && currentProbes.current[page.id]?.some(previous =>
        isOpenProbe(previous) && page.items.some(item => item.id === previous.id && !isOpenProbe(item)),
      ))
      setProbeByModel((prev) => {
        const next = { ...prev }
        for (const page of pages) if (!page.failed) next[page.id] = page.items
        return next
      })
      const still = pages.some((page) => (page.failed ? Boolean(modelActivity(probeByModel[page.id])) : page.items.some(isOpenProbe)))
      if (failure) {
        if (failure instanceof ApiError && failure.status === 401) { onAuthLost(); return }
        setError(message(failure))
      }
      if (still) timer = setTimeout(poll, 2000)
      if (!failure && (!still || finished)) void refresh().catch(() => { })
    }
    void poll()
    return () => {
      stop = true
      if (timer) clearTimeout(timer)
    }
  }, [auth, watchKey, busy])
  return { probeByModel, now, acceptProbes: (id: string, probes: Probe[]) => { setProbeByModel(previous => ({ ...previous, [id]: probes })); setNow(Date.now()) } }
}
