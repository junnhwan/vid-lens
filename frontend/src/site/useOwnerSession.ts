import { useEffect, useRef, useState } from "react"
import { HttpApi } from "../api/http"

type Action = (name: string, work: () => Promise<void>) => Promise<void>

export function useOwnerSession(action: Action) {
  const api = useRef(new HttpApi())
  const [auth, setAuthenticated] = useState<boolean | null>(null)
  const [password, setPassword] = useState("")
  const [reveal, setReveal] = useState(false)
  const revision = useRef(0)
  const alive = useRef(true)

  function setAuth(value: boolean | null) {
    revision.current += 1
    if (!value) api.current.csrfToken = ""
    setAuthenticated(value)
  }

  useEffect(() => {
    alive.current = true
    const generation = revision.current
    document.title = "站长管理 · VidLens"
    const meta = document.createElement("meta")
    meta.name = "robots"
    meta.content = "noindex,nofollow"
    document.head.appendChild(meta)
    void api.current.request<{ csrf_token: string }>("GET", "/api/v1/admin/session")
      .then(response => {
        if (!alive.current || generation !== revision.current) return
        api.current.csrfToken = response.body.csrf_token
        setAuthenticated(true)
      })
      .catch(() => {
        if (alive.current && generation === revision.current) setAuthenticated(false)
      })
    return () => {
      alive.current = false
      revision.current += 1
      meta.remove()
    }
  }, [])

  async function login() {
    const generation = ++revision.current
    await action("login", async () => {
      const response = await api.current.request<{ csrf_token: string }>("POST", "/api/v1/admin/login", { password })
      if (!alive.current || generation !== revision.current) return
      api.current.csrfToken = response.body.csrf_token
      setPassword("")
      setAuthenticated(true)
    })
  }

  return { api, auth, setAuth, password, setPassword, reveal, setReveal, login }
}
